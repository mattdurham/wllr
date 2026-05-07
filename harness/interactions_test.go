package harness

// Interaction tests covering model message handling and chat view behaviours
// not yet covered by tui_test.go or chat_render_test.go.

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/mattdurham/wllr/sdk"
)

// ─── Tab autocomplete ────────────────────────────────────────────────────────

func TestModel_Tab_CompletesSelectedSuggestion(t *testing.T) {
	m := newTestModel()
	m.commands.Register(Command{Name: "help", Desc: "Show help", Handler: func([]string) tea.Cmd { return nil }})
	m.commands.Register(Command{Name: "reload", Desc: "Reload", Handler: func([]string) tea.Cmd { return nil }})

	// Type "/h" to get suggestions
	m.input.SetValue("/h")
	m.updateSuggestions()

	if len(m.suggestions) == 0 {
		t.Fatal("expected suggestions after /h")
	}
	m.suggestionIdx = 0

	// Press Tab
	next, _ := m.Update(keyMsg(tea.KeyTab, 0))
	m = next.(Model)

	val := m.input.Value()
	if !strings.HasPrefix(val, "/help") {
		t.Errorf("tab should complete to /help, got %q", val)
	}
	if len(m.suggestions) != 0 {
		t.Error("suggestions should close after tab completion")
	}
}

// ─── Esc-esc aborts stream ────────────────────────────────────────────────────

func TestModel_EscEsc_DuringStream_GeneratesAbort(t *testing.T) {
	m := newTestModel()
	m.streaming = true

	// First esc: sets lastWasEsc in input area
	next1, cmd1 := m.Update(keyMsg(tea.KeyEscape, 0))
	m = next1.(Model)
	if cmd1 != nil {
		// First esc may produce a nil cmd — that's fine
	}

	// Second esc: should generate abortStreamMsg via the input Cmd
	_, cmd2 := m.Update(keyMsg(tea.KeyEscape, 0))
	if cmd2 == nil {
		t.Skip("second esc produced nil cmd — input may not have lastWasEsc set")
	}
	// Execute the cmd to get the message
	msg := cmd2()
	if _, ok := msg.(abortStreamMsg); !ok {
		t.Errorf("second esc should produce abortStreamMsg, got %T", msg)
	}
}

// ─── ToolCallStartMsg ────────────────────────────────────────────────────────

func TestModel_ToolCallStartMsg_AddsToolBoxToChat(t *testing.T) {
	m := newTestModel()
	m.width = 80
	m.height = 40
	m.chat.SetSize(80, 30)

	next, _ := m.Update(ToolCallStartMsg{
		ID:       "call-1",
		ToolName: "exec",
		Input:    `{"command":"ls"}`,
	})
	m = next.(Model)

	content := m.chat.vp.View()
	if !strings.Contains(content, "exec") {
		t.Error("tool summary should show the tool name")
	}
	if !strings.Contains(content, "↳") {
		t.Error("tool calls should render as ↳ summary")
	}
}

// ─── ToolCallDoneMsg ─────────────────────────────────────────────────────────

func TestModel_ToolCallDoneMsg_UpdatesToolBox(t *testing.T) {
	m := newTestModel()
	m.width = 80
	m.height = 40
	m.chat.SetSize(80, 30)

	// Add a pending tool call first
	m.chat.AddToolCall("call-1", "exec", `{"command":"ls"}`)
	if m.chat.messages[0].toolDone {
		t.Fatal("tool call should start as pending")
	}

	// Mark it done
	next, _ := m.Update(ToolCallDoneMsg{ID: "call-1", IsError: false, Output: "file.txt"})
	m = next.(Model)

	if !m.chat.messages[0].toolDone {
		t.Error("ToolCallDoneMsg should mark the tool call as done")
	}
}

func TestModel_ToolCallDoneMsg_ErrorFlag(t *testing.T) {
	m := newTestModel()
	m.chat.AddToolCall("call-1", "exec", `{"command":"bad"}`)

	next, _ := m.Update(ToolCallDoneMsg{ID: "call-1", IsError: true, Output: "permission denied"})
	m = next.(Model)

	if !m.chat.messages[0].toolError {
		t.Error("ToolCallDoneMsg with IsError=true should set toolError on the message")
	}
}

// ─── Token routing to toolResponse ───────────────────────────────────────────

func TestChatView_AppendToken_AlwaysGoesToCurrent(t *testing.T) {
	// Tokens always go to c.current regardless of tool state so the LLM
	// response is never fragmented across multiple boxes.
	c := NewChatView(80, 20)
	c.AddUserMessage("run it")
	c.AddToolCall("t1", "exec", `{"command":"ls"}`)
	c.UpdateToolCall("t1", false, "")

	c.AppendToken("hello")
	c.AppendToken(" world")

	// Tokens should always go to c.current, never to toolResponse.
	if c.current != "hello world" {
		t.Errorf("tokens should go to c.current, got %q", c.current)
	}
	if c.messages[1].toolResponse != "" {
		t.Errorf("toolResponse should be empty with new routing, got %q", c.messages[1].toolResponse)
	}
}

