package agent_test

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"context"
	"fmt"
	"testing"
	"time"

	"charm.land/fantasy"
	"github.com/mattdurham/wllr/modules/agent"
	"github.com/mattdurham/wllr/modules/sdk"
)

// usageLM emits fixed tokens and then a finish part that includes usage statistics.
// This allows tests to assert that streamTurn captures and returns real usage.
type usageLM struct {
	tokens       []string
	inputTokens  int64
	outputTokens int64
}

func (u *usageLM) Model() string    { return "usage-model" }
func (u *usageLM) Provider() string { return "test" }

func (u *usageLM) Stream(_ context.Context, _ fantasy.Call) (fantasy.StreamResponse, error) {
	toks := u.tokens
	usage := fantasy.Usage{
		InputTokens:  u.inputTokens,
		OutputTokens: u.outputTokens,
		TotalTokens:  u.inputTokens + u.outputTokens,
	}
	return func(yield func(fantasy.StreamPart) bool) {
		for _, tok := range toks {
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
			Usage:        usage,
		})
	}, nil
}

func (u *usageLM) Generate(_ context.Context, _ fantasy.Call) (*fantasy.Response, error) {
	return &fantasy.Response{}, nil
}

func (u *usageLM) GenerateObject(_ context.Context, _ fantasy.ObjectCall) (*fantasy.ObjectResponse, error) {
	return nil, nil
}

func (u *usageLM) StreamObject(_ context.Context, _ fantasy.ObjectCall) (fantasy.ObjectStreamResponse, error) {
	return nil, nil
}

// TestStreamTurnReturnsUsage verifies that after a successful turn,
// the usage returned from streamTurn has non-zero InputTokens and OutputTokens.
func TestStreamTurnReturnsUsage(t *testing.T) {
	pool := agent.NewPool()
	lm := &usageLM{
		tokens:       []string{"hello", " world"},
		inputTokens:  1500,
		outputTokens: 42,
	}
	a, err := pool.Spawn("usage-test", lm, agent.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	done := make(chan error, 1)
	a.SetOnDone(func(e error) { done <- e })
	a.Submit(context.Background(), "hi")

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("submit error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for turn to complete")
	}

	u := a.LastUsage()
	if u.InputTokens <= 0 {
		t.Errorf("LastUsage.InputTokens = %d, want > 0", u.InputTokens)
	}
	if u.OutputTokens <= 0 {
		t.Errorf("LastUsage.OutputTokens = %d, want > 0", u.OutputTokens)
	}
}

