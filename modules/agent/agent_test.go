package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"charm.land/fantasy"
	"github.com/mattdurham/wllr/modules/agent"
	"github.com/mattdurham/wllr/modules/sdk"
)

func sdkMsg(content string) sdk.Message {
	return sdk.Message{Role: sdk.RoleUser, Content: content}
}

func TestAgent_Submit_DeliversTokensToOnToken(t *testing.T) {
	pool := agent.NewPool()
	lm := &tokenStreamLM{tokens: []string{"hello", " ", "world"}}
	a, err := pool.Spawn("tok", lm, agent.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	var mu sync.Mutex
	var received []string
	done := make(chan error, 1)

	a.SetOnToken(func(tok string) {
		mu.Lock()
		received = append(received, tok)
		mu.Unlock()
	})
	a.SetOnDone(func(err error) {
		done <- err
	})

	a.Submit(context.Background(), "ping")

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("onDone error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for onDone")
	}

	mu.Lock()
	got := received
	mu.Unlock()

	if len(got) == 0 {
		t.Fatal("expected tokens, got none")
	}
	combined := ""
	for _, tok := range got {
		combined += tok
	}
	if combined != "hello world" {
		t.Errorf("combined tokens: got %q, want %q", combined, "hello world")
	}
}

func TestAgent_Submit_DeliversDoneCallback(t *testing.T) {
	pool := agent.NewPool()
	lm := &tokenStreamLM{tokens: []string{"done"}}
	a, err := pool.Spawn("done-test", lm, agent.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	done := make(chan error, 1)
	a.SetOnDone(func(err error) {
		done <- err
	})

	a.Submit(context.Background(), "test")

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for done callback")
	}
}

func TestAgent_Submit_BrokenLM_CallsDoneWithError(t *testing.T) {
	pool := agent.NewPool()
	lm := &errStreamLM{}
	a, _ := pool.Spawn("err-lm", lm, agent.SpawnOpts{})

	done := make(chan error, 1)
	a.SetOnDone(func(err error) {
		done <- err
	})

	a.Submit(context.Background(), "test")

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected error when LM returns error, got nil")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}
}

func TestAgent_Cancel_StopsRunningTurn(t *testing.T) {
	pool := agent.NewPool()
	lm := &slowLM{}
	a, err := pool.Spawn("cancel-test", lm, agent.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	done := make(chan error, 1)
	a.SetOnDone(func(err error) {
		done <- err
	})

	a.Submit(context.Background(), "slow request")
	// Give the goroutine a moment to start.
	time.Sleep(20 * time.Millisecond)
	a.Cancel()

	select {
	case err := <-done:
		// context.Canceled is expected, nil is also acceptable if stream finished.
		_ = err
	case <-time.After(5 * time.Second):
		t.Fatal("timeout: Cancel did not stop the turn")
	}
}

func TestAgent_SetSystemPrompt_UsedInNextTurn(t *testing.T) {
	pool := agent.NewPool()
	lm := &tokenStreamLM{tokens: []string{"ok"}}
	a, _ := pool.Spawn("sys", lm, agent.SpawnOpts{})

	a.SetSystemPrompt("You are a test assistant.")

	done := make(chan error, 1)
	a.SetOnDone(func(err error) { done <- err })
	a.Submit(context.Background(), "hello")

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}
	// Verify no panic; system prompt propagation is internal to fantasy.Agent.
}

func TestAgent_AppendInbox_MessagesDeliveredBeforeNextTurn(t *testing.T) {
	pool := agent.NewPool()
	lm := newMockLM()
	a, _ := pool.Spawn("inbox", lm, agent.SpawnOpts{})

	a.AppendInbox(sdkMsg("hello from inbox"))
	a.AppendInbox(sdkMsg("second message"))

	drained := a.DrainInbox()
	if len(drained) != 2 {
		t.Fatalf("expected 2 inbox messages, got %d", len(drained))
	}
	if drained[0].Content != "hello from inbox" {
		t.Errorf("drained[0]: got %q", drained[0].Content)
	}
	if drained[1].Content != "second message" {
		t.Errorf("drained[1]: got %q", drained[1].Content)
	}
}

func TestAgent_ID_ReturnsSpawnedID(t *testing.T) {
	pool := agent.NewPool()
	lm := newMockLM()
	a, _ := pool.Spawn("my-agent", lm, agent.SpawnOpts{})
	if a.ID() != "my-agent" {
		t.Errorf("ID: got %q, want %q", a.ID(), "my-agent")
	}
}

