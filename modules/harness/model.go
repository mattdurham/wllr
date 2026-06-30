package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/fantasy"
	lipgloss "charm.land/lipgloss/v2"
	"github.com/mattdurham/wllr/modules/agent"
	"github.com/mattdurham/wllr/modules/extension"
	"github.com/mattdurham/wllr/modules/sdk"
	"github.com/mattdurham/wllr/modules/tools"
)

// liveState holds fields that change frequently and must be readable by
// OnGetStatusInfo closures. Since Model is a value type, closures cannot
// capture fields directly — they capture this shared pointer instead.
// mu protects all fields; OnGetStatusInfo runs on the WASM goroutine
// concurrently with Update() on the bubbletea goroutine.
type liveState struct {
	streamStart time.Time
	statuses    map[string]string
	provider    string
	model       string
	width       int
	tokens      int
	mu          sync.RWMutex
	streaming   bool
	hasError    bool
}

func (s *liveState) setStreaming(v bool, start time.Time, hasErr bool) {
	s.mu.Lock()
	s.streaming = v
	if v {
		s.streamStart = start
		s.hasError = false
	} else if hasErr {
		s.hasError = true
	}
	s.mu.Unlock()
}

func (s *liveState) setWidth(w int) {
	s.mu.Lock()
	s.width = w
	s.mu.Unlock()
}

func (s *liveState) setModel(model string) {
	s.mu.Lock()
	s.model = model
	s.mu.Unlock()
}

func (s *liveState) setTokens(n int) {
	s.mu.Lock()
	s.tokens = n
	s.mu.Unlock()
}

func (s *liveState) setStatus(key, value string) {
	s.mu.Lock()
	if s.statuses == nil {
		s.statuses = make(map[string]string)
	}
	if value == "" {
		delete(s.statuses, key)
	} else {
		s.statuses[key] = value
	}
	s.mu.Unlock()
}

func (s *liveState) getStatus(key string) string {
	s.mu.RLock()
	v := s.statuses[key]
	s.mu.RUnlock()
	return v
}

// Model is the root bubbletea v2 model for the bob TUI.
type Model struct {
	streamStart time.Time
	commands    *Registry

	agentPool *agent.AgentPool
	extHost   *extension.Host
	live      *liveState // shared pointer updated in-place; safe for closure capture

	// program is set after the bubbletea program starts so goroutines can
	// send messages back. Set via SetProgram.
	program *tea.Program

	// logFn is used for warning/debug logging from the harness (e.g. tool adapter).
	// If nil, warnings are silently dropped.
	logFn func(int, string)

	// OnMessageEnd is called after a completed assistant turn with the role
	// and full content string. It is invoked on the bubbletea update goroutine.
	// Nil means no callback. Set by cmd/main.go for session persistence.
	OnMessageEnd func(role, content string)

	// OnUserMessage is called when the user submits a message, before it is
	// sent to the agent. It is invoked on the bubbletea update goroutine.
	// Nil means no callback. Set by cmd/main.go for session persistence.
	OnUserMessage func(content string)

	// scene holds extension-driven UI areas (the declarative, node-based
	// renderer). Shared by pointer with the harnessUIBridge so the bridge can
	// mutate it off-loop and the View can read it.
	scene *SceneRenderer

	mainAgentID string
	activeModel string

	// Modal overlay state (non-empty when modal is open).
	modalContent string
	// streamContent accumulates the in-flight assistant response text so it can
	// be captured for session persistence (OnMessageEnd) and logging when the
	// turn completes. The transcript itself is rendered by the WASM extension.
	streamContent string
	input         InputArea

	// Loaded extension paths for reload.
	extPaths []string

	// Autocomplete dropdown state.
	suggestions []Command

	console ConsoleView


	chat ChatView

	picker PickerView

	width, height int

	suggestionIdx  int
	dropdownOffset int // first visible suggestion index

	modalScroll    int
	streaming      bool
	consoleVisible bool
}

// wasmChatAreaID is the scene area ID the WASM extension uses to own the main
// chat transcript. The harness feeds this area's rendered content into the
// (still harness-owned) scrollable chat viewport.
const wasmChatAreaID = "chat"

// statuslineAreaID is the scene area ID the statusline extension uses to drive
// the standalone status row rendered above the input box.
const statuslineAreaID = "statusline"

