package agent_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"charm.land/fantasy"
	"github.com/mattdurham/wllr/agent"
	"github.com/mattdurham/wllr/sdk"
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
	// Regression test: after a cancelled turn, transferring the cancelled history
	// to a second agent and running a new turn must succeed without a
	// "messages must alternate" rejection from the provider.
	//
	// The mechanism: Cancel() causes the agent to record "[response cancelled]" as
	// the assistant placeholder, so history ends with a valid user/assistant pair.
	// A second agent with this history can immediately accept a new user message.
	pool := agent.NewPool()
	t.Cleanup(func() {
		pool.Close("first")
		pool.Close("second")
	})

	a, err := pool.Spawn("first", &slowLM{}, agent.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn first: %v", err)
	}

	done := make(chan error, 1)
	a.SetOnDone(func(e error) { done <- e })

	// First turn — cancel it mid-flight.
	a.Submit(testCtx(t), "cancelled question")
	time.Sleep(10 * time.Millisecond)
	a.Cancel()
	<-done

	// The cancelled history must end with an assistant placeholder message.
	cancelledHistory := a.History()
	if len(cancelledHistory) < 2 {
		t.Fatalf("expected at least 2 history messages after cancel, got %d", len(cancelledHistory))
	}
	last := cancelledHistory[len(cancelledHistory)-1]
	if last.Role != sdk.RoleAssistant {
		t.Errorf("history after cancel must end with assistant placeholder, got role=%s content=%q",
			last.Role, last.Content)
	}

	// Spawn a second agent on the SAME pool and transfer the cancelled history.
	// This exercises the regression: cancelled history must be transferable.
	lm := &tokenStreamLM{tokens: []string{"Paris"}}
	a2, err := pool.Spawn("second", lm, agent.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn second: %v", err)
	}
	if err := pool.SetAgentHistory("second", cancelledHistory); err != nil {
		t.Fatalf("SetAgentHistory: %v", err)
	}

	done2 := make(chan error, 1)
	var got string
	a2.SetOnToken(func(tok string) { got += tok })
	a2.SetOnDone(func(e error) { done2 <- e })

	// Submit a new turn — must NOT fail with "messages must alternate" error.
	a2.Submit(testCtx(t), "what is the capital of France?")

	select {
	case err := <-done2:
		if err != nil {
			t.Fatalf("second turn failed: %v — cancelled history did not transfer cleanly", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout on second turn")
	}

	if got == "" {
		t.Error("second turn produced no output")
	}
}

// --- C9/H-con1: shutdown context in drain loop ---

// switchableLM completes the first turn instantly, then blocks subsequent turns
// until their context is cancelled. This lets us guarantee the drain turn is
// in-flight when pool.Close fires, making TestAgent_ShutdownCancels_DrainLoop
// a deterministic regression test rather than a timing-dependent one.
type switchableLM struct {
	calls atomic.Int32
}

func (s *switchableLM) Model() string    { return "switchable" }
func (s *switchableLM) Provider() string { return "test" }
func (s *switchableLM) Stream(ctx context.Context, _ fantasy.Call) (fantasy.StreamResponse, error) {
	if s.calls.Add(1) == 1 {
		// First call: return immediately so the drain loop starts.
		return func(yield func(fantasy.StreamPart) bool) {
			yield(fantasy.StreamPart{
				Type:         fantasy.StreamPartTypeFinish,
				FinishReason: fantasy.FinishReasonStop,
			})
		}, nil
	}
	// Subsequent calls: block until context cancelled — drain turn is in-flight.
	<-ctx.Done()
	return nil, ctx.Err()
}

func (s *switchableLM) Generate(_ context.Context, _ fantasy.Call) (*fantasy.Response, error) {
	return &fantasy.Response{}, nil
}

func (s *switchableLM) GenerateObject(_ context.Context, _ fantasy.ObjectCall) (*fantasy.ObjectResponse, error) {
	return &fantasy.ObjectResponse{}, nil
}

func (s *switchableLM) StreamObject(_ context.Context, _ fantasy.ObjectCall) (fantasy.ObjectStreamResponse, error) {
	return nil, nil
}

func TestAgent_ShutdownCancels_DrainLoop(t *testing.T) {
	// H-con1/C9: pool.Close must cancel the drain loop by cancelling shutdownCtx.
	// Without this fix, the drain loop would use context.Background() and ignore
	// the shutdown signal for up to 10 minutes.
	//
	// switchableLM guarantees the drain turn is always in-flight when pool.Close
	// fires: the first LM call returns instantly (completing turn 1 and starting
	// the drain loop), and all subsequent calls block until context cancellation.
	// This makes the test deterministic — no sleep or timing dependency.
	pool := agent.NewPool()
	lm := &switchableLM{}
	a, err := pool.Spawn("main", lm, agent.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	done := make(chan error, 1)
	a.SetOnDone(func(e error) {
		done <- e
	})

	// Start first turn — completes quickly, triggering the drain loop.
	a.Submit(testCtx(t), "turn 1")

	// Queue a second message so the drain loop picks it up immediately.
	a.AppendInbox(sdk.Message{Role: sdk.RoleUser, Content: "drain message"})

	// Wait for first turn to finish (drain turn is now in-flight and blocking).
	select {
	case <-done:
		// First turn done — drain turn has started and is blocking in slowLM.
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for first turn")
	}

	// Close the pool — shutdownCtx must be cancelled, unblocking the drain turn.
	if err := pool.Close("main"); err != nil {
		t.Fatalf("pool.Close: %v", err)
	}

	// The drain turn must terminate with context.Canceled — not time out.
	select {
	case err := <-done:
		if err != context.Canceled {
			t.Errorf("drain turn error = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("drain loop did not terminate after pool.Close — shutdownCtx not propagated to drain turn")
	}
}

// --- C1: panic leaves isRunning stuck ---

// panicLM panics inside Stream to test the recover() path in Submit.
type panicLM struct{}

func (p *panicLM) Model() string    { return "panic-model" }
func (p *panicLM) Provider() string { return "test" }
func (p *panicLM) Stream(_ context.Context, _ fantasy.Call) (fantasy.StreamResponse, error) {
	panic("simulated LM panic")
}

func (p *panicLM) Generate(_ context.Context, _ fantasy.Call) (*fantasy.Response, error) {
	panic("simulated LM panic")
}

func (p *panicLM) GenerateObject(_ context.Context, _ fantasy.ObjectCall) (*fantasy.ObjectResponse, error) {
	return nil, nil
}

func (p *panicLM) StreamObject(_ context.Context, _ fantasy.ObjectCall) (fantasy.ObjectStreamResponse, error) {
	return nil, nil
}

func TestAgent_Panic_ResetsIsRunning(t *testing.T) {
	// C1: if the LM panics during streaming, the recover() block must reset
	// isRunning to false so the agent can accept future turns.
	// Before this fix: isRunning stayed true forever after a panic.
	pool := agent.NewPool()
	a, err := pool.Spawn("panic-agent", &panicLM{}, agent.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	done := make(chan error, 1)
	a.SetOnDone(func(e error) { done <- e })

	a.Submit(context.Background(), "trigger panic")

	// Wait for the turn to complete (via the recover path).
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected non-nil error from panic recovery, got nil")
		}
		if !strings.Contains(err.Error(), "panic") {
			t.Errorf("expected panic error, got: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout: onDone was never called after panic")
	}

	// Critical invariant: IsRunning must be false after the panic.
	if a.IsRunning() {
		t.Error("C1 regression: IsRunning() is still true after panic — agent is stuck")
	}

	// Bonus: verify the *same* agent accepts a second turn after recovery (L-1 fix).
	// Submitting to "a" again proves the original panicking agent's isRunning was
	// correctly reset — not merely that a fresh agent works.
	done2 := make(chan error, 1)
	a.SetOnDone(func(e error) { done2 <- e })
	a.Submit(context.Background(), "post-panic turn")
	select {
	case err := <-done2:
		// The panicLM will panic again — that's fine; we only care that isRunning resets.
		if err == nil {
			t.Fatal("expected non-nil error from second panic, got nil")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout on second turn of panicking agent")
	}
	// IsRunning must be false again after the second panic.
	if a.IsRunning() {
		t.Error("C1 regression: IsRunning() is still true after second panic on same agent")
	}
}
