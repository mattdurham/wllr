package agent

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"context"
	"testing"
	"time"

	"charm.land/fantasy"
)

// tpsBase anchors every injected timestamp; only relative distances matter.
var tpsBase = time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

// at returns tpsBase plus ms milliseconds.
func at(ms int) time.Time { return tpsBase.Add(time.Duration(ms) * time.Millisecond) }

func TestTPSRate_Guards(t *testing.T) {
	cases := []struct {
		name         string
		outputTokens int64
		streamed     time.Duration
		want         float64
	}{
		{"no tokens", 0, time.Second, 0},
		{"negative tokens", -5, time.Second, 0},
		{"below min span", 10, 99 * time.Millisecond, 0},
		{"exactly min span", 10, minStreamSpan, 100},
		{"two seconds", 10, 2 * time.Second, 5},
	}
	for _, tc := range cases {
		if got := tpsRate(tc.outputTokens, tc.streamed); got != tc.want {
			t.Errorf("%s: tpsRate(%d, %v) = %v, want %v", tc.name, tc.outputTokens, tc.streamed, got, tc.want)
		}
	}
}

// TestStreamStats_LiveExcludesSilentTail verifies a span contributes only up
// to its last token: time between the final delta and OnStepFinish (or any
// later poll) must not dilute the rate.
func TestStreamStats_LiveExcludesSilentTail(t *testing.T) {
	s := newStreamStats()
	s.stepStart(at(0))
	s.token(at(100), "hello")  // last token of the span at 100ms
	s.stepFinish(at(1000), 10) // closed 900ms later
	// 10 output tokens over the 100ms that actually produced them = 100 t/s.
	// Wall-clock (10/1s) would wrongly report 10.
	if got := s.live(at(5000)); got != 100 {
		t.Errorf("live after silent tail = %v, want 100 (tail must be excluded)", got)
	}
}

// TestStreamStats_MultiStepExcludesToolGap verifies that time between steps —
// fantasy executes the step's tools after OnStepFinish and before the next
// OnStepStart — never lands in the streamed denominator.
func TestStreamStats_MultiStepExcludesToolGap(t *testing.T) {
	s := newStreamStats()
	s.stepStart(at(0))
	s.token(at(1000), "step one")
	s.stepFinish(at(1500), 20)
	// Tool/sub-agent execution gap: 1500ms → 3500ms. Excluded by design.
	s.stepStart(at(3500))
	s.token(at(4500), "step two")
	s.stepFinish(at(6000), 20)
	// Streamed = (1000-0) + (4500-3500) = 2000ms; output = 40 → 20 t/s.
	// Wall clock would report 40/6s ≈ 6.7.
	if got := s.live(at(7000)); got != 20 {
		t.Errorf("live across tool gap = %v, want 20 (gap must be excluded)", got)
	}
}

func TestStreamStats_FinishFreezesExactRate(t *testing.T) {
	s := newStreamStats()
	s.stepStart(at(0))
	s.token(at(500), "hello world")
	s.stepFinish(at(1000), 40)
	s.finish(at(1100), 50, true)
	// reported total (50) wins over per-step sum (40); streamed = 500ms → 100.
	if got := s.exact(); got != 100 {
		t.Errorf("exact with reported usage = %v, want 100", got)
	}
	if got := s.live(at(90000)); got != 100 {
		t.Errorf("live after finish = %v, want frozen 100", got)
	}
}

func TestStreamStats_FinishEstimatesWhenUsageMissing(t *testing.T) {
	s := newStreamStats()
	s.stepStart(at(0))
	s.token(at(500), "abcdabcdab") // 10 chars → 2 estimated tokens
	s.stepFinish(at(600), 0)
	s.finish(at(700), 0, false)
	// No provider usage: 40 per-step tokens are absent, so the char estimate
	// stands in: 2 tokens over 500ms = 4 t/s.
	if got := s.exact(); got != 4 {
		t.Errorf("exact without reported usage = %v, want 4", got)
	}
}