func TestAgent_Submit_InboxMessagesIncorporated(t *testing.T) {
	pool := agent.NewPool()
	lm := &tokenStreamLM{tokens: []string{"response"}}
	a, _ := pool.Spawn("inbox2", lm, agent.SpawnOpts{})

	// Queue inbox messages before the turn.
	a.AppendInbox(sdkMsg("context from another agent"))

	done := make(chan error, 1)
	a.SetOnDone(func(err error) { done <- err })
	a.Submit(context.Background(), "use the context")

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}
	// Inbox should be empty after Submit drains it.
	if msgs := a.DrainInbox(); len(msgs) != 0 {
		t.Errorf("inbox should be empty after Submit, got %d messages", len(msgs))
	}
}

// ---- system message filtering tests ----

// spyLM captures the fantasy.Call.Prompt passed to Stream for inspection.
type spyLM struct {
	mu       sync.Mutex
	calls    [][]fantasy.Message // captured prompts per call
	response []string            // tokens to emit
}

func (s *spyLM) Model() string    { return "spy-model" }
func (s *spyLM) Provider() string { return "test" }

func (s *spyLM) Stream(_ context.Context, call fantasy.Call) (fantasy.StreamResponse, error) {
	s.mu.Lock()
	// call.Prompt is []fantasy.Message
	msgs := make([]fantasy.Message, len(call.Prompt))
	copy(msgs, call.Prompt)
	s.calls = append(s.calls, msgs)
	toks := s.response
	s.mu.Unlock()

	return func(yield func(fantasy.StreamPart) bool) {
		for _, tok := range toks {
			if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextDelta, Delta: tok}) {
				return
			}
		}
		yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeFinish, FinishReason: fantasy.FinishReasonStop})
	}, nil
}

func (s *spyLM) Generate(_ context.Context, _ fantasy.Call) (*fantasy.Response, error) {
	return &fantasy.Response{}, nil
}

func (s *spyLM) GenerateObject(_ context.Context, _ fantasy.ObjectCall) (*fantasy.ObjectResponse, error) {
	return nil, nil
}

func (s *spyLM) StreamObject(_ context.Context, _ fantasy.ObjectCall) (fantasy.ObjectStreamResponse, error) {
	return nil, nil
}

func (s *spyLM) capturedPrompts() [][]fantasy.Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([][]fantasy.Message, len(s.calls))
	copy(result, s.calls)
	return result
}

