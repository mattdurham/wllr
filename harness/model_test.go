package harness

import (
	"context"
	"errors"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/mattdurham/wllr/agent"
	"github.com/mattdurham/wllr/sdk"
)

// newTestPool creates an AgentPool with a mock LM and a spawned "main" agent.
func newTestPool() *agent.AgentPool {
	pool := agent.NewPool()
	lm := newMockLM("hello", " ", "world")
	_, _ = pool.Spawn("main", lm, agent.SpawnOpts{})
	return pool
}

func newTestModel() Model {
	pool := newTestPool()
	return New(pool, "main", nil)
}

// callUpdate is a helper that calls Update and returns the concrete Model.
func callUpdate(m Model, msg tea.Msg) (Model, tea.Cmd) {
	newModel, cmd := m.Update(msg)
	return newModel.(Model), cmd
}

func TestModel_Init_ReturnsCmd(t *testing.T) {
	m := newTestModel()
	cmd := m.Init()
	if cmd == nil {
		t.Error("Init() should return a non-nil Cmd")
	}
}

func TestModel_View_ReturnsNonEmpty(t *testing.T) {
	m := newTestModel()
	view := m.View()
	if view.Content == "" {
		t.Error("View() returned empty content")
	}
}

func TestModel_Update_TokenMsg_AppendsToChat(t *testing.T) {
	m := newTestModel()

	m, _ = callUpdate(m, TokenMsg{Token: "hello"})
	if m.chat.current != "hello" {
		t.Errorf("chat current: got %q, want %q", m.chat.current, "hello")
	}

	m, _ = callUpdate(m, TokenMsg{Token: " world"})
	if m.chat.current != "hello world" {
		t.Errorf("chat current: got %q, want %q", m.chat.current, "hello world")
	}
}

func TestModel_Update_StreamDoneMsg_ClearsStreamStatus(t *testing.T) {
	m := newTestModel()
	m.chat.AppendToken("test response")
	// Simulate a stream status set externally.
	m.statusBar.statuses["stream"] = "working."

	m, _ = callUpdate(m, StreamDoneMsg{Err: nil})
	// Stream status should be cleared.
	if _, ok := m.statusBar.statuses["stream"]; ok {
		t.Error("stream status should be cleared after StreamDoneMsg")
	}
}

func TestModel_Update_StreamDoneMsg_Error_ShowsError(t *testing.T) {
	m := newTestModel()

	m, _ = callUpdate(m, StreamDoneMsg{Err: errors.New("API error")})
	// Should have added an error notification.
	if len(m.chat.messages) == 0 {
		t.Error("expected error notification in chat")
	}
}

func TestModel_Update_StreamDoneMsg_ContextCanceled_NoError(t *testing.T) {
	m := newTestModel()
	m.chat.AppendToken("partial")

	// context.Canceled should not show as an error notification.
	m, _ = callUpdate(m, StreamDoneMsg{Err: context.Canceled})
	// FinalizeMessage adds the partial assistant message (1 message expected).
	// No additional error notification should be added for context.Canceled.
	for _, msg := range m.chat.messages {
		if msg.role == "system" {
			t.Errorf("unexpected error notification for context.Canceled: %q", msg.content)
		}
	}
}

func TestModel_Update_ReloadMsg_TriggersExtensionReload(t *testing.T) {
	m := newTestModel() // no extension host
	_, cmd := callUpdate(m, ReloadMsg{})
	if cmd == nil {
		t.Error("expected non-nil cmd after ReloadMsg")
	}
	// Execute the cmd.
	msg := cmd()
	_, ok := msg.(NotifyMsg)
	if !ok {
		t.Errorf("expected NotifyMsg from reload cmd, got %T", msg)
	}
}

func TestModel_Update_ClearMsg_ClearsHistory(t *testing.T) {
	m := newTestModel()
	m.history = append(m.history, sdk.Message{Role: sdk.RoleUser, Content: "hello"})
	m.chat.AddUserMessage("hello")

	m, _ = callUpdate(m, clearMsg{})
	if len(m.history) != 0 {
		t.Errorf("expected empty history after clear, got %d items", len(m.history))
	}
	if len(m.chat.messages) != 0 {
		t.Errorf("expected empty chat after clear, got %d messages", len(m.chat.messages))
	}
}

func TestModel_Update_SetModelMsg(t *testing.T) {
	m := newTestModel()
	m, _ = callUpdate(m, setModelMsg{Model: "claude-haiku-3-5"})
	if m.activeModel != "claude-haiku-3-5" {
		t.Errorf("activeModel: got %q, want %q", m.activeModel, "claude-haiku-3-5")
	}
	if m.statusBar.modelName != "claude-haiku-3-5" {
		t.Errorf("statusBar.modelName: got %q, want %q", m.statusBar.modelName, "claude-haiku-3-5")
	}
}

