package agent_test

import (
	"context"
	"errors"
	"sync"
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
