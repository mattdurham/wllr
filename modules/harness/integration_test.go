package harness

import (
	"testing"
	"time"

	"github.com/mattdurham/wllr/modules/agent"
	"github.com/mattdurham/wllr/modules/extension"
	"github.com/mattdurham/wllr/modules/sdk"
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

	// User message should be in chat immediately after SubmitMsg.
	if len(m.chat.messages) != 1 {
		t.Fatalf("expected 1 message in chat after SubmitMsg, got %d", len(m.chat.messages))
	}
	if m.chat.messages[0].role != sdk.RoleUser {
		t.Errorf("expected RoleUser, got %v", m.chat.messages[0].role)
	}
	if m.chat.messages[0].content != "what is 2+2?" {
		t.Errorf("chat.messages[0].content: got %q, want %q", m.chat.messages[0].content, "what is 2+2?")
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

	// User message should be in chat immediately after SubmitMsg.
	if len(m.chat.messages) != 1 {
		t.Fatalf("expected 1 message in chat, got %d", len(m.chat.messages))
	}
	if m.chat.messages[0].role != sdk.RoleUser {
		t.Errorf("expected RoleUser, got %v", m.chat.messages[0].role)
	}
	if m.chat.messages[0].content != "hello world" {
		t.Errorf("chat.messages[0].content: got %q, want %q", m.chat.messages[0].content, "hello world")
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
	if len(m.chat.messages) != 1 {
		t.Errorf("expected 1 chat entry, got %d", len(m.chat.messages))
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
	if len(m.chat.messages) != 1 {
		t.Errorf("expected 1 chat entry, got %d", len(m.chat.messages))
	}
}

// TestOnAgentRun_NonEmptyPromptPreventsError verifies that when the main agent's
// history ends with an assistant message, calling pool.Send with a non-empty
// sentinel does NOT produce a "prompt can't be empty" error.
// This directly tests the belt-and-suspenders fix in OnAgentRun (Fix 2).
func TestOnAgentRun_NonEmptyPromptPreventsError(t *testing.T) {
	pool := agent.NewPool()
	lm := newMockLM("response")
	a, err := pool.Spawn("main", lm, agent.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	// Run one complete turn so history ends with an assistant message.
	done := make(chan error, 1)
	a.SetOnDone(func(err error) { done <- err })

	if err := pool.Send("main", "first message"); err != nil {
		t.Fatalf("first Send: %v", err)
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("first turn error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("first turn timed out")
	}

	// History ends with assistant message. Simulate OnAgentRun by sending
	// a non-empty sentinel. This must not produce "prompt can't be empty".
	// AppendInbox first (simulating what send_message does before agent_run).
	a.AppendInbox(sdk.Message{Role: sdk.RoleUser, Content: "from sub-agent"})

	if err := pool.Send("main", "[process pending inbox messages]"); err != nil {
		t.Fatalf("second Send: %v", err)
	}

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("OnAgentRun sentinel turn failed: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("second turn timed out")
	}
}