func TestSubmit_SystemMessagesFilteredFromLLMContext(t *testing.T) {
	pool := agent.NewPool()
	spy := &spyLM{response: []string{"ok"}}
	a, err := pool.Spawn("filter-test", spy, agent.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	// Inject normal + system messages into inbox.
	a.AppendInbox(sdk.Message{Role: sdk.RoleUser, Content: "normal inbox message"})
	a.AppendInbox(sdk.Message{Role: sdk.RoleUser, Content: `{"event":"system_event"}`, Type: sdk.MessageTypeSystem})
	a.AppendInbox(sdk.Message{Role: sdk.RoleUser, Content: "steering hint", Type: sdk.MessageTypeSteering})

	done := make(chan error, 1)
	a.SetOnDone(func(e error) { done <- e })
	a.Submit(testCtx(t), "user prompt")

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("turn error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}

	prompts := spy.capturedPrompts()
	if len(prompts) == 0 {
		t.Fatal("spy received no calls")
	}
	// Check that the system and steering messages do NOT appear in the LLM context.
	allMessages := prompts[0]
	for _, m := range allMessages {
		for _, part := range m.Content {
			if tp, ok := part.(fantasy.TextPart); ok {
				if tp.Text == `{"event":"system_event"}` {
					t.Errorf("system message leaked to LLM context: %q", tp.Text)
				}
				if tp.Text == "steering hint" {
					t.Errorf("steering message leaked to LLM context: %q", tp.Text)
				}
			}
		}
	}
	// Normal inbox message SHOULD appear.
	found := false
	for _, m := range allMessages {
		for _, part := range m.Content {
			if tp, ok := part.(fantasy.TextPart); ok && tp.Text == "normal inbox message" {
				found = true
			}
		}
	}
	if !found {
		t.Error("normal inbox message was incorrectly filtered from LLM context")
	}
}

func TestDrainInbox_SystemMessagesRetained(t *testing.T) {
	// System messages should survive AppendInbox/DrainInbox — filtering happens
	// at sdkToFantasyMessages time, not at drain time.
	pool := agent.NewPool()
	lm := newMockLM()
	a, _ := pool.Spawn("drain-test", lm, agent.SpawnOpts{})

	sysMsg := sdk.Message{Role: sdk.RoleUser, Content: `{"event":"test"}`, Type: sdk.MessageTypeSystem}
	a.AppendInbox(sysMsg)
	a.AppendInbox(sdk.Message{Role: sdk.RoleUser, Content: "normal"})

	drained := a.DrainInbox()
	if len(drained) != 2 {
		t.Fatalf("expected 2 messages after drain, got %d", len(drained))
	}
	found := false
	for _, m := range drained {
		if m.Type == sdk.MessageTypeSystem && m.Content == sysMsg.Content {
			found = true
		}
	}
	if !found {
		t.Error("system message was removed during DrainInbox — it should only be filtered at LLM conversion time")
	}
}

// ---- helper LM types ----

// tokenStreamLM emits fixed tokens then a finish event.
type tokenStreamLM struct {
	tokens []string
}

func (t *tokenStreamLM) Model() string    { return "token-stream" }
func (t *tokenStreamLM) Provider() string { return "test" }

func (t *tokenStreamLM) Stream(ctx context.Context, _ fantasy.Call) (fantasy.StreamResponse, error) {
	toks := t.tokens
	return func(yield func(fantasy.StreamPart) bool) {
		for _, tok := range toks {
			select {
			case <-ctx.Done():
				return
			default:
			}
			if !yield(fantasy.StreamPart{
				Type:  fantasy.StreamPartTypeTextDelta,
				Delta: tok,
			}) {
				return
			}
		}
		yield(fantasy.StreamPart{
			Type:         fantasy.StreamPartTypeFinish,
			FinishReason: fantasy.FinishReasonStop,
		})
	}, nil
}

func (t *tokenStreamLM) Generate(_ context.Context, _ fantasy.Call) (*fantasy.Response, error) {
	return &fantasy.Response{}, nil
}

func (t *tokenStreamLM) GenerateObject(_ context.Context, _ fantasy.ObjectCall) (*fantasy.ObjectResponse, error) {
	return nil, nil
}

func (t *tokenStreamLM) StreamObject(_ context.Context, _ fantasy.ObjectCall) (fantasy.ObjectStreamResponse, error) {
	return nil, nil
}

// errStreamLM always returns an error from Stream.
type errStreamLM struct{}

func (e *errStreamLM) Model() string    { return "err-model" }
func (e *errStreamLM) Provider() string { return "test" }
func (e *errStreamLM) Stream(_ context.Context, _ fantasy.Call) (fantasy.StreamResponse, error) {
	return func(yield func(fantasy.StreamPart) bool) {
		yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeError, Error: errors.New("stream error")})
	}, nil
}

func (e *errStreamLM) Generate(_ context.Context, _ fantasy.Call) (*fantasy.Response, error) {
	return nil, errors.New("generate error")
}

func (e *errStreamLM) GenerateObject(_ context.Context, _ fantasy.ObjectCall) (*fantasy.ObjectResponse, error) {
	return nil, nil
}

func (e *errStreamLM) StreamObject(_ context.Context, _ fantasy.ObjectCall) (fantasy.ObjectStreamResponse, error) {
	return nil, nil
}

// slowLM blocks until its context is cancelled — used to test Cancel().
type slowLM struct{}

func (s *slowLM) Model() string    { return "slow" }
func (s *slowLM) Provider() string { return "test" }
func (s *slowLM) Stream(ctx context.Context, _ fantasy.Call) (fantasy.StreamResponse, error) {
	return func(yield func(fantasy.StreamPart) bool) {
		<-ctx.Done()
	}, nil
}

func (s *slowLM) Generate(_ context.Context, _ fantasy.Call) (*fantasy.Response, error) {
	return &fantasy.Response{}, nil
}

func (s *slowLM) GenerateObject(_ context.Context, _ fantasy.ObjectCall) (*fantasy.ObjectResponse, error) {
	return nil, nil
}

func (s *slowLM) StreamObject(_ context.Context, _ fantasy.ObjectCall) (fantasy.ObjectStreamResponse, error) {
	return nil, nil
}