// streamStatusError is the status key value shown when a turn fails.
const streamStatusError = "error"

// inputAreaHeight = top border (1) + textarea rows (3) + bottom border (1)
const inputAreaHeight = 5

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
		commands:    NewRegistry(),
		agentPool:   pool,
		mainAgentID: mainAgentID,
		extHost:     h,
		console:     NewConsoleView(),
		live:        &liveState{provider: provName, statuses: make(map[string]string)},
		scene:       NewSceneRenderer(),
	}
	// Pre-create the statusline area so the placement slot is reserved before
	// session_start fires and the bundled statusline extension sets its root.
	_ = m.scene.CreateArea(sdk.UIArea{ID: statuslineAreaID, Placement: sdk.UIAreaStatus})

	registerBuiltins(m.commands)

	// /prompt shows the accumulated system prompt so users can verify
	// what's actually being sent to the LLM (AGENTS.md, skills list, etc.).
	if pool != nil {
		m.commands.Register(Command{
			Name: "prompt",
			Desc: "Show the current system prompt sent to the LLM",
			Handler: func(_ []string) tea.Cmd {
				return func() tea.Msg {
					sp := pool.BaseSystemPrompt()
					if sp == "" {
						sp = "(no system prompt set)"
					}
					return ShowModalMsg{Text: sp}
				}
			},
		})
	}

	// Wire a minimal UIBridge and stub AgentBridge for _init (before SetProgram).
	// The full bridges are wired in SetProgram once the tea.Program is available.
	if h != nil {
		cmds := m.commands
		h.SetUIBridge(&earlyUIBridge{cmds: cmds})
		h.SetAgentBridge(&earlyAgentBridge{})
	}

	return m
}

// SetProgram stores the bubbletea program reference so goroutines can call Send.
// It also wires the main agent's onToken and onDone callbacks to the program,
// and installs the interface bridges on the extension host.
func (m *Model) SetProgram(p *tea.Program) {
	m.program = p
	if m.extHost != nil {
		pool := m.agentPool
		extHostRef := m.extHost
		logFnRef := m.logFn
		mainID := m.mainAgentID

		spawner := agent.NewSpawner(pool, func(agentID string) []fantasy.AgentTool {
			return tools.BuildFantasyTools(extHostRef, agentID, logFnRef)
		}, func(text string) {
			p.Send(NotifyMsg{Text: "⚠ " + text})
		})

		m.extHost.SetAgentBridge(&harnessAgentBridge{
			pool:    pool,
			spawner: spawner,
			mainID:  mainID,
			prog:    p,
		})
		m.extHost.SetTeamBridge(&harnessTeamBridge{pool: pool})
		m.extHost.SetUIBridge(&harnessUIBridge{
			pool:   pool,
			prog:   p,
			live:   m.live,
			mainID: mainID,
			cmds:   m.commands,
			scene:  m.scene,
		})

		// Wire context-usage dispatcher so agent turns forward EventContextUsage
		// to WASM extensions without a circular import between agent and extension.
		if pool != nil {
			pool.SetContextUsageDispatcher(func(cu sdk.ContextUsage, compacted bool) {
				payload, _ := json.Marshal(sdk.ContextUsagePayload{Usage: cu, Compacted: compacted})
				evt := sdk.Event{Type: sdk.EventContextUsage, Payload: payload}
				_, _ = extHostRef.DispatchEvent(context.Background(), evt)
			})
		}
	}

	m.wireMainAgentCallbacks(p)
}

const tokenBatchInterval = 30 * time.Millisecond

func (b *tokenBatcher) onToken(token string) {
	b.mu.Lock()
	b.buf.WriteString(token)
	now := time.Now()
	if now.Sub(b.lastSend) >= tokenBatchInterval {
		s := b.buf.String()
		b.buf.Reset()
		b.lastSend = now
		b.mu.Unlock()
		b.p.Send(TokenMsg{Token: s})
		if b.dispatch != nil {
			b.dispatch(s)
		}
		return
	}
	b.mu.Unlock()
}

func (b *tokenBatcher) flush() {
	b.mu.Lock()
	s := b.buf.String()
	b.buf.Reset()
	b.mu.Unlock()
	if s != "" {
		b.p.Send(TokenMsg{Token: s})
		if b.dispatch != nil {
			b.dispatch(s)
		}
	}
}