// TestStreamTurnUsageZeroOnError verifies that when streamTurn returns an error,
// the stored usage is zero-valued (not contaminated from a previous turn).
func TestStreamTurnUsageZeroOnError(t *testing.T) {
	pool := agent.NewPool()
	errLM := &errStreamLM{}
	a, err := pool.Spawn("err-usage", errLM, agent.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	done := make(chan error, 1)
	a.SetOnDone(func(e error) { done <- e })
	a.Submit(context.Background(), "will fail")

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected error from errStreamLM, got nil")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}

	u := a.LastUsage()
	if u.InputTokens != 0 || u.OutputTokens != 0 {
		t.Errorf("LastUsage after error = {InputTokens:%d, OutputTokens:%d}, want zero", u.InputTokens, u.OutputTokens)
	}
}

// TestAgentLastUsage verifies that after a completed Submit, LastUsage
// returns the usage reported by the provider.
func TestAgentLastUsage(t *testing.T) {
	pool := agent.NewPool()
	lm := &usageLM{
		tokens:       []string{"response"},
		inputTokens:  800,
		outputTokens: 20,
	}
	a, err := pool.Spawn("last-usage", lm, agent.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	done := make(chan error, 1)
	a.SetOnDone(func(e error) { done <- e })
	a.Submit(context.Background(), "test")

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}

	u := a.LastUsage()
	if u.InputTokens != 800 {
		t.Errorf("LastUsage.InputTokens = %d, want 800", u.InputTokens)
	}
	if u.OutputTokens != 20 {
		t.Errorf("LastUsage.OutputTokens = %d, want 20", u.OutputTokens)
	}
}

// TestPoolMainAgentContextUsage verifies that after a turn completes, the pool
// exposes context usage with non-zero InputTokens and a Percent > 0 (when ContextWindow is set).
func TestPoolMainAgentContextUsage(t *testing.T) {
	pool := agent.NewPool()
	pool.SetContextWindow(200_000)

	lm := &usageLM{
		tokens:       []string{"output"},
		inputTokens:  50_000,
		outputTokens: 500,
	}
	_, err := pool.Spawn(agent.MainAgentID, lm, agent.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	mainAgent := pool.Get(agent.MainAgentID)
	done := make(chan error, 1)
	mainAgent.SetOnDone(func(e error) { done <- e })
	mainAgent.Submit(context.Background(), "query")

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}

	cu := pool.MainAgentContextUsage()
	if cu.InputTokens <= 0 {
		t.Errorf("ContextUsage.InputTokens = %d, want > 0", cu.InputTokens)
	}
	if cu.ContextWindow <= 0 {
		t.Errorf("ContextUsage.ContextWindow = %d, want > 0", cu.ContextWindow)
	}
	if cu.Percent <= 0 {
		t.Errorf("ContextUsage.Percent = %f, want > 0", cu.Percent)
	}
}

// successThenErrLM succeeds on the first Stream call (with fixed usage) and
// returns an error on every subsequent call. Used to test that a failed turn
// zeroes out lastUsage even after a prior successful turn.
type successThenErrLM struct {
	calls        int
	inputTokens  int64
	outputTokens int64
}

func (s *successThenErrLM) Model() string    { return "success-then-err-model" }
func (s *successThenErrLM) Provider() string { return "test" }

func (s *successThenErrLM) Stream(_ context.Context, _ fantasy.Call) (fantasy.StreamResponse, error) {
	s.calls++
	if s.calls == 1 {
		usage := fantasy.Usage{
			InputTokens:  s.inputTokens,
			OutputTokens: s.outputTokens,
			TotalTokens:  s.inputTokens + s.outputTokens,
		}
		return func(yield func(fantasy.StreamPart) bool) {
			yield(fantasy.StreamPart{
				Type:  fantasy.StreamPartTypeTextDelta,
				Delta: "ok",
			})
			yield(fantasy.StreamPart{
				Type:         fantasy.StreamPartTypeFinish,
				FinishReason: fantasy.FinishReasonStop,
				Usage:        usage,
			})
		}, nil
	}
	return nil, fmt.Errorf("stream error on turn %d", s.calls)
}

func (s *successThenErrLM) Generate(_ context.Context, _ fantasy.Call) (*fantasy.Response, error) {
	return &fantasy.Response{}, nil
}
func (s *successThenErrLM) GenerateObject(_ context.Context, _ fantasy.ObjectCall) (*fantasy.ObjectResponse, error) {
	return nil, nil
}
func (s *successThenErrLM) StreamObject(_ context.Context, _ fantasy.ObjectCall) (fantasy.ObjectStreamResponse, error) {
	return nil, nil
}

// TestAgentLastUsageClearedOnError verifies that a failed turn zeroes out lastUsage
// even when a prior successful turn had set non-zero usage. This guards against the
// contamination scenario: stale usage from a successful turn influencing the compaction
// decision on the subsequent failed turn.
func TestAgentLastUsageClearedOnError(t *testing.T) {
	pool := agent.NewPool()
	lm := &successThenErrLM{inputTokens: 1200, outputTokens: 50}
	a, err := pool.Spawn("success-then-err", lm, agent.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	// First turn: should succeed and set non-zero lastUsage.
	done := make(chan error, 1)
	a.SetOnDone(func(e error) { done <- e })
	a.Submit(context.Background(), "first turn")

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("first turn unexpectedly failed: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for first turn")
	}

	u := a.LastUsage()
	if u.InputTokens == 0 {
		t.Fatal("LastUsage.InputTokens should be non-zero after a successful turn")
	}

	// Second turn: should fail and zero out lastUsage.
	a.SetOnDone(func(e error) { done <- e })
	a.Submit(context.Background(), "second turn")

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected second turn to fail, got nil error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for second turn")
	}

	u = a.LastUsage()
	if u.InputTokens != 0 || u.OutputTokens != 0 {
		t.Errorf("LastUsage after error = {InputTokens:%d, OutputTokens:%d}, want zero", u.InputTokens, u.OutputTokens)
	}
}

// TestEventContextUsageDispatched verifies that the pool's contextUsageDispatcher is called
// after a successful turn with non-zero usage.
func TestEventContextUsageDispatched(t *testing.T) {
	pool := agent.NewPool()
	pool.SetContextWindow(200_000)

	lm := &usageLM{
		tokens:       []string{"response"},
		inputTokens:  30_000,
		outputTokens: 100,
	}
	a, err := pool.Spawn(agent.MainAgentID, lm, agent.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	type dispatchEvent struct {
		cu        sdk.ContextUsage
		compacted bool
	}
	dispatched := make(chan dispatchEvent, 1)
	pool.SetContextUsageDispatcher(func(cu sdk.ContextUsage, compacted bool) {
		select {
		case dispatched <- dispatchEvent{cu, compacted}:
		default:
		}
	})

	done := make(chan error, 1)
	a.SetOnDone(func(e error) { done <- e })
	a.Submit(context.Background(), "test")

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("turn error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}

	select {
	case ev := <-dispatched:
		if ev.cu.InputTokens <= 0 {
			t.Errorf("dispatched InputTokens = %d, want > 0", ev.cu.InputTokens)
		}
		if ev.cu.ContextWindow <= 0 {
			t.Errorf("dispatched ContextWindow = %d, want > 0", ev.cu.ContextWindow)
		}
		if ev.compacted {
			t.Error("expected Compacted=false for this turn (no compaction threshold crossed)")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for contextUsageDispatcher to be called")
	}
}
