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
// CacheReadTokens models the OpenAI-family convention where cached tokens are
// reported separately from InputTokens (which is prompt − cached).
type usageLM struct {
	tokens            []string
	inputTokens       int64
	outputTokens      int64
	cacheReadTokens   int64
	cacheCreateTokens int64
}

func (u *usageLM) Model() string    { return "usage-model" }
func (u *usageLM) Provider() string { return "test" }

func (u *usageLM) Stream(_ context.Context, _ fantasy.Call) (fantasy.StreamResponse, error) {
	toks := u.tokens
	usage := fantasy.Usage{
		InputTokens:         u.inputTokens,
		OutputTokens:        u.outputTokens,
		TotalTokens:         u.inputTokens + u.outputTokens,
		CacheReadTokens:     u.cacheReadTokens,
		CacheCreationTokens: u.cacheCreateTokens,
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

// TestStreamTurnUsageZeroOnError verifies that when an agent's first turn fails,
// LastUsage stays zero-valued: there is no prior successful turn whose context
// size could be retained.
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

// TestPoolMainAgentContextUsage_UsesAgentWindow pins the display denominator:
// the usage must be computed against the main agent's own resolved window (the
// one its turns and compaction actually use), not the pool's default-model
// window. The two can legitimately diverge — a pool default set for one model
// while the agent runs another — and when they do, the old pool-window display
// reported a percentage that disagreed with every compaction decision.
func TestPoolMainAgentContextUsage_UsesAgentWindow(t *testing.T) {
	pool := agent.NewPool()
	pool.SetContextWindow(1_000_000) // pool default-model decoy

	lm := &usageLM{
		tokens:       []string{"output"},
		inputTokens:  50_000,
		outputTokens: 500,
	}
	_, err := pool.Spawn(agent.MainAgentID, lm, agent.SpawnOpts{
		ModelName: "agent-window-model", ContextWindow: 200_000,
	})
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
	if cu.ContextWindow != 200_000 {
		t.Errorf("ContextUsage.ContextWindow = %d, want 200000 (the agent's window, not the pool's 1000000)", cu.ContextWindow)
	}
	if cu.Percent < 24.9 || cu.Percent > 25.1 {
		t.Errorf("ContextUsage.Percent = %f, want ~25 (50000/200000)", cu.Percent)
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

// TestAgentLastUsageRetainedOnError pins the error-turn display semantics: a
// failed turn keeps the last-known usage rather than zeroing it. The context
// does not shrink because a turn failed, and LastUsage feeds both the statusline
// context indicator (which visibly dropped to ctx:0 after any error) and the
// usage-threshold compaction trigger (which silently stopped firing after one).
// Failed-turn accounting is carried by TurnUsage.Err, not by wiping this value.
func TestAgentLastUsageRetainedOnError(t *testing.T) {
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

	// Second turn: fails, and LastUsage must retain the first turn's values.
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
	if u.InputTokens != 1200 || u.OutputTokens != 50 {
		t.Errorf("LastUsage after error = {InputTokens:%d, OutputTokens:%d}, want retained {1200, 50}", u.InputTokens, u.OutputTokens)
	}
}

// TestPoolMainAgentContextUsage_IncludesCacheTokens pins the display numerator:
// cached tokens count toward used context. OpenAI-family providers report
// InputTokens as prompt − cached, so a heavily (or fully) cache-served turn
// reports near-zero InputTokens even though the prompt fills the window. The
// display must sum InputTokens + CacheRead + CacheCreation — the only formula
// that is correct on every provider given their differing cache conventions.
func TestPoolMainAgentContextUsage_IncludesCacheTokens(t *testing.T) {
	pool := agent.NewPool()
	pool.SetContextWindow(200_000)

	lm := &usageLM{
		tokens:          []string{"output"},
		inputTokens:     0,
		cacheReadTokens: 90_000, // fully cache-served turn: raw InputTokens = 0
		outputTokens:    500,
	}
	// Explicit window: SetContextWindow with an empty default model name does
	// not populate the per-model map, and Spawn would otherwise fall through to
	// contextWindowForModel("") whose longest-substring match is arbitrary.
	_, err := pool.Spawn(agent.MainAgentID, lm, agent.SpawnOpts{ContextWindow: 200_000})
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
	if cu.InputTokens != 90_000 {
		t.Errorf("ContextUsage.InputTokens = %d, want 90000 (cache reads counted)", cu.InputTokens)
	}
	if cu.Percent < 44.9 || cu.Percent > 45.1 {
		t.Errorf("ContextUsage.Percent = %f, want ~45 (90000/200000)", cu.Percent)
	}
}

// TestEventContextUsageDispatchedOnErrorRetainsLastKnown verifies that a failed
// main-agent turn re-dispatches the retained usage instead of nothing: the
// statusline must refresh to the last-known context size after an error, not
// stay stuck or drop to zero.
func TestEventContextUsageDispatchedOnErrorRetainsLastKnown(t *testing.T) {
	pool := agent.NewPool()
	pool.SetContextWindow(200_000)

	lm := &successThenErrLM{inputTokens: 1200, outputTokens: 50}
	a, err := pool.Spawn(agent.MainAgentID, lm, agent.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	type dispatchEvent struct {
		cu     sdk.ContextUsage
		notice *agent.CompactionNotice
	}
	dispatched := make(chan dispatchEvent, 4)
	pool.SetContextUsageDispatcher(func(cu sdk.ContextUsage, notice *agent.CompactionNotice, _ int) {
		select {
		case dispatched <- dispatchEvent{cu, notice}:
		default:
		}
	})

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

	a.SetOnDone(func(e error) { done <- e })
	a.Submit(context.Background(), "second turn")
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected second turn to fail")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for second turn")
	}

	// Drain dispatches until the retained-usage one arrives.
	deadline := time.After(2 * time.Second)
	for {
		select {
		case ev := <-dispatched:
			if ev.cu.InputTokens == 1200 {
				return // retained usage re-dispatched after the failed turn
			}
		case <-deadline:
			t.Fatal("no context-usage dispatch with retained usage after failed turn")
		}
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
		cu          sdk.ContextUsage
		notice      *agent.CompactionNotice
		compactions int
	}
	dispatched := make(chan dispatchEvent, 1)
	pool.SetContextUsageDispatcher(func(cu sdk.ContextUsage, notice *agent.CompactionNotice, compactions int) {
		select {
		case dispatched <- dispatchEvent{cu, notice, compactions}:
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
		if ev.notice != nil {
			t.Error("expected notice=nil for this turn (no compaction ran)")
		}
		if ev.compactions != 0 {
			t.Errorf("expected Compactions=0 for a turn without compaction, got %d", ev.compactions)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for contextUsageDispatcher to be called")
	}
}