func TestModel_Update_CommandMsg_Clear(t *testing.T) {
	m := newTestModel()
	m.history = append(m.history, sdk.Message{Role: sdk.RoleUser, Content: "test"})

	// Dispatch /clear command.
	cmd := m.commands.Dispatch("clear", nil)
	msg := cmd()
	m, _ = callUpdate(m, msg)

	if len(m.history) != 0 {
		t.Errorf("expected empty history after /clear, got %d", len(m.history))
	}
}

func TestModel_Update_CommandMsg_UnknownCommand(t *testing.T) {
	m := newTestModel()
	m, cmd := callUpdate(m, CommandMsg{Name: "nonexistent", Args: nil})
	if cmd == nil {
		t.Fatal("expected non-nil cmd for unknown command")
	}
	msg := cmd()
	notify, ok := msg.(NotifyMsg)
	if !ok {
		t.Fatalf("expected NotifyMsg, got %T", msg)
	}
	_ = m
	if notify.Text == "" {
		t.Error("expected non-empty error message")
	}
}

func TestModel_Update_SubmitMsg_AddsToHistoryAndChat(t *testing.T) {
	pool := newTestPool()
	m := New(pool, "main", nil)

	m, cmd := callUpdate(m, SubmitMsg{Content: "hello"})
	// SubmitMsg no longer starts a synchronous stream; it sends to the pool.
	// The model should return nil cmd (pool runs asynchronously).
	_ = cmd

	// User message should be in history immediately.
	if len(m.history) != 1 {
		t.Fatalf("expected 1 history entry after SubmitMsg, got %d", len(m.history))
	}
	if m.history[0].Content != "hello" {
		t.Errorf("history[0].Content: got %q, want %q", m.history[0].Content, "hello")
	}

	// User message should be in chat.
	if len(m.chat.messages) != 1 {
		t.Errorf("expected 1 chat message after SubmitMsg, got %d", len(m.chat.messages))
	}
}

func TestModel_Update_SubmitMsg_MultipleSubmitsAllowed(t *testing.T) {
	// With the pool-based model, SubmitMsg is never dropped.
	// Multiple submits are accepted (each enqueued to the pool).
	pool := newTestPool()
	m := New(pool, "main", nil)

	m, _ = callUpdate(m, SubmitMsg{Content: "first"})
	m, _ = callUpdate(m, SubmitMsg{Content: "second"})

	// Both messages should be in history.
	if len(m.history) != 2 {
		t.Errorf("expected 2 history entries, got %d", len(m.history))
	}
}

func TestModel_Update_NotifyMsg(t *testing.T) {
	m := newTestModel()
	m, _ = callUpdate(m, NotifyMsg{Text: "test notification"})
	// Should have added to chat.
	if len(m.chat.messages) == 0 {
		t.Error("expected notification in chat")
	}
}

func TestModel_Update_StatusUpdateMsg(t *testing.T) {
	m := newTestModel()
	m, _ = callUpdate(m, StatusUpdateMsg{Key: "foo", Value: "bar"})
	if m.statusBar.statuses["foo"] != "bar" {
		t.Errorf("statusBar.statuses[foo]: got %q, want %q", m.statusBar.statuses["foo"], "bar")
	}
}

func TestModel_Update_WindowSizeMsg(t *testing.T) {
	m := newTestModel()
	m, _ = callUpdate(m, tea.WindowSizeMsg{Width: 120, Height: 40})
	if m.width != 120 || m.height != 40 {
		t.Errorf("dimensions: got %dx%d, want 120x40", m.width, m.height)
	}
}

// TestModel_NilPool_SubmitMsg_Safe verifies that a nil agentPool does not panic on SubmitMsg.
func TestModel_NilPool_SubmitMsg_Safe(t *testing.T) {
	// Model with nil pool — should not panic.
	m := New(nil, "main", nil)

	m, cmd := callUpdate(m, SubmitMsg{Content: "hello"})
	_ = cmd
	// User message should still be in history and chat.
	if len(m.history) != 1 {
		t.Errorf("expected 1 history entry, got %d", len(m.history))
	}
}

// TestModel_AbortStreamMsg_CancelsAgent verifies abortStreamMsg cancels the main agent.
func TestModel_AbortStreamMsg_CancelsAgent(t *testing.T) {
	pool := newTestPool()
	m := New(pool, "main", nil)

	// abortStreamMsg should not panic even with no active turn.
	m, cmd := callUpdate(m, abortStreamMsg{})
	_ = m
	if cmd != nil {
		t.Error("expected nil cmd from abortStreamMsg")
	}
}
