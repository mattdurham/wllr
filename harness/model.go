package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/fantasy"
	lipgloss "charm.land/lipgloss/v2"
	"github.com/mattdurham/wllr/agent"
	"github.com/mattdurham/wllr/extension"
	"github.com/mattdurham/wllr/sdk"
)

// Model is the root bubbletea v2 model for the bob TUI.
type Model struct {
	chat      ChatView
	input     InputArea
	statusBar StatusBar
	commands  *Registry

	agentPool   *agent.AgentPool
	mainAgentID string
	extHost     *extension.Host

	history     []sdk.Message
	streaming   bool
	streamStart time.Time
	activeModel string

	width, height int

	// Loaded extension paths for reload.
	extPaths []string

	// program is set after the bubbletea program starts so goroutines can
	// send messages back. Set via SetProgram.
	program *tea.Program

	// logFn is used for warning/debug logging from the harness (e.g. tool adapter).
	// If nil, warnings are silently dropped.
	logFn func(int, string)
}

// inputAreaHeight = top border (1) + textarea rows (3) + bottom border (1)
const inputAreaHeight = 5
const statusBarHeight = 0

// New creates a Model wired to the given agent pool, main agent ID, and extension host.
// The pool must have its provider name set via SetProviderName before calling New
// for correct status bar display.
func New(pool *agent.AgentPool, mainAgentID string, h *extension.Host) Model {
	provName := ""
	if pool != nil {
		provName = pool.ProviderName()
	}

	m := Model{
		chat:        NewChatView(80, 20),
		input:       NewInputArea(80),
		statusBar:   NewStatusBar(provName, ""),
		commands:    NewRegistry(),
		agentPool:   pool,
		mainAgentID: mainAgentID,
		extHost:     h,
	}

	registerBuiltins(m.commands)
	return m
}

// SetProgram stores the bubbletea program reference so goroutines can call Send.
// It also wires the main agent's onToken and onDone callbacks to the program.
func (m *Model) SetProgram(p *tea.Program) {
	m.program = p
	if m.extHost != nil {
		m.extHost.OnSetStatus = func(k, v string) {
			p.Send(StatusUpdateMsg{Key: k, Value: v})
		}
		m.extHost.OnNotify = func(text string) {
			p.Send(NotifyMsg{Text: text})
		}
		m.extHost.OnSendMessage = func(msg sdk.Message) {
			p.Send(SubmitMsg{Content: msg.Content})
		}
		m.extHost.OnAbort = func() {
			p.Send(abortStreamMsg{})
		}
		m.extHost.OnAfterToolCall = func(id, _, result string, isError bool) {
			p.Send(ToolCallDoneMsg{ID: id, IsError: isError, Output: result})
		}
		m.extHost.OnRegisterCommand = func(name, desc string) {
			m.commands.Register(Command{
				Name: name,
				Desc: desc,
				Handler: func(args []string) tea.Cmd {
					return func() tea.Msg {
						return dispatchOnCommandMsg{Name: name, Args: args}
					}
				},
			})
		}
	}
	// Wire the main agent's token and done callbacks so streaming output reaches the TUI.
	m.wireMainAgentCallbacks(p)
}

// wireMainAgentCallbacks sets the onToken, onDone, and toolsFn callbacks on the main agent.
func (m *Model) wireMainAgentCallbacks(p *tea.Program) {
	if m.agentPool == nil {
		return
	}
	a := m.agentPool.Get(m.mainAgentID)
	if a == nil {
		return
	}
	a.SetOnToken(func(token string) {
		p.Send(TokenMsg{Token: token})
	})
	a.SetOnDone(func(err error) {
		p.Send(StreamDoneMsg{Err: err})
	})
	extHost := m.extHost
	logFn := m.logFn
	a.SetToolsFn(func() []fantasy.AgentTool {
		return BuildFantasyTools(extHost, logFn)
	})
	a.SetOnToolCall(func(id, toolName, input string) {
		p.Send(ToolCallStartMsg{ID: id, ToolName: toolName, Input: input})
	})
}

// SetLogFn sets the logging function used for internal harness warnings
// (e.g. tool schema parse errors). If fn is nil, warnings are silently dropped.
// Must be called before prog.Run().
func (m *Model) SetLogFn(fn func(int, string)) {
	m.logFn = fn
}

// SetExtensionPaths sets the paths used by /reload.
func (m *Model) SetExtensionPaths(paths []string) {
	m.extPaths = paths
}

// Init returns the initial command.
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.input.ta.Focus(),
		m.cmdDispatchSessionStart(),
	)
}

// cmdDispatchSessionStart sends EventSessionStart to all extensions asynchronously.
func (m Model) cmdDispatchSessionStart() tea.Cmd {
	if m.extHost == nil {
		return nil
	}
	extHost := m.extHost
	return func() tea.Msg {
		payload, _ := json.Marshal(sdk.SessionStartPayload{Reason: "new_session"})
		evt := sdk.Event{Type: sdk.EventSessionStart, Payload: payload}
		results, err := extHost.DispatchEvent(context.Background(), evt)
		return ExtensionEventResultMsg{Results: results, Err: err}
	}
}

