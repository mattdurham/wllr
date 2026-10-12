package agent

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// authoritativewindow_test.go covers the provider-authoritative context-window
// clamp: when a context-too-long rejection reports the model's real maximum, the
// agent must size subsequent turns against that number instead of the estimate
// that just failed. Without it the 80% usage trigger keeps computing against a
// window the provider already refused, so every following turn is rejected
// again.
//
// The error shapes mirror what the fantasy adapters produce (structured
// ProviderError, and RetryError wrapping), not this package's own logic.

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"charm.land/fantasy"
)

// contextTooLongErr builds the structured rejection the fantasy adapters emit,
// carrying both the prompt size that was refused and the model's real maximum.
// ProviderError's token fields are int, matching the adapters' parse targets.
func contextTooLongErr(max, used int) error {
	return &fantasy.ProviderError{
		Title:              "context window exceeded",
		Message:            fmt.Sprintf("input tokens [%d] exceed context window [%d]", used, max),
		ContextTooLargeErr: true,
		ContextUsedTokens:  used,
		ContextMaxTokens:   max,
	}
}

// TestAuthoritativeContextWindow covers recovering the reported maximum from
// every shape the error arrives in, including the wrapping the retry middleware
// applies.
func TestAuthoritativeContextWindow(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int64
	}{
		{"nil", nil, 0},
		{"structured", contextTooLongErr(128_000, 200_000), 128_000},
		{"wrapped %w", fmt.Errorf("provider request failed: %w", contextTooLongErr(8_192, 9_000)), 8_192},
		{"RetryError", &fantasy.RetryError{Errors: []error{contextTooLongErr(131_072, 200_000)}}, 131_072},
		{"flag without number", &fantasy.ProviderError{ContextTooLargeErr: true}, 0},
		{"bare string", errors.New("prompt is too long"), 0},
	}
	for _, tc := range cases {
		if got := authoritativeContextWindow(tc.err); got != tc.want {
			t.Errorf("authoritativeContextWindow(%s) = %d, want %d", tc.name, got, tc.want)
		}
	}
}

// TestAdoptAuthoritativeContextWindow_ClampsDown is the core regression guard:
// a rejection that names a smaller maximum must lower the agent's window, lower
// the pool's per-model window, and notify the host so its window cache follows.
func TestAdoptAuthoritativeContextWindow_ClampsDown(t *testing.T) {
	a := &Agent{id: MainAgentID, modelName: "local-model"}
	a.contextWindow = 200_000
	pool := NewPool()

	var (
		fired    bool
		gotModel string
		gotPrev  int64
		gotAuth  int64
	)
	pool.SetContextWindowObserver(func(model string, previous, authoritative int64) {
		fired, gotModel, gotPrev, gotAuth = true, model, previous, authoritative
	})

	a.adoptAuthoritativeContextWindow(pool, "local-model", contextTooLongErr(128_000, 200_000))

	if got := a.ContextWindow(); got != 128_000 {
		t.Errorf("agent window = %d, want 128000", got)
	}
	if got := pool.ContextWindowForModel("local-model"); got != 128_000 {
		t.Errorf("pool window = %d, want 128000", got)
	}
	if !fired {
		t.Fatal("context-window observer not called, want correction reported to the host")
	}
	if gotModel != "local-model" || gotPrev != 200_000 || gotAuth != 128_000 {
		t.Errorf("observer got (%q, %d, %d), want (local-model, 200000, 128000)", gotModel, gotPrev, gotAuth)
	}
}

// TestAdoptAuthoritativeContextWindow_DoesNotRaise pins the downward-only rule.
// A reported maximum at or above the current window cannot explain the
// rejection, so adopting it would inflate the window and suppress compaction.
func TestAdoptAuthoritativeContextWindow_DoesNotRaise(t *testing.T) {
	a := &Agent{id: MainAgentID, modelName: "m"}
	a.contextWindow = 8_192
	pool := NewPool()
	fired := false
	pool.SetContextWindowObserver(func(string, int64, int64) { fired = true })

	a.adoptAuthoritativeContextWindow(pool, "m", contextTooLongErr(128_000, 9_000))

	if got := a.ContextWindow(); got != 8_192 {
		t.Errorf("agent window = %d, want 8192 (unchanged by a larger report)", got)
	}
	if fired {
		t.Error("observer fired for a non-lowering report, want no correction")
	}
}

// TestAdoptAuthoritativeContextWindow_AdoptsWhenUnknown covers a model whose
// window never resolved: the reported maximum is the first real value, so it is
// adopted outright rather than clamped against zero.
func TestAdoptAuthoritativeContextWindow_AdoptsWhenUnknown(t *testing.T) {
	a := &Agent{id: MainAgentID, modelName: "m"}
	pool := NewPool()

	a.adoptAuthoritativeContextWindow(pool, "m", contextTooLongErr(32_768, 40_000))

	if got := a.ContextWindow(); got != 32_768 {
		t.Errorf("agent window = %d, want 32768", got)
	}
}

