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

	// Autocomplete dropdown state.
	suggestions   []Command
	suggestionIdx int
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

	// Wire OnRegisterCommand here so extensions can register slash commands
	// during _init (before SetProgram is called).
	if h != nil {
		cmds := m.commands
		h.OnRegisterCommand = func(name, desc string) {
			cmds.Register(Command{
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
		m.chat.SetSize(msg.Width, m.chatHeight())
		m.input.SetWidth(msg.Width - 4)
		m.statusBar.SetWidth(msg.Width)
		return m, nil

	case tea.KeyPressMsg:
		// Dropdown navigation takes priority over textarea key handling.
		if len(m.suggestions) > 0 {
			switch msg.String() {
			case "up":
				if m.suggestionIdx > 0 {
					m.suggestionIdx--
				}
				return m, nil
			case "down":
				if m.suggestionIdx < len(m.suggestions)-1 {
					m.suggestionIdx++
				}
				return m, nil
			case "tab":
				// Autocomplete: fill the selected command name into the input.
				if m.suggestionIdx < len(m.suggestions) {
					m.input.SetValue("/" + m.suggestions[m.suggestionIdx].Name + " ")
				}
				m.closeSuggestions()
				return m, nil
			case "enter":
				// Select and immediately dispatch the command.
				if m.suggestionIdx < len(m.suggestions) {
					cmd := m.suggestions[m.suggestionIdx]
					m.input.Reset()
					m.closeSuggestions()
					return m, cmd.Handler(nil)
				}
			case "esc":
				m.closeSuggestions()
				return m, nil
			}
		}

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
		m.closeSuggestions()
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
	m.updateSuggestions() // recompute dropdown after any key that may change input
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

// chatHeight returns the number of lines available for the chat viewport,
// accounting for the input box and any visible suggestion dropdown.
func (m Model) chatHeight() int {
	h := m.height - inputAreaHeight - statusBarHeight - m.dropdownHeight()
	if h < 1 {
		h = 1
	}
	return h
}

// dropdownHeight returns the rendered height of the suggestion dropdown (0 when hidden).
func (m Model) dropdownHeight() int {
	n := len(m.suggestions)
	if n == 0 {
		return 0
	}
	if n > 8 {
		n = 8
	}
	return n + 2 // top border + entries + bottom border
}

// updateSuggestions recomputes the autocomplete list based on current input.
// If the input starts with / and has no spaces, suggestions are filtered commands.
func (m *Model) updateSuggestions() {
	val := m.input.Value()
	if !strings.HasPrefix(val, "/") || strings.ContainsRune(val, ' ') {
		m.closeSuggestions()
		return
	}
	query := strings.ToLower(val[1:])
	var matched []Command
	for _, cmd := range m.commands.List() {
		if strings.HasPrefix(cmd.Name, query) {
			matched = append(matched, cmd)
		}
	}
	prevH := m.dropdownHeight()
	m.suggestions = matched
	if m.suggestionIdx >= len(matched) {
		m.suggestionIdx = 0
	}
	if m.dropdownHeight() != prevH {
		m.chat.SetSize(m.width, m.chatHeight())
	}
}

// closeSuggestions hides the dropdown and restores the full chat height.
func (m *Model) closeSuggestions() {
	if len(m.suggestions) == 0 {
		return
	}
	m.suggestions = nil
	m.suggestionIdx = 0
	m.chat.SetSize(m.width, m.chatHeight())
}

// renderDropdown builds the suggestion dropdown string, or "" when hidden.
func (m Model) renderDropdown() string {
	if len(m.suggestions) == 0 {
		return ""
	}
	shown := m.suggestions
	if len(shown) > 8 {
		shown = shown[:8]
	}

	width := m.width
	if width < 20 {
		width = 20
	}
	innerWidth := width - 2
	contentWidth := innerWidth - 2

	border := lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFFFF"))
	selStyle := lipgloss.NewStyle().Background(lipgloss.Color("#1A4A8A")).Foreground(lipgloss.Color("#FFFFFF"))
	normStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#CCCCCC"))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#666666"))

	var sb strings.Builder
	sb.WriteString(border.Render("╭" + strings.Repeat("─", innerWidth) + "╮"))
	sb.WriteString("\n")

	for i, cmd := range shown {
		name := "/" + cmd.Name
		desc := cmd.Desc
		// name in brighter colour, desc dimmed — measure combined visible width
		sep := "  "
		visLen := len([]rune(name)) + len(sep) + len([]rune(desc))
		if visLen > contentWidth {
			// Truncate description to fit
			trim := contentWidth - len([]rune(name)) - len(sep)
			if trim < 0 {
				trim = 0
			}
			desc = string([]rune(desc)[:min(trim, len([]rune(desc)))])
		}
		padding := strings.Repeat(" ", max(0, contentWidth-len([]rune(name))-len(sep)-len([]rune(desc))))

		sb.WriteString(border.Render("│") + " ")
		if i == m.suggestionIdx {
			line := name + sep + desc + padding
			if len([]rune(line)) > contentWidth {
				line = string([]rune(line)[:contentWidth])
			}
			sb.WriteString(selStyle.Render(line))
		} else {
			sb.WriteString(normStyle.Render(name))
			sb.WriteString(dimStyle.Render(sep + desc))
			sb.WriteString(padding)
		}
		sb.WriteString(" " + border.Render("│") + "\n")
	}

	sb.WriteString(border.Render("╰" + strings.Repeat("─", innerWidth) + "╯"))
	return sb.String()
}

// View renders the full TUI.
func (m Model) View() tea.View {
	var sb strings.Builder
	sb.WriteString(strings.TrimRight(m.chat.View(), "\n") + "\n")
	if dropdown := m.renderDropdown(); dropdown != "" {
		sb.WriteString(dropdown)
		sb.WriteString("\n")
	}
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
