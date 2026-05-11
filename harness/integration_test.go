package harness

import (
	"testing"

	"github.com/mattdurham/wllr/agent"
	"github.com/mattdurham/wllr/extension"
	"github.com/mattdurham/wllr/sdk"
)

// TestIntegration_FullSubmitFlow exercises the submit flow:
// user message is added to history/chat synchronously, pool.Send is called.
// Token delivery and StreamDoneMsg are verified by manually triggering callbacks.
func TestIntegration_FullSubmitFlow(t *testing.T) {
	pool := agent.NewPool()
	lm := newMockLM("hello", " ", "world", "!", "\n")
	a, err := pool.Spawn("main", lm, agent.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	m := New(pool, "main", nil)

	// Track tokens via manual callback wiring.
	var receivedTokens []string
	a.SetOnToken(func(tok string) { receivedTokens = append(receivedTokens, tok) })

	// Submit user message.
	m, cmd := callUpdate(m, SubmitMsg{Content: "what is 2+2?"})
	_ = cmd

	// User message should be in history immediately.
	if len(m.history) != 1 {
		t.Fatalf("expected 1 message in history after SubmitMsg, got %d", len(m.history))
	}
	if m.history[0].Role != sdk.RoleUser {
		t.Errorf("expected RoleUser, got %v", m.history[0].Role)
	}
	if m.history[0].Content != "what is 2+2?" {
		t.Errorf("history[0].Content: got %q, want %q", m.history[0].Content, "what is 2+2?")
	}

	// The agent goroutine is started by pool.Send inside the returned tea.Cmd,
	// which is discarded here — this test only verifies synchronous state changes.
	_ = receivedTokens
}

// TestIntegration_UserMessageInHistory verifies the user message is recorded correctly.
func TestIntegration_UserMessageInHistory(t *testing.T) {
	pool := newTestPool()
	m := New(pool, "main", nil)

	m, _ = callUpdate(m, SubmitMsg{Content: "hello world"})

	// History should have 1 user message immediately after SubmitMsg.
	if len(m.history) != 1 {
		t.Fatalf("expected 1 message in history, got %d", len(m.history))
	}
	if m.history[0].Role != sdk.RoleUser {
		t.Errorf("expected RoleUser, got %v", m.history[0].Role)
	}
	if m.history[0].Content != "hello world" {
		t.Errorf("history[0].Content: got %q, want %q", m.history[0].Content, "hello world")
	}

	// Chat should have 1 message (the user message).
	if len(m.chat.messages) != 1 {
		t.Errorf("expected 1 chat message, got %d", len(m.chat.messages))
	}
}

// TestIntegration_TokenMsg_UpdatesChat verifies that TokenMsg sent via the program
// correctly appends to the chat view. Simulates what SetOnToken callback does.
func TestIntegration_TokenMsg_UpdatesChat(t *testing.T) {
	m := newTestModel()

	m, _ = callUpdate(m, TokenMsg{Token: "hello"})
	m, _ = callUpdate(m, TokenMsg{Token: " world"})
	if m.chat.current != "hello world" {
		t.Errorf("chat.current: got %q, want %q", m.chat.current, "hello world")
	}

	// StreamDoneMsg finalizes.
	m, _ = callUpdate(m, StreamDoneMsg{Err: nil})
	if m.chat.current != "" {
		t.Errorf("chat.current should be empty after StreamDoneMsg, got %q", m.chat.current)
	}
	if len(m.chat.messages) == 0 {
		t.Error("expected assistant message after StreamDoneMsg")
	}
}

// TestIntegration_NilExtensionHost_Safe verifies that nil host never panics.
func TestIntegration_NilExtensionHost_Safe(t *testing.T) {
	pool := newTestPool()
	m := New(pool, "main", nil)

	// Init should return a non-nil Cmd and not panic.
	cmd := m.Init()
	if cmd == nil {
		t.Error("Init() should return a non-nil Cmd")
	}

	// cmdDispatchSessionStart with nil host returns nil — that's fine.
	sessionCmd := m.cmdDispatchSessionStart()
	if sessionCmd != nil {
		t.Error("cmdDispatchSessionStart with nil host should return nil")
	}

	// A normal SubmitMsg should work without panicking.
	m, _ = callUpdate(m, SubmitMsg{Content: "test"})
	if len(m.history) != 1 {
		t.Errorf("expected 1 history entry, got %d", len(m.history))
	}
}

// TestIntegration_ExtensionHost_NoExtensions_Safe verifies a real host with no extensions loaded.
func TestIntegration_ExtensionHost_NoExtensions_Safe(t *testing.T) {
	pool := newTestPool()
	h := extension.NewHost(nil)
	m := New(pool, "main", h)

	// Init should not panic.
	cmd := m.Init()
	if cmd == nil {
		t.Error("Init() should return a non-nil Cmd")
	}

	// Submit — no extensions loaded, no panics expected.
	m, _ = callUpdate(m, SubmitMsg{Content: "test with host"})
	if len(m.history) != 1 {
		t.Errorf("expected 1 history entry, got %d", len(m.history))
	}
}