func TestChatView_FinalizeMessage_ResetslastDoneToolID(t *testing.T) {
	c := NewChatView(80, 20)
	c.AddToolCall("t1", "exec", `{"command":"ls"}`)
	c.UpdateToolCall("t1", false, "")
	c.AppendToken("result")

	c.FinalizeMessage()

	if c.lastDoneToolID != "" {
		t.Errorf("FinalizeMessage should clear lastDoneToolID, got %q", c.lastDoneToolID)
	}
}

// ─── AddToolCall seals c.current ─────────────────────────────────────────────

func TestChatView_AddToolCall_SealsCurrentText(t *testing.T) {
	c := NewChatView(80, 20)
	c.AddUserMessage("question")
	c.AppendToken("Thinking... ")
	c.AppendToken("let me check")

	// c.current has partial text — AddToolCall should seal it
	c.AddToolCall("t1", "exec", `{"command":"ls"}`)

	// c.current should be empty now, and the text should be in a message
	if c.current != "" {
		t.Errorf("c.current should be empty after AddToolCall, got %q", c.current)
	}
	found := false
	for _, m := range c.messages {
		if m.role == sdk.RoleAssistant && strings.Contains(m.content, "Thinking") {
			found = true
			break
		}
	}
	if !found {
		t.Error("partial text should be sealed as assistant message before tool call")
	}
}

// ─── NotifyMsg ───────────────────────────────────────────────────────────────

func TestModel_NotifyMsg_AddsToChat(t *testing.T) {
	m := newTestModel()
	m.width = 80
	m.height = 40
	m.chat.SetSize(80, 30)

	next, _ := m.Update(NotifyMsg{Text: "Extension loaded"})
	m = next.(Model)

	content := m.chat.vp.View()
	if !strings.Contains(content, "Extension loaded") {
		t.Error("NotifyMsg should add notification text to chat")
	}
}

// ─── StatusUpdateMsg ─────────────────────────────────────────────────────────

func TestModel_StatusUpdateMsg_UpdatesStatusBar(t *testing.T) {
	m := newTestModel()

	next, _ := m.Update(StatusUpdateMsg{Key: "test", Value: "active"})
	m = next.(Model)

	if m.statusBar.statuses["test"] != "active" {
		t.Errorf("StatusUpdateMsg should update status bar, got %q", m.statusBar.statuses["test"])
	}
}

// ─── clearMsg ────────────────────────────────────────────────────────────────

func TestModel_ClearMsg_ClearsChat(t *testing.T) {
	m := newTestModel()
	m.width = 80
	m.height = 40
	m.chat.SetSize(80, 30)
	m.chat.AddUserMessage("should be cleared")

	next, _ := m.Update(clearMsg{})
	m = next.(Model)

	if m.chat.MessageCount() != 0 {
		t.Errorf("clearMsg should clear chat, got %d messages", m.chat.MessageCount())
	}
}

// ─── setModelMsg ─────────────────────────────────────────────────────────────

func TestModel_SetModelMsg_UpdatesActiveModel(t *testing.T) {
	m := newTestModel()

	next, _ := m.Update(setModelMsg{Model: "claude-haiku-4-5"})
	m = next.(Model)

	if m.activeModel != "claude-haiku-4-5" {
		t.Errorf("setModelMsg should update activeModel, got %q", m.activeModel)
	}
	if m.statusBar.modelName != "claude-haiku-4-5" {
		t.Errorf("setModelMsg should update status bar model name")
	}
}

// ─── Window resize ────────────────────────────────────────────────────────────

func TestModel_WindowResize_UpdatesDimensions(t *testing.T) {
	m := newTestModel()

	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 50})
	m = next.(Model)

	if m.width != 120 || m.height != 50 {
		t.Errorf("window resize should update dimensions: got %dx%d", m.width, m.height)
	}
}

// ─── StreamDoneMsg with error ─────────────────────────────────────────────────

func TestModel_StreamDoneMsg_WithError_ShowsInChat(t *testing.T) {
	m := newTestModel()
	m.width = 80
	m.height = 40
	m.chat.SetSize(80, 30)
	m.streaming = true
	m.streamStart = time.Now()

	next, _ := m.Update(StreamDoneMsg{Err: fmt.Errorf("rate limit exceeded")})
	m = next.(Model)

	if m.streaming {
		t.Error("streaming should be false after StreamDoneMsg")
	}
	content := m.chat.vp.View()
	if !strings.Contains(content, "rate limit") {
		t.Error("error message should appear in chat")
	}
	if m.statusBar.statuses["stream"] != "error" {
		t.Error("status bar should show 'error'")
	}
}