// Update handles all incoming messages.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		chatHeight := msg.Height - inputAreaHeight - statusBarHeight
		if chatHeight < 1 {
			chatHeight = 1
		}
		m.chat.SetSize(msg.Width, chatHeight)
		m.input.SetWidth(msg.Width - 4) // textarea sits inside the bordered box
		m.statusBar.SetWidth(msg.Width)
		return m, nil

	case tea.KeyPressMsg:
		switch msg.String() {
		case "ctrl+c":
			if m.streaming {
				if m.agentPool != nil {
					_ = m.agentPool.Cancel(m.mainAgentID)
				}
				return m, nil
			}
			return m, tea.Quit
		case "ctrl+q":
			return m, tea.Quit
		}

	case TokenMsg:
		m.chat.AppendToken(msg.Token)
		return m, nil

	case streamTickMsg:
		if m.streaming {
			since := time.Since(m.streamStart)
			dots := strings.Repeat(".", int(since/400/time.Millisecond)%3+1)
			m.statusBar.statuses["stream"] = fmt.Sprintf("working%-3s %s", dots, formatElapsed(since))
			cmds = append(cmds, tea.Tick(400*time.Millisecond, func(time.Time) tea.Msg { return streamTickMsg{} }))
		}
		return m, tea.Batch(cmds...)

	case StreamDoneMsg:
		m.streaming = false
		if m.agentPool != nil {
			m.statusBar.totalTokens = int(m.agentPool.TokenCount())
		}
		if msg.Err != nil && !errors.Is(msg.Err, context.Canceled) {
			m.chat.AddNotification(fmt.Sprintf("Error: %v", msg.Err))
			m.statusBar.statuses["stream"] = "error"
		} else {
			delete(m.statusBar.statuses, "stream")
		}
		m.chat.FinalizeMessage()
		cmds = append(cmds, m.cmdDispatchAfterProviderResponse())
		return m, tea.Batch(cmds...)

	case addAssistantMsgToHistoryMsg:
		m.history = append(m.history, sdk.Message{Role: sdk.RoleAssistant, Content: msg.content})
		return m, nil

	case ExtensionEventResultMsg:
		for _, r := range msg.Results {
			if r.Error != "" {
				m.chat.AddNotification(fmt.Sprintf("Extension error: %s", r.Error))
			}
		}
		return m, nil

	case ReloadMsg:
		return m, m.cmdReloadExtensions()

	case NotifyMsg:
		m.chat.AddNotification(msg.Text)
		return m, nil

	case StatusUpdateMsg:
		m.statusBar, _ = m.statusBar.Update(msg)
		return m, nil

	case SubmitMsg:
		// With the pool-based model, messages are always queued — no streaming guard.
		return m.submitToAgent(msg.Content)

	case CommandMsg:
		// /help is special: show command list in chat
		if msg.Name == "help" {
			m.chat.AddNotification(m.commands.HelpText())
			return m, nil
		}
		return m, m.commands.Dispatch(msg.Name, msg.Args)

	case clearMsg:
		m.chat.Clear()
		m.history = nil
		return m, nil

	case setModelMsg:
		m.activeModel = msg.Model
		m.statusBar.modelName = msg.Model
		m.chat.AddNotification(fmt.Sprintf("Model set to: %s", msg.Model))
		return m, nil

	case abortStreamMsg:
		if m.agentPool != nil {
			_ = m.agentPool.Cancel(m.mainAgentID)
		}
		return m, nil

	case ToolCallStartMsg:
		m.chat.AddToolCall(msg.ID, msg.ToolName, msg.Input)
		return m, nil

	case ToolCallDoneMsg:
		m.chat.UpdateToolCall(msg.ID, msg.IsError, msg.Output)
		return m, nil

	case dispatchOnCommandMsg:
		if m.extHost != nil {
			extHost := m.extHost
			return m, func() tea.Msg {
				payload, _ := json.Marshal(sdk.OnCommandPayload{Name: msg.Name, Args: msg.Args})
				evt := sdk.Event{Type: sdk.EventOnCommand, Payload: payload}
				results, err := extHost.DispatchEvent(context.Background(), evt)
				return ExtensionEventResultMsg{Results: results, Err: err}
			}
		}
		return m, nil
	}

	// Forward to sub-models.
	var inputCmd tea.Cmd
	m.input, inputCmd = m.input.Update(msg)
	cmds = append(cmds, inputCmd)

	var chatCmd tea.Cmd
	m.chat, chatCmd = m.chat.Update(msg)
	cmds = append(cmds, chatCmd)

	return m, tea.Batch(cmds...)
}

// addAssistantMsgToHistoryMsg carries a finalised assistant message to add to history.
type addAssistantMsgToHistoryMsg struct{ content string }