// makeBatchedOnToken returns an onToken callback and a flush function.
// Tokens are coalesced into batches sent at most every 30ms, capping
// render cycles to ~33/sec regardless of LLM speed (prevents O(n²) work).
// flush() must be called from onDone to deliver any buffered tail tokens.
// dispatch, when non-nil, receives each flushed batch for forwarding to WASM
// (EventToken).
func makeBatchedOnToken(p *tea.Program, dispatch func(string)) (onToken func(string), flush func()) {
	b := &tokenBatcher{p: p, dispatch: dispatch}
	return b.onToken, b.flush
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
	extHostForToken := m.extHost
	mainID := m.mainAgentID
	var dispatchToken func(string)
	if extHostForToken != nil {
		dispatchToken = func(text string) {
			if text == "" {
				return
			}
			payload, _ := json.Marshal(sdk.TokenPayload{AgentID: mainID, Text: text})
			_, _ = extHostForToken.DispatchEvent(context.Background(), sdk.Event{Type: sdk.EventToken, Payload: payload})
		}
	}
	onToken, stopBatch := makeBatchedOnToken(p, dispatchToken)
	a.SetOnToken(onToken)
	a.SetOnDone(func(err error) {
		stopBatch()
		p.Send(StreamDoneMsg{Err: err})
	})
	extHost := m.extHost
	logFn := m.logFn
	a.SetToolsFn(func() []fantasy.AgentTool {
		return tools.BuildFantasyTools(extHost, agent.MainAgentID, logFn)
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
// It returns sessionStartDoneMsg (not ExtensionEventResultMsg) so the harness can
// inject the default action prompt after all session_start handlers have run.
func (m Model) cmdDispatchSessionStart() tea.Cmd {
	if m.extHost == nil {
		return nil
	}
	extHost := m.extHost
	return func() tea.Msg {
		payload, _ := json.Marshal(sdk.SessionStartPayload{Reason: "new_session"})
		evt := sdk.Event{Type: sdk.EventSessionStart, Payload: payload}
		results, err := extHost.DispatchEvent(context.Background(), evt)
		return sessionStartDoneMsg{Results: results, Err: err}
	}
}

// Update handles all incoming messages.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if n, cmd, ok := m.updateWindow(msg); ok {
		return n, cmd
	}
	if n, cmd, ok := m.updateKeyPress(msg); ok {
		return n, cmd
	}
	if n, cmd, ok := m.updateStream(msg); ok {
		return n, cmd
	}
	if n, cmd, ok := m.updateTools(msg); ok {
		return n, cmd
	}
	if n, cmd, ok := m.updateActions(msg); ok {
		return n, cmd
	}
	if n, cmd, ok := m.updateExtension(msg); ok {
		return n, cmd
	}

	// Forward to sub-models.
	var cmds []tea.Cmd
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

// updateWindow handles window resize and modal display messages.
// Returns (model, cmd, true) when the message was handled.
func (m Model) updateWindow(msg tea.Msg) (Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.live.setWidth(msg.Width)
		m.height = msg.Height
		m.chat.SetSize(msg.Width, m.chatHeight())
		m.picker.SetSize(msg.Width, m.chatHeight())
		m.input.SetWidth(msg.Width - 4)
		// Re-render the WASM transcript at the new width.
		m.refreshWASMChat()
		return m, nil, true
	case ShowModalMsg:
		m.modalContent = msg.Text
		m.modalScroll = 0
		return m, nil, true
	case ShowPickerMsg:
		m.picker.Open(msg.Title, msg.Items, msg.Callback)
		m.picker.SetSize(m.width, m.chatHeight())
		return m, nil, true
	case ResetHistoryMsg:
		// Agent history is replaced by the UIBridge; the visual transcript is
		// owned by the WASM extension, so we reset it to empty here. Subsequent
		// turns repopulate it. (The restored messages remain in agent context.)
		m.resetChatArea()
		m.streamContent = ""
		m.picker.Close()
		m.pushNotification("History restored — conversation loaded from selected point.")
		return m, nil, true
	}
	return m, nil, false
}

// updateKeyPress handles keyboard input: modal keys, dropdown navigation, and global hotkeys.
// Returns (model, cmd, true) when the message was handled.
func (m Model) updateKeyPress(msg tea.Msg) (Model, tea.Cmd, bool) {
	kp, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil, false
	}

	if m.picker.IsActive() {
		return m.updateKeyPressPicker(kp)
	}
	if m.modalContent != "" {
		return m.updateKeyPressModal(kp)
	}
	if len(m.suggestions) > 0 {
		if m2, cmd, handled := m.updateKeyPressDropdown(kp); handled {
			return m2, cmd, true
		}
	}

	switch kp.String() {
	case "ctrl+c":
		if m.streaming {
			if m.agentPool != nil {
				m.agentPool.CancelAll()
			}
			m.live.setStatus("stream", "cancelling…")
			return m, nil, true
		}
		return m, tea.Quit, true
	case "ctrl+q":
		return m, tea.Quit, true
	case "ctrl+t":
		m.modalContent = m.chat.ToolLogModal()
		m.modalScroll = 0
		return m, nil, true
	// Explicit chat scroll — pgup/pgdown work while typing without
	// triggering the viewport's default vim-style key bindings.
	case "pgup":
		m.chat.ScrollUp(m.chat.height / 2)
		return m, nil, true
	case "pgdown":
		m.chat.ScrollDown(m.chat.height / 2)
		return m, nil, true
	}

	return m, nil, false
}