// TestAdoptAuthoritativeContextWindow_NoNumberLeavesWindow covers a rejection
// with no recoverable maximum (a bare string, or the flag alone) and a nil pool:
// nothing to adopt, nothing to panic on.
func TestAdoptAuthoritativeContextWindow_NoNumberLeavesWindow(t *testing.T) {
	a := &Agent{id: MainAgentID, modelName: "m"}
	a.contextWindow = 200_000

	a.adoptAuthoritativeContextWindow(nil, "m", &fantasy.ProviderError{ContextTooLargeErr: true})
	a.adoptAuthoritativeContextWindow(nil, "m", errors.New("prompt is too long"))

	if got := a.ContextWindow(); got != 200_000 {
		t.Errorf("agent window = %d, want 200000 (unchanged)", got)
	}
}

// reactiveWindowLM rejects the first Stream with a structured
// context-too-long error that names a smaller window than the agent was given,
// then serves the compaction summary and the retried turn. It is the
// end-to-end shape of the bug: the rejection itself carries the correction.
type reactiveWindowLM struct{ calls int }

func (r *reactiveWindowLM) Model() string    { return "reactive-window-model" }
func (r *reactiveWindowLM) Provider() string { return "test" }

func (r *reactiveWindowLM) Generate(_ context.Context, _ fantasy.Call) (*fantasy.Response, error) {
	return &fantasy.Response{}, nil
}

func (r *reactiveWindowLM) GenerateObject(_ context.Context, _ fantasy.ObjectCall) (*fantasy.ObjectResponse, error) {
	return nil, nil
}

func (r *reactiveWindowLM) StreamObject(_ context.Context, _ fantasy.ObjectCall) (fantasy.ObjectStreamResponse, error) {
	return nil, nil
}

func (r *reactiveWindowLM) Stream(_ context.Context, _ fantasy.Call) (fantasy.StreamResponse, error) {
	r.calls++
	if r.calls == 1 {
		return nil, contextTooLongErr(128_000, 150_000)
	}
	return func(yield func(fantasy.StreamPart) bool) {
		yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextDelta, Delta: "recovered"})
		yield(fantasy.StreamPart{
			Type:         fantasy.StreamPartTypeFinish,
			FinishReason: fantasy.FinishReasonStop,
			Usage:        fantasy.Usage{InputTokens: 40_000, OutputTokens: 10, TotalTokens: 40_010},
		})
	}, nil
}

// TestExecuteTurn_ReactiveAdoptsAuthoritativeWindow is the end-to-end guard:
// driving a real turn into a structured context-too-long rejection must clamp
// the agent's window to the reported maximum and report the correction to the
// host, so the turn after this one sizes against the model's real limit.
func TestExecuteTurn_ReactiveAdoptsAuthoritativeWindow(t *testing.T) {
	pool := NewPool()
	lm := &reactiveWindowLM{}
	a, err := pool.Spawn(MainAgentID, lm, SpawnOpts{
		ModelName:     lm.Model(),
		ContextWindow: 200_000,
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	// History long enough for reactive compaction to have a summarizable span,
	// so the turn proceeds past the rejection rather than failing fast.
	a.historyMu.Lock()
	a.history = longHistory(212)
	a.historyMu.Unlock()

	var (
		fired   bool
		gotPrev int64
		gotAuth int64
	)
	pool.SetContextWindowObserver(func(_ string, previous, authoritative int64) {
		fired, gotPrev, gotAuth = true, previous, authoritative
	})

	done := make(chan error, 1)
	a.SetOnDone(func(e error) { done <- e })
	a.Submit(context.Background(), "go")

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("turn error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for turn")
	}

	if got := a.ContextWindow(); got != 128_000 {
		t.Errorf("agent window after reactive rejection = %d, want 128000", got)
	}
	if got := pool.ContextWindowForModel(a.ModelName()); got != 128_000 {
		t.Errorf("pool window after reactive rejection = %d, want 128000", got)
	}
	if !fired || gotPrev != 200_000 || gotAuth != 128_000 {
		t.Errorf("observer = (fired=%v, prev=%d, auth=%d), want (true, 200000, 128000)", fired, gotPrev, gotAuth)
	}
}

// TestAdoptAuthoritativeContextWindow_DrivesUsageTrigger ties the clamp to the
// behaviour it exists for: a prompt that exceeded the model's real window while
// the stale window still reported it as comfortably under the trigger must
// cross the 80% threshold once the real window is adopted — so the next turn
// compacts instead of being rejected again.
func TestAdoptAuthoritativeContextWindow_DrivesUsageTrigger(t *testing.T) {
	a := &Agent{id: MainAgentID, modelName: "m"}
	a.contextWindow = 200_000
	pool := NewPool()

	// 130k prompt served mostly from cache: 30k fresh input + 100k cache read.
	// It already exceeds the model's real 128k window (hence the rejection),
	// but against the stale 200k window it reads as 65%.
	usage := fantasy.Usage{InputTokens: 30_000, CacheReadTokens: 100_000}
	if shouldCompactByUsage(usage, a.ContextWindow(), 0.80) {
		t.Fatal("precondition: 130k of the stale 200k window should sit below the 80% trigger")
	}

	a.adoptAuthoritativeContextWindow(pool, "m", contextTooLongErr(128_000, 130_000))

	if !shouldCompactByUsage(usage, a.ContextWindow(), 0.80) {
		t.Error("after adopting the 128k window, a 130k prompt must exceed the 80% trigger")
	}
}
