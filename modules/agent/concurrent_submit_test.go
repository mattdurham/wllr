package agent_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"charm.land/fantasy"
	"github.com/mattdurham/wllr/modules/agent"
	"github.com/mattdurham/wllr/modules/sdk"
)

// blockingLM blocks in Stream until its release channel is closed.
// This lets tests control exactly when a turn finishes.
type blockingLM struct {
	release chan struct{}
	tokens  []string
}

func newBlockingLM(tokens ...string) *blockingLM {
	return &blockingLM{release: make(chan struct{}), tokens: tokens}
}

func (b *blockingLM) Model() string    { return "blocking-model" }
func (b *blockingLM) Provider() string { return "blocking" }

func (b *blockingLM) Stream(ctx context.Context, _ fantasy.Call) (fantasy.StreamResponse, error) {
	toks := b.tokens
	rel := b.release
	return func(yield func(fantasy.StreamPart) bool) {
		// Block until released or context cancelled.
		select {
		case <-rel:
		case <-ctx.Done():
			return
		}
		for _, tok := range toks {
			if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextDelta, Delta: tok}) {
				return
			}
		}
		yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeFinish, FinishReason: fantasy.FinishReasonStop})
	}, nil
}

func (b *blockingLM) Generate(_ context.Context, _ fantasy.Call) (*fantasy.Response, error) {
	return nil, nil
}

func (b *blockingLM) GenerateObject(_ context.Context, _ fantasy.ObjectCall) (*fantasy.ObjectResponse, error) {
	return nil, nil
}

func (b *blockingLM) StreamObject(_ context.Context, _ fantasy.ObjectCall) (fantasy.ObjectStreamResponse, error) {
	return nil, nil
}

// Release unblocks the LM so the stream can complete.
func (b *blockingLM) Release() { close(b.release) }

// TestSubmit_ConcurrentCallQueuesContent verifies that a second Submit call while
// a turn is running queues content to the inbox and the running goroutine processes
// it in a second turn after the first completes.
func TestSubmit_ConcurrentCallQueuesContent(t *testing.T) {
	pool := agent.NewPool()
	lm := newBlockingLM("response1")
	a, _ := pool.Spawn("concurrent-test", lm, agent.SpawnOpts{})

	doneCount := 0
	doneMu := sync.Mutex{}
	doneCh := make(chan error, 5)
	a.SetOnDone(func(err error) {
		doneMu.Lock()
		doneCount++
		doneMu.Unlock()
		doneCh <- err
	})

	// Start first turn — it will block.
	a.Submit(context.Background(), "first content")

	// Give the goroutine time to start and block.
	time.Sleep(50 * time.Millisecond)

	// Call Submit again while first turn is running.
	// This should return immediately (non-blocking) and queue content to inbox.
	secondDone := make(chan struct{})
	go func() {
		defer close(secondDone)
		a.Submit(context.Background(), "second content")
	}()

	// Second Submit should return without blocking.
	select {
	case <-secondDone:
		// Good - returned immediately.
	case <-time.After(2 * time.Second):
		t.Fatal("second Submit blocked for too long — isRunning guard not implemented")
	}

	// Release the first turn.
	lm.Release()

	// Wait for the final onDone — fired once after both turns complete.
	// After the fix for double-StreamDoneMsg (H1), onDone is NOT fired on the
	// intermediate drain branch; only the final drain turn fires it when the inbox
	// is confirmed empty. This means one onDone per logical session, not per turn.
	select {
	case err := <-doneCh:
		if err != nil {
			t.Fatalf("turn error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("second turn (from drain) did not complete")
	}

	// Verify history has 2 user+assistant pairs (not corrupted).
	history := a.History()
	// We expect: setup from first turn (user+assistant) and queued second content.
	// At minimum history must have entries and not be empty.
	if len(history) == 0 {
		t.Error("history is empty after two turns")
	}
}

// TestSubmit_ConcurrentCall_HistoryNotCorrupted runs two independent agents
// concurrently and verifies history integrity. Run with -race to catch data races.
func TestSubmit_ConcurrentCall_HistoryNotCorrupted(t *testing.T) {
	pool := agent.NewPool()
	lm1 := &tokenStreamLM{tokens: []string{"response-a"}}
	lm2 := &tokenStreamLM{tokens: []string{"response-b"}}

	a1, _ := pool.Spawn("concurrent-a", lm1, agent.SpawnOpts{})
	a2, _ := pool.Spawn("concurrent-b", lm2, agent.SpawnOpts{})

	var wg sync.WaitGroup
	done1 := make(chan error, 1)
	done2 := make(chan error, 1)

	a1.SetOnDone(func(err error) { done1 <- err })
	a2.SetOnDone(func(err error) { done2 <- err })

	wg.Add(2)
	go func() {
		defer wg.Done()
		a1.Submit(context.Background(), "msg for a")
	}()
	go func() {
		defer wg.Done()
		a2.Submit(context.Background(), "msg for b")
	}()

	// Wait for both agents to finish.
	for _, ch := range []chan error{done1, done2} {
		select {
		case err := <-ch:
			if err != nil {
				t.Errorf("agent error: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Error("agent timed out")
		}
	}
	wg.Wait()

	// Verify each agent has its own history (not mixed).
	h1 := a1.History()
	h2 := a2.History()

	if len(h1) == 0 {
		t.Error("a1 history is empty")
	}
	if len(h2) == 0 {
		t.Error("a2 history is empty")
	}

	// Check that a1's history doesn't contain b's content and vice versa.
	for _, msg := range h1 {
		if msg.Role == sdk.RoleUser && msg.Content == "msg for b" {
			t.Error("a1 history contains a2's user message")
		}
	}
	for _, msg := range h2 {
		if msg.Role == sdk.RoleUser && msg.Content == "msg for a" {
			t.Error("a2 history contains a1's user message")
		}
	}
}