// updateKeyPressPicker handles key events when the picker overlay is active.
func (m Model) updateKeyPressPicker(kp tea.KeyPressMsg) (Model, tea.Cmd, bool) {
	callback := m.picker.Callback
	selected, id, cancelled := m.picker.HandleKey(kp)
	if cancelled {
		m.picker.Close()
		return m, nil, true
	}
	if selected {
		m.picker.Close()
		extHost := m.extHost
		return m, func() tea.Msg {
			payload, _ := json.Marshal(sdk.OnCommandPayload{Name: callback, Args: []string{id}})
			evt := sdk.Event{Type: sdk.EventOnCommand, Payload: payload}
			results, err := extHost.DispatchEvent(context.Background(), evt)
			return ExtensionEventResultMsg{Results: results, Err: err}
		}, true
	}
	return m, nil, true
}

// updateKeyPressModal handles key events when the modal overlay is open.
func (m Model) updateKeyPressModal(kp tea.KeyPressMsg) (Model, tea.Cmd, bool) {
	switch kp.String() {
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
	return m, nil, true
}

// updateKeyPressDropdown handles key events when the autocomplete dropdown is visible.
// Returns (model, cmd, true) if the key was consumed; (m, nil, false) otherwise.
func (m Model) updateKeyPressDropdown(kp tea.KeyPressMsg) (Model, tea.Cmd, bool) {
	switch kp.String() {
	case "up":
		if m.suggestionIdx > 0 {
			m.suggestionIdx--
			if m.suggestionIdx < m.dropdownOffset {
				m.dropdownOffset = m.suggestionIdx
			}
		}
		return m, nil, true
	case "down":
		if m.suggestionIdx < len(m.suggestions)-1 {
			m.suggestionIdx++
			const maxShow = 8
			if m.suggestionIdx >= m.dropdownOffset+maxShow {
				m.dropdownOffset = m.suggestionIdx - maxShow + 1
			}
		}
		return m, nil, true
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
		return m, nil, true
	case "enter":
		// Dispatch the selected command and clear input.
		if m.suggestionIdx < len(m.suggestions) {
			cmd := m.suggestions[m.suggestionIdx]
			m.input.Reset()
			m.closeSuggestions()
			return m, cmd.Handler(nil), true
		}
	case "esc":
		m.closeSuggestions()
		return m, nil, true
	}
	return m, nil, false
}

// updateStream handles token streaming messages: TokenMsg, streamTickMsg, StreamDoneMsg, agentWakeupMsg.
// Returns (model, cmd, true) when the message was handled.
func (m Model) updateStream(msg tea.Msg) (Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case agentWakeupMsg:
		_ = msg
		if !m.streaming {
			m.streaming = true
			m.streamStart = time.Now()
			m.live.setStreaming(true, m.streamStart, false)
		}
		return m, tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg { return streamTickMsg{} }), true

	case TokenMsg:
		// Accumulate for session persistence/logging; the visible transcript is
		// rendered by the WASM extension via EventToken.
		m.streamContent += msg.Token
		return m, nil, true

	case streamTickMsg:
		cmds := make([]tea.Cmd, 0, 1)
		if m.streaming {
			cmds = append(cmds, tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg { return streamTickMsg{} }))
		}
		return m, tea.Batch(cmds...), true

	case StreamDoneMsg:
		cmds := make([]tea.Cmd, 0, 2)
		m.streaming = false
		m.live.setStreaming(false, time.Time{}, false)
		m.consoleVisible = false
		if m.agentPool != nil {
			n := int(m.agentPool.TokenCount())
			m.live.setTokens(n)
			// Update context-usage percentage from real API token counts.
			cu := m.agentPool.MainAgentContextUsage()
			if cu.ContextWindow > 0 {
				cfg := m.agentPool.CompactConfig()
				rem := cfg.ThresholdPct*100 - cu.Percent
				m.live.setStatus("ctx rem", fmt.Sprintf("%.0f%%", rem))
			} else {
				m.live.setStatus("ctx rem", "")
			}
		}
		if msg.Err != nil {
			if errors.Is(msg.Err, context.Canceled) {
				slog.Info("stream cancelled by user")
				m.live.setStatus("stream", "")
			} else {
				slog.Error("stream error", "err", msg.Err)
				m.pushNotification(fmt.Sprintf("⚠ %v", msg.Err))
				m.live.setStatus("stream", streamStatusError)
				m.live.setStreaming(false, time.Time{}, true)
			}
		} else {
			slog.Info("stream done", "tokens", m.agentPool.TokenCount())
			m.live.setStatus("stream", "")
		}
		// Capture and reset the accumulated response text.
		responseContent := m.streamContent
		m.streamContent = ""
		if msg.Err == nil {
			preview := responseContent
			if r := []rune(preview); len(r) > 150 {
				preview = string(r[:150]) + "…"
			}
			if preview == "" {
				preview = "(no text — tool calls only)"
			}
			slog.Info("stream response", "text", preview)
		}
		if responseContent != "" && m.OnMessageEnd != nil {
			m.OnMessageEnd(string(sdk.RoleAssistant), responseContent)
		}
		cmds = append(cmds, m.cmdDispatchAfterProviderResponse())
		if responseContent != "" {
			cmds = append(cmds, m.cmdDispatchMessageEnd(string(sdk.RoleAssistant), responseContent))
		}
		return m, tea.Batch(cmds...), true

	}
	return m, nil, false
}