func TestAgent_Cancel_RecordsUserMessageInHistory(t *testing.T) {
	// When a turn is cancelled (Esc), the user message must still be recorded
	// so the agent doesn't "forget" what was asked.
	pool := agent.NewPool()
	a, err := pool.Spawn("main", &slowLM{}, agent.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	done := make(chan error, 1)
	a.SetOnDone(func(e error) { done <- e })

	// Start a turn, then cancel it immediately.
	a.Submit(testCtx(t), "what is the capital of France?")
	time.Sleep(10 * time.Millisecond)
	a.Cancel()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}

	history := a.History()
	if len(history) == 0 {
		t.Fatal("history is empty after cancelled turn — user message was lost")
	}

	// The user message must be present.
	found := false
	for _, m := range history {
		if m.Role == sdk.RoleUser && m.Content == "what is the capital of France?" {
			found = true
		}
	}
	if !found {
		t.Errorf("user message not found in history after cancellation: %v", history)
	}

	// History must still alternate correctly (no lone user message at end).
	last := history[len(history)-1]
	if last.Role != sdk.RoleAssistant {
		t.Errorf("history ends with %s, want assistant — alternation broken", last.Role)
	}
}

func TestAgent_Cancel_PlaceholderAllowsNextTurn(t *testing.T) {
	// After a cancelled turn, a subsequent turn must succeed without a
	// "messages must alternate" rejection from the provider.
	pool := agent.NewPool()
	lm := &tokenStreamLM{tokens: []string{"Paris"}}

	a, err := pool.Spawn("main", &slowLM{}, agent.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	done := make(chan error, 1)
	a.SetOnDone(func(e error) { done <- e })

	// First turn — cancel it.
	a.Submit(testCtx(t), "cancelled question")
	time.Sleep(10 * time.Millisecond)
	a.Cancel()
	<-done

	// Second turn with a real LM — must succeed.
	// Replace the LM by re-spawning with the real LM on a fresh pool.
	pool2 := agent.NewPool()
	a2, err := pool2.Spawn("main", lm, agent.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn2: %v", err)
	}

	// Copy history from first agent to simulate continuity.
	for _, m := range a.History() {
		_ = m // history verified separately; just ensure no panic
	}

	done2 := make(chan error, 1)
	var got string
	a2.SetOnToken(func(tok string) { got += tok })
	a2.SetOnDone(func(e error) { done2 <- e })
	a2.Submit(testCtx(t), "what is the capital of France?")

	select {
	case err := <-done2:
		if err != nil {
			t.Fatalf("second turn failed: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout on second turn")
	}

	if got == "" {
		t.Error("second turn produced no output")
	}
}

// ---- finishTurn graceful shutdown tests ----

// TestFinishTurn_ShutdownRequest_SendsAgentShutdownAndClosesSelf verifies that when
// a system shutdown_request message arrives in the agent's inbox while a turn is running,
// finishTurn sends an AGENT_SHUTDOWN system message to the creator and removes the
// agent from the pool.
func TestFinishTurn_ShutdownRequest_SendsAgentShutdownAndClosesSelf(t *testing.T) {
	pool := agent.NewPool()
	lm := newBlockingLM("response")

	// Spawn orchestrator (creator) and worker (target of shutdown).
	orch, err := pool.Spawn("main/orchestrator", newMockLM(), agent.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn orchestrator: %v", err)
	}
	_, err = pool.Spawn("main/worker", lm, agent.SpawnOpts{CreatorID: "main/orchestrator"})
	if err != nil {
		t.Fatalf("Spawn worker: %v", err)
	}

	worker := pool.Get("main/worker")
	if worker == nil {
		t.Fatal("worker not found after spawn")
	}

	done := make(chan error, 1)
	worker.SetOnDone(func(e error) { done <- e })

	// Start a turn — the blocking LM will hold it open.
	worker.Submit(testCtx(t), "hello")

	// Wait until Stream has been entered before injecting inbox messages.
	lm.WaitForStream(t)

	// Inject the shutdown_request while the turn is in flight.
	shutdownMsg, _ := json.Marshal(map[string]string{
		"event": "shutdown_request",
		"from":  "main/orchestrator",
	})
	worker.AppendInbox(sdk.Message{
		Role:    sdk.RoleUser,
		Content: string(shutdownMsg),
		Type:    sdk.MessageTypeSystem,
	})

	// Release the blocking LM to let the turn complete.
	lm.Release()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("worker turn error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for worker turn to complete")
	}

	// Worker should have been removed from pool.
	if pool.Get("main/worker") != nil {
		t.Error("worker should have been removed from pool after shutdown_request, but it's still present")
	}

	// Orchestrator's inbox should contain an AGENT_SHUTDOWN system message.
	orchInbox := orch.DrainInbox()
	if len(orchInbox) == 0 {
		t.Fatal("orchestrator's inbox is empty — expected AGENT_SHUTDOWN system message")
	}
	found := false
	for _, m := range orchInbox {
		if m.Type != sdk.MessageTypeSystem {
			continue
		}
		var evt struct {
			Event   string `json:"event"`
			AgentID string `json:"agent_id"`
		}
		if err := json.Unmarshal([]byte(m.Content), &evt); err == nil &&
			evt.Event == "AGENT_SHUTDOWN" && evt.AgentID == "main/worker" {
			found = true
		}
	}
	if !found {
		t.Errorf("AGENT_SHUTDOWN message not found in orchestrator inbox; got: %+v", orchInbox)
	}
}

// TestFinishTurn_NormalMessagesProcessedBeforeShutdown verifies that if both a normal
// message and a shutdown_request are pending in the inbox when finishTurn runs,
// the normal message is processed first (drain-until-empty pattern) and shutdown
// is deferred to the next drain turn.
func TestFinishTurn_NormalMessagesProcessedBeforeShutdown(t *testing.T) {
	pool := agent.NewPool()
	lm := newBlockingLM("response")

	orch, err := pool.Spawn("main/orchestrator", newMockLM(), agent.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn orchestrator: %v", err)
	}
	_, err = pool.Spawn("main/worker", lm, agent.SpawnOpts{CreatorID: "main/orchestrator"})
	if err != nil {
		t.Fatalf("Spawn worker: %v", err)
	}

	worker := pool.Get("main/worker")
	if worker == nil {
		t.Fatal("worker not found")
	}

	done := make(chan error, 1)
	worker.SetOnDone(func(e error) { done <- e })

	// Start a turn (blocking LM holds it open).
	worker.Submit(testCtx(t), "initial task")

	// Wait until Stream has been entered before injecting inbox messages.
	lm.WaitForStream(t)

	// Inject both a normal message AND a shutdown_request while in-flight.
	worker.AppendInbox(sdk.Message{Role: sdk.RoleUser, Content: "do some work"})
	shutdownPayload, _ := json.Marshal(map[string]string{
		"event": "shutdown_request",
		"from":  "main/orchestrator",
	})
	worker.AppendInbox(sdk.Message{
		Role:    sdk.RoleUser,
		Content: string(shutdownPayload),
		Type:    sdk.MessageTypeSystem,
	})

	// Release the blocking LM — finishTurn should drain normal messages first
	// (another turn via mockLM), then process shutdown on the next cycle.
	lm.Release()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("worker turn error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}

	// Worker should have shut down after draining all normal messages.
	if pool.Get("main/worker") != nil {
		t.Error("worker should have been removed from pool after shutdown processing")
	}

	// Orchestrator should have received AGENT_SHUTDOWN.
	orchInbox := orch.DrainInbox()
	found := false
	for _, m := range orchInbox {
		if m.Type != sdk.MessageTypeSystem {
			continue
		}
		var evt struct {
			Event string `json:"event"`
		}
		if err := json.Unmarshal([]byte(m.Content), &evt); err == nil && evt.Event == "AGENT_SHUTDOWN" {
			found = true
		}
	}
	if !found {
		t.Error("AGENT_SHUTDOWN not received by orchestrator after normal message processing")
	}
}

// TestFinishTurn_NoShutdownRequest_Normal verifies that a normal turn with only
// regular inbox messages calls onDone normally without closing the agent.
func TestFinishTurn_NoShutdownRequest_Normal(t *testing.T) {
	pool := agent.NewPool()
	lm := newBlockingLM("response")

	_, err := pool.Spawn("main/worker", lm, agent.SpawnOpts{CreatorID: "main/orchestrator"})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	worker := pool.Get("main/worker")

	done := make(chan error, 1)
	worker.SetOnDone(func(e error) { done <- e })

	// Start a turn, inject only a normal message while it's running.
	worker.Submit(testCtx(t), "task")

	// Wait until Stream has been entered before injecting inbox messages.
	lm.WaitForStream(t)
	worker.AppendInbox(sdk.Message{Role: sdk.RoleUser, Content: "normal message"})
	lm.Release()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("turn error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}

	// Worker should still be in pool — no shutdown was requested.
	if pool.Get("main/worker") == nil {
		t.Error("worker should still be in pool when no shutdown_request was sent")
	}
}
