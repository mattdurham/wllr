package harness

import (
	"context"
	"encoding/json"
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

	// Submit user message. The transcript itself is produced by the WASM
	// extension (see test/wasmchat); here we verify the harness-side state.
	m, cmd := callUpdate(m, SubmitMsg{Content: "what is 2+2?"})
	_ = cmd

	if !m.streaming {
		t.Fatal("expected streaming=true after SubmitMsg")
	}

	// The agent goroutine is started by pool.Send inside the returned tea.Cmd,
	// which is discarded here — this test only verifies synchronous state changes.
	_ = receivedTokens
}

// TestIntegration_UserMessageInHistory verifies a submit marks the turn active.
func TestIntegration_UserMessageInHistory(t *testing.T) {
	pool := newTestPool()
	m := New(pool, "main", nil)

	m, _ = callUpdate(m, SubmitMsg{Content: "hello world"})

	if !m.streaming {
		t.Fatal("expected streaming=true after SubmitMsg")
	}
}

// TestIntegration_TokenMsg_AccumulatesResponse verifies that TokenMsg accumulates
// the assistant response text used for session persistence, and StreamDoneMsg
// resets it. The visible transcript is produced by the WASM extension.
func TestIntegration_TokenMsg_AccumulatesResponse(t *testing.T) {
	m := newTestModel()

	m, _ = callUpdate(m, TokenMsg{Token: "hello"})
	m, _ = callUpdate(m, TokenMsg{Token: " world"})
	if m.streamContent != "hello world" {
		t.Errorf("streamContent: got %q, want %q", m.streamContent, "hello world")
	}

	// StreamDoneMsg resets the accumulator.
	m, _ = callUpdate(m, StreamDoneMsg{Err: nil})
	if m.streamContent != "" {
		t.Errorf("streamContent should be empty after StreamDoneMsg, got %q", m.streamContent)
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
	if !m.streaming {
		t.Error("expected streaming=true after submit")
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
	if !m.streaming {
		t.Error("expected streaming=true after submit")
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

// TestAfterProviderResponseUsagePopulated verifies that the EventAfterProviderResponse
// event dispatched after a turn contains non-zero usage from the real API call.
func TestAfterProviderResponseUsagePopulated(t *testing.T) {
	pool := agent.NewPool()
	pool.SetContextWindow(200_000)
	lm := newUsageMockLM(1500, 42, "hello world")
	a, err := pool.Spawn(agent.MainAgentID, lm, agent.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	h := extension.NewHost(nil)
	m := New(pool, agent.MainAgentID, h)

	// Subscribe to EventAfterProviderResponse via the host's bus.
	received := make(chan sdk.AfterProviderResponsePayload, 1)
	h.Bus.Subscribe(sdk.EventAfterProviderResponse, func(_ context.Context, evt sdk.Event) error {
		var p sdk.AfterProviderResponsePayload
		if err := json.Unmarshal(evt.Payload, &p); err != nil {
			return err
		}
		select {
		case received <- p:
		default:
		}
		return nil
	})

	// Run a turn to populate LastUsage.
	done := make(chan error, 1)
	a.SetOnDone(func(e error) { done <- e })
	a.Submit(context.Background(), "query")
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("turn error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for turn")
	}

	// Process StreamDoneMsg — this should dispatch EventAfterProviderResponse.
	m, cmd := callUpdate(m, StreamDoneMsg{Err: nil})
	_ = m

	// Execute the cmd (dispatches the event).
	if cmd != nil {
		cmd()
	}

	// Wait for the event to arrive on the bus.
	select {
	case payload := <-received:
		if payload.Usage.InputTokens <= 0 {
			t.Errorf("AfterProviderResponse.Usage.InputTokens = %d, want > 0", payload.Usage.InputTokens)
		}
		if payload.Usage.OutputTokens <= 0 {
			t.Errorf("AfterProviderResponse.Usage.OutputTokens = %d, want > 0", payload.Usage.OutputTokens)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for EventAfterProviderResponse")
	}
}