// updateTools handles tool call lifecycle messages: ToolCallStartMsg and ToolCallDoneMsg.
// Returns (model, cmd, true) when the message was handled.
func (m Model) updateTools(msg tea.Msg) (Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case ToolCallStartMsg:
		preview := msg.Input
		if r := []rune(preview); len(r) > 120 {
			preview = string(r[:120]) + "…"
		}
		slog.Info("tool call start", "tool", msg.ToolName, "id", msg.ID, "input", preview)
		m.chat.AddToolCall(msg.ID, msg.ToolName, msg.Input)
		return m, nil, true
	case ToolCallDoneMsg:
		slog.Info("tool call done", "id", msg.ID, "error", msg.IsError)
		m.chat.UpdateToolCall(msg.ID, msg.IsError, msg.Output)
		return m, nil, true
	case ConsoleMsg:
		if msg.Clear {
			m.console.Clear()
			m.consoleVisible = false
		}
		if msg.Line != "" {
			m.console.Append(msg.Line)
			m.consoleVisible = true
		}
		return m, nil, true
	}
	return m, nil, false
}

func (m Model) updateActions(msg tea.Msg) (Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case showToolsMsg:
		m.modalContent = m.chat.ToolLogModal()
		m.modalScroll = 0
		return m, nil, true

	case SubmitMsg:
		m.closeSuggestions()
		n, cmd := m.submitToAgent(msg.Content, msg.Display)
		return n.(Model), cmd, true

	case CommandMsg:
		if msg.Name == "help" {
			return m, func() tea.Msg { return ShowModalMsg{Text: m.commands.HelpText()} }, true
		}
		// Instant commands bypass WASM dispatch: invoke the handler directly
		// without setting the "queuing…" indicator. Built-in commands are always Instant.
		if cmd, ok := m.commands.Get(msg.Name); ok && cmd.Instant {
			return m, cmd.Handler(msg.Args), true
		}
		// Show a queuing indicator while the command is dispatched asynchronously
		// (e.g. skill commands go through WASM before submitToAgent is called).
		m.live.setStatus("stream", "queuing…")
		return m, m.commands.Dispatch(msg.Name, msg.Args), true

	case clearMsg:
		m.resetChatArea()
		m.streamContent = ""
		m.chat.ClearToolLog()
		return m, nil, true

	case setModelMsg:
		m.activeModel = msg.Model
		m.live.setModel(msg.Model)
		m.pushNotification(fmt.Sprintf("Model set to: %s", msg.Model))
		return m, nil, true

	case abortStreamMsg:
		if m.agentPool != nil {
			m.agentPool.CancelAll()
		}
		m.live.setStatus("stream", "cancelling…")
		return m, nil, true

	case dispatchOnCommandMsg:
		if m.extHost != nil {
			extHost := m.extHost
			return m, func() tea.Msg {
				payload, _ := json.Marshal(sdk.OnCommandPayload{Name: msg.Name, Args: msg.Args})
				evt := sdk.Event{Type: sdk.EventOnCommand, Payload: payload}
				results, err := extHost.DispatchEvent(context.Background(), evt)
				return ExtensionEventResultMsg{Results: results, Err: err}
			}, true
		}
		return m, nil, true
	}
	return m, nil, false
}