// TestStreamStats_BelowMinSpanHidden verifies a stream faster than
// minStreamSpan reports nothing rather than an absurd rate.
func TestStreamStats_BelowMinSpanHidden(t *testing.T) {
	s := newStreamStats()
	s.stepStart(at(0))
	s.token(at(50), "hi")
	s.stepFinish(at(60), 5)
	if got := s.live(at(100)); got != 0 {
		t.Errorf("live for 50ms span = %v, want 0 (below minStreamSpan)", got)
	}
	s.finish(at(70), 5, true)
	if got := s.exact(); got != 0 {
		t.Errorf("exact for 50ms span = %v, want 0 (below minStreamSpan)", got)
	}
}

// TestStreamStats_TokenReopensSpan verifies the defensive path where deltas
// arrive with no step lifecycle around them: the span opens at the first
// token and contributes up to the last one.
func TestStreamStats_TokenReopensSpan(t *testing.T) {
	s := newStreamStats()
	s.token(at(0), "x")
	s.token(at(200), "y")
	s.stepFinish(at(300), 4)
	if got := s.live(at(400)); got != 20 {
		t.Errorf("live after implicit reopen = %v, want 20", got)
	}

	// A span whose only token lands at its open instant contributes zero
	// duration — there was no measured generation time to divide by.
	s2 := newStreamStats()
	s2.token(at(1000), "x")
	s2.stepFinish(at(1200), 4)
	if got := s2.live(at(1300)); got != 0 {
		t.Errorf("live for zero-duration span = %v, want 0", got)
	}
}

// TestStreamStats_FinishIdempotent verifies a second finish (possible via
// streamTurn's deferred panic guard) does not double-count the estimate.
func TestStreamStats_FinishIdempotent(t *testing.T) {
	s := newStreamStats()
	s.stepStart(at(0))
	s.token(at(500), "abcd") // 1 estimated token
	s.finish(at(600), 0, false)
	first := s.exact()
	s.finish(at(900), 0, false)
	if second := s.exact(); second != first {
		t.Errorf("second finish changed rate: first %v, second %v", first, second)
	}
}

// TestAgent_StreamTps_Lifecycle exercises the agent-level tracker lifecycle
// directly: zero before any turn, live while a tracker is open, frozen after
// endStreamStats, and untouched by a redundant end call.
func TestAgent_StreamTps_Lifecycle(t *testing.T) {
	a := &Agent{}
	if got := a.StreamTps(); got != 0 {
		t.Errorf("StreamTps on fresh agent = %v, want 0", got)
	}

	stats := a.beginStreamStats()
	now := time.Now() // single capture so the folded span is exactly 1s
	stats.stepStart(now.Add(-time.Second))
	stats.token(now, "hello")
	if got := a.StreamTps(); got <= 0 {
		t.Errorf("StreamTps with open tracker = %v, want > 0", got)
	}

	a.endStreamStats(100, true)
	if a.liveStats != nil {
		t.Error("liveStats not cleared by endStreamStats")
	}
	if got := a.StreamTps(); got != 100 {
		t.Errorf("StreamTps after endStreamStats = %v, want frozen 100 (100 tokens / 1s)", got)
	}

	// The deferred guard in streamTurn calls endStreamStats again after the
	// normal path; it must be a no-op.
	a.endStreamStats(0, false)
	if got := a.StreamTps(); got != 100 {
		t.Errorf("StreamTps after redundant endStreamStats = %v, want 100", got)
	}
}

// ---- streaming LM doubles for the end-to-end smoke tests ----

// tpsSleepLM streams single-char tokens with a real sleep between them and
// reports the configured output usage on the finish part, giving the tracker
// a realistically-paced provider stream.
type tpsSleepLM struct {
	tokens     int
	tokenEvery time.Duration
	output     int64
}

func (t *tpsSleepLM) Model() string    { return "tps-sleep" }
func (t *tpsSleepLM) Provider() string { return "test" }

func (t *tpsSleepLM) Stream(_ context.Context, _ fantasy.Call) (fantasy.StreamResponse, error) {
	return func(yield func(fantasy.StreamPart) bool) {
		for i := 0; i < t.tokens; i++ {
			if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextDelta, Delta: "x"}) {
				return
			}
			time.Sleep(t.tokenEvery)
		}
		yield(fantasy.StreamPart{
			Type:         fantasy.StreamPartTypeFinish,
			FinishReason: fantasy.FinishReasonStop,
			Usage: fantasy.Usage{
				InputTokens:  100,
				OutputTokens: t.output,
				TotalTokens:  100 + t.output,
			},
		})
	}, nil
}