// submitToAgent processes user input: adds to history, marks streaming, submits to the main agent.
// The agent's onToken/onDone callbacks (wired in SetProgram) deliver results back to the program.
func (m Model) submitToAgent(content string) (tea.Model, tea.Cmd) {
	m.chat.AddUserMessage(content)
	m.history = append(m.history, sdk.Message{Role: sdk.RoleUser, Content: content})
	m.streaming = true
	m.streamStart = time.Now()
	m.statusBar.statuses["stream"] = "working."

	pool := m.agentPool
	mainAgentID := m.mainAgentID
	extHost := m.extHost
	activeModel := m.activeModel

	cmd := func() tea.Msg {
		// Dispatch before_agent_start.
		if extHost != nil {
			payload, _ := json.Marshal(sdk.BeforeAgentStartPayload{
				Prompt: content,
			})
			evt := sdk.Event{Type: sdk.EventBeforeAgentStart, Payload: payload}
			_, _ = extHost.DispatchEvent(context.Background(), evt)
		}

		// Dispatch before_provider_request.
		if extHost != nil {
			payload, _ := json.Marshal(sdk.BeforeProviderRequestPayload{
				Messages: []sdk.Message{{Role: sdk.RoleUser, Content: content}},
				Model:    activeModel,
			})
			evt := sdk.Event{Type: sdk.EventBeforeProviderRequest, Payload: payload}
			_, _ = extHost.DispatchEvent(context.Background(), evt)
		}

		if pool == nil {
			return StreamDoneMsg{Err: fmt.Errorf("no agent pool configured")}
		}

		// Submit to the main agent. The agent runs its turn in a goroutine and
		// calls onToken/onDone (wired in SetProgram) to deliver results back.
		if err := pool.Send(mainAgentID, content); err != nil {
			return StreamDoneMsg{Err: fmt.Errorf("submit to agent: %w", err)}
		}

		// pool.Send is non-blocking; results arrive via callbacks. Return nil.
		return nil
	}

	tick := tea.Tick(400*time.Millisecond, func(time.Time) tea.Msg { return streamTickMsg{} })
	return m, tea.Batch(cmd, tick)
}

func (m Model) cmdDispatchAfterProviderResponse() tea.Cmd {
	if m.extHost == nil {
		return nil
	}
	extHost := m.extHost
	return func() tea.Msg {
		payload, _ := json.Marshal(sdk.AfterProviderResponsePayload{})
		evt := sdk.Event{Type: sdk.EventAfterProviderResponse, Payload: payload}
		results, err := extHost.DispatchEvent(context.Background(), evt)
		return ExtensionEventResultMsg{Results: results, Err: err}
	}
}

func (m Model) cmdReloadExtensions() tea.Cmd {
	if m.extHost == nil {
		return func() tea.Msg { return NotifyMsg{Text: "No extension host configured."} }
	}
	paths := m.extPaths
	extHost := m.extHost
	return func() tea.Msg {
		if err := extHost.Reload(context.Background(), paths); err != nil {
			return NotifyMsg{Text: fmt.Sprintf("Reload error: %v", err)}
		}
		return NotifyMsg{Text: "Extensions reloaded."}
	}
}

// View renders the full TUI.
func (m Model) View() tea.View {
	var sb strings.Builder
	// Normalise chat viewport output to exactly one trailing newline so the
	// input box top border always starts on its own line.
	sb.WriteString(strings.TrimRight(m.chat.View(), "\n") + "\n")
	sb.WriteString(m.renderInputBox())
	v := tea.NewView(sb.String())
	v.AltScreen = true
	return v
}

// renderInputBox wraps the textarea in a white-bordered box with the status
// line embedded in the top border, matching the tool-call box style.
func (m Model) renderInputBox() string {
	width := m.width
	if width < 8 {
		width = 8
	}
	innerWidth := width - 2
	contentWidth := innerWidth - 2

	b := lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFFFF"))

	// Build top border: ╭─ provider  model  status ──────────────╮
	label := m.statusBar.Line()
	var prefix string
	if label != "" {
		prefix = "─ " + label + " "
	}
	fillLen := innerWidth - len([]rune(prefix))
	if fillLen < 0 {
		fillLen = 0
	}
	fill := strings.Repeat("─", fillLen)
	top := b.Render("╭" + prefix + fill + "╮")

	// Embed textarea lines between side borders.
	// Use lipgloss.Width for padding — len([]rune) counts ANSI escape bytes.
	taLines := strings.Split(strings.TrimRight(m.input.View(), "\n"), "\n")
	var body strings.Builder
	for _, line := range taLines {
		visible := lipgloss.Width(line)
		pad := contentWidth - visible
		if pad < 0 {
			pad = 0
		}
		body.WriteString(b.Render("│") + " " + line + strings.Repeat(" ", pad) + " " + b.Render("│") + "\n")
	}

	bottom := b.Render("╰" + strings.Repeat("─", innerWidth) + "╯")
	return top + "\n" + body.String() + bottom
}