// updateExtension handles extension-related messages: ExtensionEventResultMsg, ReloadMsg, NotifyMsg, StatusUpdateMsg.
// Returns (model, cmd, true) when the message was handled.
func (m Model) updateExtension(msg tea.Msg) (Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case sessionStartDoneMsg:
		for _, r := range msg.Results {
			if r.Error != "" {
				m.pushNotification(fmt.Sprintf("Extension error: %s", r.Error))
			}
		}
		m.updateSuggestions()
		// Append the default action prompt after all session_start handlers have run,
		// so the tool and command lists reflect everything registered at startup.
		// Prepend the action prompt before AGENTS.md content so the directive
		// comes first in the system prompt — buried-at-the-end guidance is ignored.
		if m.agentPool != nil && m.extHost != nil {
			action := buildDefaultActionPrompt(m.extHost.GetRegisteredTools(), m.commands.List())
			existing := m.agentPool.BaseSystemPrompt()
			if existing == "" {
				m.agentPool.SetBaseSystemPrompt(action)
			} else {
				m.agentPool.SetBaseSystemPrompt(action + "\n\n" + existing)
			}
		}
		// Start the 1-second extension tick after session is ready.
		return m, tea.Tick(time.Second, func(time.Time) tea.Msg { return extensionTickMsg{} }), true

	case extensionTickMsg:
		cmds := []tea.Cmd{tea.Tick(time.Second, func(time.Time) tea.Msg { return extensionTickMsg{} })}
		if m.extHost != nil {
			extHost := m.extHost
			cmds = append(cmds, func() tea.Msg {
				results, _ := extHost.DispatchEvent(context.Background(), sdk.Event{Type: sdk.EventTick})
				return ExtensionEventResultMsg{Results: results}
			})
		}
		return m, tea.Batch(cmds...), true

	case ExtensionEventResultMsg:
		for _, r := range msg.Results {
			if r.Error != "" {
				m.pushNotification(fmt.Sprintf("Extension error: %s", r.Error))
			}
		}
		// Clear queuing… if streaming hasn't started yet (command didn't reach the LLM).
		if !m.streaming && m.live.getStatus("stream") == "queuing…" {
			m.live.setStatus("stream", "")
		}
		return m, nil, true

	case ReloadMsg:
		return m, m.cmdReloadExtensions(), true

	case NotifyMsg:
		m.pushNotification(msg.Text)
		return m, nil, true

	case StatusUpdateMsg:
		m.live.setStatus(msg.Key, msg.Value)
		if m.program != nil {
			m.program.Send(sceneDirtyMsg{})
		}
		return m, nil, true

	case sceneDirtyMsg:
		// The scene was mutated off-loop by the UI bridge. When the WASM-driven
		// chat is active, refresh the transcript viewport from the scene area;
		// otherwise this just forces a re-render.
		m.refreshWASMChat()
		return m, nil, true
	}
	return m, nil, false
}