func (t *tpsSleepLM) Generate(_ context.Context, _ fantasy.Call) (*fantasy.Response, error) {
	return &fantasy.Response{}, nil
}

func (t *tpsSleepLM) GenerateObject(_ context.Context, _ fantasy.ObjectCall) (*fantasy.ObjectResponse, error) {
	return nil, nil
}

func (t *tpsSleepLM) StreamObject(_ context.Context, _ fantasy.ObjectCall) (fantasy.ObjectStreamResponse, error) {
	return nil, nil
}

// tpsInstantLM finishes immediately with no usage — the everything-tiny case
// whose rate must be suppressed by the minStreamSpan guard.
type tpsInstantLM struct{}

func (t *tpsInstantLM) Model() string    { return "tps-instant" }
func (t *tpsInstantLM) Provider() string { return "test" }

func (t *tpsInstantLM) Stream(_ context.Context, _ fantasy.Call) (fantasy.StreamResponse, error) {
	return func(yield func(fantasy.StreamPart) bool) {
		yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeFinish, FinishReason: fantasy.FinishReasonStop})
	}, nil
}

func (t *tpsInstantLM) Generate(_ context.Context, _ fantasy.Call) (*fantasy.Response, error) {
	return &fantasy.Response{}, nil
}

func (t *tpsInstantLM) GenerateObject(_ context.Context, _ fantasy.ObjectCall) (*fantasy.ObjectResponse, error) {
	return nil, nil
}

func (t *tpsInstantLM) StreamObject(_ context.Context, _ fantasy.ObjectCall) (fantasy.ObjectStreamResponse, error) {
	return nil, nil
}

// TestAgent_StreamTps_EndToEnd drives a real Submit through the full turn
// path (fantasy agent loop → streamTurn hooks → endStreamStats) and checks
// the frozen rate's bounds: the 100 output tokens streamed over ~600ms must
// yield roughly 167 t/s — bounded above by the sleep pacing and strictly
// positive (a zero would mean the tracker never measured).
func TestAgent_StreamTps_EndToEnd(t *testing.T) {
	pool := NewPool()
	lm := &tpsSleepLM{tokens: 10, tokenEvery: 60 * time.Millisecond, output: 100}
	a, err := pool.Spawn("main", lm, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	done := make(chan error, 1)
	a.SetOnDone(func(e error) { done <- e })

	a.Submit(context.Background(), "hi")
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for turn to finish")
	}
	if a.IsRunning() {
		t.Error("agent still running after onDone")
	}

	tps := pool.MainAgentTps() // exercises the pool accessor too
	if tps <= 0 {
		t.Fatalf("MainAgentTps() = %v after a streaming turn, want > 0", tps)
	}
	// Last token lands at ≥ 9×60ms = 540ms after the span opens, so the rate
	// cannot exceed 100/0.54 ≈ 185; use 200 as the CI-friendly ceiling. A
	// value near the wall-clock floor (100 tokens / ~600ms+ ≈ 167) is fine —
	// this smoke test only bounds, the unit tests pin exact values.
	if tps > 200 {
		t.Errorf("MainAgentTps() = %v, want ≤ 200 (streamed denominator must cover ≥540ms of sleeps)", tps)
	}
	// The value persists while idle (mirrors the ctx display).
	if again := pool.MainAgentTps(); again != tps {
		t.Errorf("MainAgentTps() changed while idle: %v → %v", tps, again)
	}
}

// TestAgent_StreamTps_InstantStreamHidden verifies the full turn path reports
// zero — no segment — for an instant, usage-less stream.
func TestAgent_StreamTps_InstantStreamHidden(t *testing.T) {
	pool := NewPool()
	a, err := pool.Spawn("main", &tpsInstantLM{}, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	done := make(chan error, 1)
	a.SetOnDone(func(e error) { done <- e })

	a.Submit(context.Background(), "hi")
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for turn to finish")
	}
	if got := pool.MainAgentTps(); got != 0 {
		t.Errorf("MainAgentTps() = %v for an instant stream, want 0 (hidden)", got)
	}
}