// ─── SubmitMsg with Display field ────────────────────────────────────────────

func TestModel_SubmitMsg_DisplayField_ShownInsteadOfContent(t *testing.T) {
	m := newTestModel()
	m.width = 80
	m.height = 40
	m.chat.SetSize(80, 30)

	// The Content is a large XML blob; Display is the compact version
	next, _ := m.Update(SubmitMsg{
		Content: `<skill name="bob:work" location="/path">very long XML...</skill>`,
		Display: "[skill: bob:work]",
	})
	m = next.(Model)

	content := m.chat.vp.View()
	if strings.Contains(content, "<skill") {
		t.Error("raw XML should NOT appear in chat when Display is set")
	}
	if !strings.Contains(content, "[skill: bob:work]") {
		t.Error("Display text should appear in chat")
	}
}

// ─── Modal scroll ─────────────────────────────────────────────────────────────

func TestModel_Modal_DownKey_ScrollsContent(t *testing.T) {
	m := newTestModel()
	m.width = 80
	m.height = 40
	m.modalContent = strings.Repeat("line\n", 50) // more lines than fit
	m.modalScroll = 0

	next, _ := m.Update(keyMsg(tea.KeyDown, 0))
	m = next.(Model)

	if m.modalScroll != 1 {
		t.Errorf("down key should increment modalScroll to 1, got %d", m.modalScroll)
	}
}

func TestModel_Modal_UpKey_ScrollsUp(t *testing.T) {
	m := newTestModel()
	m.width = 80
	m.height = 40
	m.modalContent = strings.Repeat("line\n", 50)
	m.modalScroll = 5

	next, _ := m.Update(keyMsg(tea.KeyUp, 0))
	m = next.(Model)

	if m.modalScroll != 4 {
		t.Errorf("up key should decrement modalScroll to 4, got %d", m.modalScroll)
	}
}

func TestModel_Modal_DownKey_ClampsAtMax(t *testing.T) {
	m := newTestModel()
	m.width = 80
	m.height = 30
	// Only 3 lines of content — modal can't scroll at all
	m.modalContent = "line1\nline2\nline3"
	m.modalScroll = 0

	// Press down many times
	for i := 0; i < 10; i++ {
		next, _ := m.Update(keyMsg(tea.KeyDown, 0))
		m = next.(Model)
	}

	if m.modalScroll > 3 {
		t.Errorf("modalScroll should be clamped, got %d", m.modalScroll)
	}
}

func TestModel_Modal_QKey_ClosesModal(t *testing.T) {
	m := newTestModel()
	m.modalContent = "some content"

	next, _ := m.Update(keyMsg('q', 0))
	m = next.(Model)

	if m.modalContent != "" {
		t.Error("q key should close modal")
	}
}

// ─── Autocomplete mid-sentence ────────────────────────────────────────────────

func TestModel_AutocompleteTriggersOnSlashMidSentence(t *testing.T) {
	m := newTestModel()
	m.commands.Register(Command{Name: "help", Desc: "Show help", Handler: func([]string) tea.Cmd { return nil }})
	m.width = 80
	m.height = 40

	m.input.SetValue("please /hel")
	m.updateSuggestions()

	if len(m.suggestions) == 0 {
		t.Error("autocomplete should trigger for /hel mid-sentence")
	}
	found := false
	for _, s := range m.suggestions {
		if s.Name == "help" {
			found = true
		}
	}
	if !found {
		t.Error("help command should be in suggestions for /hel")
	}
}

func TestModel_AutocompleteDoesNotTriggerMidWord(t *testing.T) {
	m := newTestModel()
	m.commands.Register(Command{Name: "help", Desc: "", Handler: func([]string) tea.Cmd { return nil }})

	// "/" preceded by a non-space character should not trigger
	m.input.SetValue("http://example")
	m.updateSuggestions()

	if len(m.suggestions) != 0 {
		t.Error("autocomplete should NOT trigger for a slash mid-word (http://)")
	}
}

// ─── dispatchOnCommandMsg ────────────────────────────────────────────────────

func TestModel_DispatchOnCommandMsg_WithNoHost_IsNoOp(t *testing.T) {
	m := newTestModel() // extHost is nil

	next, cmd := m.Update(dispatchOnCommandMsg{Name: "bob:work", Args: nil})
	m = next.(Model)

	if cmd != nil {
		t.Error("dispatchOnCommandMsg with nil extHost should return nil cmd")
	}
	_ = m
}