// pushNotification shows a system notification line in the chat and dispatches
// EventNotify so extensions that own the transcript (WASM-driven chat) can
// render it. The dispatch runs in a goroutine so it never blocks the bubbletea
// loop; the SceneRenderer it ultimately mutates is goroutine-safe.
func (m *Model) pushNotification(text string) {
	if m.extHost == nil {
		return
	}
	extHost := m.extHost
	payload, _ := json.Marshal(sdk.NotifyPayload{Text: text})
	go func() {
		_, _ = extHost.DispatchEvent(context.Background(), sdk.Event{Type: sdk.EventNotify, Payload: payload})
	}()
}

// refreshWASMChat feeds the WASM-owned transcript scene area into the chat
// viewport. No-op until an extension creates the area (e.g. the agents
// extension on session start); until then the viewport is empty.
func (m *Model) refreshWASMChat() {
	if m.scene == nil || !m.scene.HasArea(wasmChatAreaID) {
		return
	}
	m.chat.SetExternalContent(m.scene.Render(wasmChatAreaID, m.chatWidth()))
}

// resetChatArea clears the WASM transcript scene area back to an empty root,
// matching the structure the extension expects ("chat-root" vstack). Used by
// /clear and history-restore, which are harness-initiated. No-op if the area
// does not exist.
func (m *Model) resetChatArea() {
	if m.scene == nil || !m.scene.HasArea(wasmChatAreaID) {
		return
	}
	_ = m.scene.ApplyPatch(sdk.UIPatchParams{Area: wasmChatAreaID, Ops: []sdk.UIPatchOp{
		{Op: sdk.UIOpSetRoot, Node: &sdk.UINode{ID: "chat-root", Type: sdk.UINodeVStack}},
	}})
	m.chat.SetExternalContent("")
}

// chatWidth returns the content width available to the chat viewport.
func (m *Model) chatWidth() int {
	if m.width > 0 {
		return m.width
	}
	return 80
}

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
	// The user prompt is echoed into the transcript by the WASM extension via
	// the before_agent_start event (dispatched below). Here we only manage
	// streaming state and the per-turn tool log.
	_ = chatText
	if !m.streaming {
		m.chat.ClearToolLog()
		m.streaming = true
	}
	m.streamStart = time.Now()
	m.live.setStreaming(true, m.streamStart, false)
	if content != "" && m.OnUserMessage != nil {
		m.OnUserMessage(content)
	}

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
		prompt := content
		if r := []rune(prompt); len(r) > 120 {
			prompt = string(r[:120]) + "…"
		}
		slog.Info("stream start", "prompt", prompt, "system_prompt_len", len(pool.BaseSystemPrompt()))
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

// cmdDispatchMessageEnd fires EventMessageEnd with the given role and content.
// Called after streaming completes so extensions (e.g. history) can record
// the assistant response.
func (m Model) cmdDispatchMessageEnd(role, content string) tea.Cmd {
	if m.extHost == nil {
		return nil
	}
	extHost := m.extHost
	return func() tea.Msg {
		payload, _ := json.Marshal(sdk.MessageEndPayload{Role: role, Content: content})
		evt := sdk.Event{Type: sdk.EventMessageEnd, Payload: payload}
		results, err := extHost.DispatchEvent(context.Background(), evt)
		return ExtensionEventResultMsg{Results: results, Err: err}
	}
}

