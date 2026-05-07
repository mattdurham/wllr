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
	suggestions    []Command
	suggestionIdx  int
	dropdownOffset int // first visible suggestion index

	// Modal overlay state (non-empty when modal is open).
	modalContent string
	modalScroll  int
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
			sm := SubmitMsg{Content: msg.Content}
			// For skill XML blocks, show a compact indicator in the chat
			// instead of the raw XML so the UI stays clean and responsive.
			if strings.HasPrefix(strings.TrimSpace(msg.Content), "<skill ") {
				sm.Display = skillDisplayName(msg.Content)
			}
			p.Send(sm)
		}
		m.extHost.OnAbort = func() {
			p.Send(abortStreamMsg{})
		}
		m.extHost.OnAfterToolCall = func(id, _, result string, isError bool) {
			p.Send(ToolCallDoneMsg{ID: id, IsError: isError, Output: result})
		}
		m.extHost.OnModal = func(text string) {
			p.Send(ShowModalMsg{Text: text})
		}

		// Wire agent and team management — all agent-pool operations belong in
		// the harness so cmd/main.go stays minimal.
		pool := m.agentPool
		m.extHost.OnAgentSpawn = func(id, name, systemPrompt, modelName string) error {
			if pool == nil {
				return fmt.Errorf("no agent pool")
			}
			lm, err := pool.LanguageModelForModel(context.Background(), modelName)
			if err != nil {
				return fmt.Errorf("spawn agent %q: get model %q: %w", id, modelName, err)
			}
			a, err := pool.Spawn(id, lm, agent.SpawnOpts{SystemPrompt: systemPrompt})
			if err != nil {
				return fmt.Errorf("spawn agent %q: %w", id, err)
			}
			a.SetOnToken(func(token string) { p.Send(TokenMsg{Token: token}) })
			a.SetOnDone(func(e error) { p.Send(StreamDoneMsg{Err: e}) })
			// Give sub-agents identical wiring to the main agent.
			agentID := id
			extHostRef := m.extHost
			logFnRef := m.logFn
			a.SetToolsFn(func() []fantasy.AgentTool {
				return BuildFantasyTools(extHostRef, agentID, logFnRef)
			})
			a.SetOnToolCall(func(toolCallID, toolName, input string) {
				p.Send(ToolCallStartMsg{ID: toolCallID, ToolName: toolName, Input: input})
			})
			return nil
		}
		m.extHost.OnAgentClose = func(id string) error {
			if pool == nil {
				return nil
			}
			return pool.Close(id)
		}
		m.extHost.OnAgentSendMessage = func(id, message string) error {
			if pool == nil {
				return fmt.Errorf("no agent pool")
			}
			return pool.Send(id, message)
		}
		m.extHost.OnAgentList = func() ([]extension.AgentInfo, error) {
			if pool == nil {
				return nil, nil
			}
			ids := pool.ListAgents()
			infos := make([]extension.AgentInfo, 0, len(ids))
			for _, id := range ids {
				infos = append(infos, extension.AgentInfo{ID: id, Name: id})
			}
			return infos, nil
		}
		m.extHost.OnAgentTokenCount = func() int64 {
			if pool == nil {
				return 0
			}
			return pool.TokenCount()
		}
		m.extHost.OnTeamCreate = func(id, _ string) error {
			if pool == nil {
				return fmt.Errorf("no agent pool")
			}
			_, err := pool.CreateTeam(id)
			return err
		}
		m.extHost.OnTeamClose = func(id string) error {
			if pool == nil {
				return nil
			}
			return pool.CloseTeam(context.Background(), id)
		}
		m.extHost.OnTeamAddMember = func(teamID, agentID string) error {
			if pool == nil {
				return fmt.Errorf("no agent pool")
			}
			t := pool.GetTeam(teamID)
			if t == nil {
				return fmt.Errorf("team not found: %s", teamID)
			}
			return t.AddMember(agentID)
		}
		m.extHost.OnTeamRemoveMember = func(teamID, agentID string) error {
			if pool == nil {
				return nil
			}
			t := pool.GetTeam(teamID)
			if t == nil {
				return fmt.Errorf("team not found: %s", teamID)
			}
			t.RemoveMember(agentID)
			return nil
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
		return BuildFantasyTools(extHost, "main", logFn)
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

	case ShowModalMsg:
		m.modalContent = msg.Text
		m.modalScroll = 0
		return m, nil

	case tea.KeyPressMsg:
		// Modal consumes all keys; esc/enter/q close it.
		if m.modalContent != "" {
			switch msg.String() {
			case "esc", "enter", "q":
				m.modalContent = ""
				m.modalScroll = 0
			case "up":
				if m.modalScroll > 0 {
					m.modalScroll--
				}
			case "down":
				chatH := m.height - inputAreaHeight
				if chatH < 5 {
					chatH = 5
				}
				modalH := chatH * 8 / 10
				if modalH < 5 {
					modalH = 5
				}
				contentLines := modalH - 2
				lines := strings.Split(strings.TrimRight(m.modalContent, "\n"), "\n")
				if max := len(lines) - contentLines; m.modalScroll < max {
					m.modalScroll++
				}
			}
			return m, nil
		}

		// Dropdown navigation takes priority over textarea key handling.
		if len(m.suggestions) > 0 {
			switch msg.String() {
			case "up":
				if m.suggestionIdx > 0 {
					m.suggestionIdx--
					if m.suggestionIdx < m.dropdownOffset {
						m.dropdownOffset = m.suggestionIdx
					}
				}
				return m, nil
			case "down":
				if m.suggestionIdx < len(m.suggestions)-1 {
					m.suggestionIdx++
					const maxShow = 8
					if m.suggestionIdx >= m.dropdownOffset+maxShow {
						m.dropdownOffset = m.suggestionIdx - maxShow + 1
					}
				}
				return m, nil
			case "tab":
				// Replace the /word at the cursor with the completed command name.
				if m.suggestionIdx < len(m.suggestions) {
					val := m.input.Value()
					if slashIdx := slashWordAt(val); slashIdx >= 0 {
						m.input.SetValue(val[:slashIdx] + "/" + m.suggestions[m.suggestionIdx].Name + " ")
					} else {
						m.input.SetValue("/" + m.suggestions[m.suggestionIdx].Name + " ")
					}
				}
				m.closeSuggestions()
				return m, nil
			case "enter":
				// Dispatch the selected command and clear input.
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
		// Explicit chat scroll — pgup/pgdown work while typing without
		// triggering the viewport's default vim-style key bindings.
		case "pgup":
			m.chat.ScrollUp(m.chat.height / 2)
			return m, nil
		case "pgdown":
			m.chat.ScrollDown(m.chat.height / 2)
			return m, nil
		}

	case TokenMsg:
		m.chat.AppendToken(msg.Token)
		return m, nil

	case streamTickMsg:
		if m.streaming {
			since := time.Since(m.streamStart)
			dots := strings.Repeat(".", int(since/400/time.Millisecond)%3+1)
			m.statusBar.statuses["stream"] = fmt.Sprintf("working%-3s %s", dots, formatElapsed(since))
			cmds = append(cmds, tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg { return streamTickMsg{} }))
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
		// Refresh autocomplete in case extensions registered new commands.
		m.updateSuggestions()
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
		return m.submitToAgent(msg.Content, msg.Display)

	case CommandMsg:
		if msg.Name == "help" {
			return m, func() tea.Msg { return ShowModalMsg{Text: m.commands.HelpText()} }
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

	// Only forward non-key events to the chat viewport. Key events would be
	// interpreted by the viewport's default key map (k/j/u/d/f/b/space etc.)
	// as scroll commands while the user is typing, causing unwanted scrolling.
	// Mouse wheel events (MouseWheelDown/Up) are non-key and still work.
	if _, isKey := msg.(tea.KeyPressMsg); !isKey {
		var chatCmd tea.Cmd
		m.chat, chatCmd = m.chat.Update(msg)
		cmds = append(cmds, chatCmd)
	}

	return m, tea.Batch(cmds...)
}

// addAssistantMsgToHistoryMsg carries a finalised assistant message to add to history.
type addAssistantMsgToHistoryMsg struct{ content string }

// skillDisplayName extracts a compact display string from a skill XML block.
// Returns something like "[skill: bob:work]" instead of the raw XML.
func skillDisplayName(xml string) string {
	if idx := strings.Index(xml, `name="`); idx >= 0 {
		rest := xml[idx+6:]
		if end := strings.Index(rest, `"`); end >= 0 {
			return "[skill: " + rest[:end] + "]"
		}
	}
	return "[skill]"
}

// submitToAgent processes user input: adds to history, marks streaming, submits to the main agent.
// The agent's onToken/onDone callbacks (wired in SetProgram) deliver results back to the program.
func (m Model) submitToAgent(content, display string) (tea.Model, tea.Cmd) {
	// Use display text if provided; otherwise derive one from the content.
	chatText := display
	if chatText == "" {
		chatText = content
	}
	m.chat.AddUserMessage(chatText)
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

	// Fire an immediate tick so the "working." indicator appears on the very
	// first render, then continue at 100ms to keep the display responsive
	// during streaming (tokens otherwise pile up and appear all at once).
	immediateTick := func() tea.Msg { return streamTickMsg{} }
	tick := tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg { return streamTickMsg{} })
	return m, tea.Batch(cmd, immediateTick, tick)
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

// slashWordAt finds the index of a slash that starts the current incomplete
// command word — the last "/" that is either at position 0 or preceded by a
// space, with no space between it and the end of the string.
// Returns -1 if no such position exists.
func slashWordAt(val string) int {
	idx := strings.LastIndex(val, "/")
	if idx < 0 {
		return -1
	}
	// Must be word-start: first char or preceded by a space.
	if idx > 0 && val[idx-1] != ' ' {
		return -1
	}
	// No space between the / and end of input (still typing the command).
	if strings.ContainsRune(val[idx+1:], ' ') {
		return -1
	}
	return idx
}

// updateSuggestions recomputes the autocomplete list based on current input.
// Triggers when a "/" word-start is found anywhere in the input.
func (m *Model) updateSuggestions() {
	val := m.input.Value()
	idx := slashWordAt(val)
	if idx < 0 {
		m.closeSuggestions()
		return
	}
	query := strings.ToLower(val[idx+1:])
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
	m.dropdownOffset = 0
	m.chat.SetSize(m.width, m.chatHeight())
}

// renderDropdown builds the suggestion dropdown string, or "" when hidden.
func (m Model) renderDropdown() string {
	if len(m.suggestions) == 0 {
		return ""
	}
	const maxShow = 8
	start := m.dropdownOffset
	end := start + maxShow
	if end > len(m.suggestions) {
		end = len(m.suggestions)
	}
	shown := m.suggestions[start:end]

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
		if start+i == m.suggestionIdx {
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
	// Render the input box once — it's used for both height calculation and output.
	inputBox := m.renderInputBox()

	var sb strings.Builder
	if m.modalContent != "" {
		chatH := m.height - inputAreaHeight
		if chatH < 5 {
			chatH = 5
		}
		modalH := chatH * 8 / 10
		topMargin := (chatH - modalH) / 2
		bottomMargin := chatH - topMargin - modalH

		blank := strings.Repeat(" ", m.width)
		for i := 0; i < topMargin; i++ {
			sb.WriteString(blank + "\n")
		}
		sb.WriteString(m.renderModal(modalH) + "\n")
		for i := 0; i < bottomMargin; i++ {
			sb.WriteString(blank + "\n")
		}
	} else {
		sb.WriteString(strings.TrimRight(m.chat.View(), "\n") + "\n")
		if dropdown := m.renderDropdown(); dropdown != "" {
			sb.WriteString(dropdown)
			sb.WriteString("\n")
		}
	}
	sb.WriteString(inputBox)

	// Pad to exactly m.height lines so no old content bleeds through when
	// the viewport shrinks (e.g. dropdown appears/disappears).
	out := sb.String()
	if m.height > 0 {
		lineCount := strings.Count(out, "\n")
		if lineCount < m.height-1 {
			out += strings.Repeat("\n", m.height-1-lineCount)
		}
	}

	v := tea.NewView(out)
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

// renderModal renders a centered modal popup sized at 80% width × height lines.
// The caller is responsible for adding the top/bottom margin blank lines.
func (m Model) renderModal(height int) string {
	if height < 5 {
		height = 5
	}
	// 80% width, horizontally centered.
	modalW := m.width * 8 / 10
	if modalW < 20 {
		modalW = 20
	}
	leftPad := strings.Repeat(" ", (m.width-modalW)/2)

	width := modalW
	innerWidth := width - 2
	contentWidth := innerWidth - 2
	contentLines := height - 2 // room for top + bottom border

	b := lipgloss.NewStyle().Foreground(lipgloss.Color("#AAAAAA"))
	lines := strings.Split(strings.TrimRight(m.modalContent, "\n"), "\n")
	maxScroll := len(lines) - contentLines
	if maxScroll < 0 {
		maxScroll = 0
	}
	scroll := m.modalScroll
	if scroll > maxScroll {
		scroll = maxScroll
	}

	hint := " ↑↓ scroll · esc close "
	if maxScroll > 0 {
		pct := 0
		if maxScroll > 0 {
			pct = scroll * 100 / maxScroll
		}
		hint = fmt.Sprintf(" ↑↓ scroll (%d%%) · esc close ", pct)
	}
	fillLen := innerWidth - len([]rune(hint))
	if fillLen < 0 {
		fillLen = 0
	}
	top := leftPad + b.Render("╭"+hint+strings.Repeat("─", fillLen)+"╮")

	var body strings.Builder
	for i := 0; i < contentLines; i++ {
		idx := scroll + i
		line := ""
		if idx < len(lines) {
			line = lines[idx]
		}
		runes := []rune(line)
		if len(runes) > contentWidth {
			runes = runes[:contentWidth]
		}
		padding := strings.Repeat(" ", contentWidth-len(runes))
		body.WriteString(leftPad + b.Render("│") + " " + string(runes) + padding + " " + b.Render("│") + "\n")
	}

	bottom := leftPad + b.Render("╰"+strings.Repeat("─", innerWidth)+"╯")
	return top + "\n" + body.String() + bottom
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

	// Always read the live pool token count so sub-agent tokens are visible.
	if m.agentPool != nil {
		m.statusBar.totalTokens = int(m.agentPool.TokenCount())
	}

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