func (m Model) cmdDispatchAfterProviderResponse() tea.Cmd {
	if m.extHost == nil {
		return nil
	}
	extHost := m.extHost
	// Snapshot current usage so the closure captures consistent values.
	var usageStats sdk.UsageStats
	if m.agentPool != nil {
		cu := m.agentPool.MainAgentContextUsage()
		usageStats = sdk.UsageStats{
			InputTokens:  int(cu.InputTokens),
			OutputTokens: int(cu.OutputTokens),
		}
	}
	return func() tea.Msg {
		payload, _ := json.Marshal(sdk.AfterProviderResponsePayload{Usage: usageStats})
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
	h := m.height - inputAreaHeight - m.statusLineHeight() - m.dropdownHeight() - m.consoleHeight()
	if h < 1 {
		h = 1
	}
	return h
}

// statusLineHeight returns the total number of lines consumed by all UIAreaStatus
// scene areas, constrained by each area's MinHeight/MaxHeight settings.
func (m Model) statusLineHeight() int {
	if m.scene == nil {
		return 0
	}
	total := 0
	for _, id := range m.scene.AreasByPlacement(sdk.UIAreaStatus) {
		w := m.scene.ConstrainWidth(id, m.chatWidth())
		rendered := m.scene.Render(id, w)
		lines := 0
		if rendered != "" {
			lines = strings.Count(rendered, "\n") + 1
		}
		total += m.scene.ConstrainHeight(id, lines, m.height)
	}
	return total
}

// renderStatusLine renders all UIAreaStatus scene areas as a contiguous block.
func (m Model) renderStatusLine() string {
	if m.scene == nil {
		return ""
	}
	var parts []string
	for _, id := range m.scene.AreasByPlacement(sdk.UIAreaStatus) {
		w := m.scene.ConstrainWidth(id, m.chatWidth())
		rendered := strings.TrimRight(m.scene.Render(id, w), "\n")
		if rendered == "" {
			continue
		}
		// Clamp to MaxHeight.
		rawLines := strings.Split(rendered, "\n")
		clamped := m.scene.ConstrainHeight(id, len(rawLines), m.height)
		if clamped < len(rawLines) {
			rawLines = rawLines[:clamped]
		}
		parts = append(parts, strings.Join(rawLines, "\n"))
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "\n")
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
	if m.picker.IsActive() {
		sb.WriteString(strings.TrimRight(m.picker.View(), "\n") + "\n")
	} else if m.modalContent != "" {
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
		if scenes := m.renderScenes(); scenes != "" {
			sb.WriteString(scenes)
			sb.WriteString("\n")
		}
		if dropdown := m.renderDropdown(); dropdown != "" {
			sb.WriteString(dropdown)
			sb.WriteString("\n")
		}
	}
	if m.consoleVisible && !m.console.IsEmpty() {
		sb.WriteString(m.renderConsole())
	}
	if sl := m.renderStatusLine(); sl != "" {
		sb.WriteString(sl + "\n")
	}
	sb.WriteString(inputBox)

	out := sb.String()
	if m.height > 0 {
		// Pad to exactly m.height lines so no old content bleeds through when
		// the viewport shrinks (e.g. dropdown appears/disappears).
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

// renderScenes renders every extension-driven scene area inline, stacked in
// creation order. P1 integration is intentionally minimal: areas render below
// the chat regardless of placement. Later phases composite by placement and
// move the chat transcript itself into a "main" scene area.
func (m Model) renderScenes() string {
	if m.scene == nil {
		return ""
	}
	width := m.width
	if width <= 0 {
		width = 80
	}
	var parts []string
	for _, placement := range []sdk.UIAreaPlacement{sdk.UIAreaMain, sdk.UIAreaSidebar, sdk.UIAreaOverlay} {
		for _, id := range m.scene.AreasByPlacement(placement) {
			// The transcript area is rendered inside the chat viewport, not
			// stacked here — skip it to avoid duplication.
			if id == wasmChatAreaID {
				continue
			}
			if r := strings.TrimRight(m.scene.Render(id, width), "\n"); r != "" {
				parts = append(parts, r)
			}
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "\n")
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

	fill := strings.Repeat("─", innerWidth)
	top := b.Render("╭" + fill + "╮")

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

const consolePaneLines = 10

func (m Model) consoleHeight() int {
	if !m.consoleVisible {
		return 0
	}
	return consolePaneLines
}

func (m Model) renderConsole() string {
	width := m.width
	if width < 20 {
		width = 20
	}
	innerWidth := width - 2
	b := lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))
	dimText := lipgloss.NewStyle().Foreground(lipgloss.Color("#555555"))
	label := "─ console "
	fillWidth := innerWidth - lipgloss.Width(label)
	if fillWidth < 0 {
		fillWidth = 0
	}
	fill := strings.Repeat("─", fillWidth)
	header := b.Render("╭" + label + fill + "╮")
	content := m.console.View(width-4, 8)
	lines := strings.Split(strings.TrimRight(content, "\n"), "\n")
	body := strings.Builder{}
	for _, line := range lines {
		visible := lipgloss.Width(line)
		pad := innerWidth - 2 - visible
		if pad < 0 {
			pad = 0
		}
		body.WriteString(b.Render("│") + " " + dimText.Render(line) + strings.Repeat(" ", pad) + " " + (b.Render("│") + "\n"))
	}
	footer := b.Render("╰" + strings.Repeat("─", innerWidth) + "╯")
	return header + "\n" + body.String() + footer + "\n"
}
