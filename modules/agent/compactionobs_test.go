package agent

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// compactionobs_test.go tests the compaction observability wiring: the
// per-session successful-compaction counter, the usage surfaced by a
// compaction run, and the counter propagated through the context-usage
// dispatch. It lives in package agent (white-box) to reach unexported state.

import (
	"context"
	"strings"
	"testing"
	"time"

	"charm.land/fantasy"
	"github.com/mattdurham/wllr/modules/sdk"
)

// obsUsageLM streams a fixed text response then a finish part carrying
// explicit usage — a real stream turn for the executeTurn test.
type obsUsageLM struct {
	tokens       []string
	inputTokens  int64
	outputTokens int64
}

var _ fantasy.LanguageModel = (*obsUsageLM)(nil)

func (u *obsUsageLM) Model() string    { return "obs-usage-model" }
func (u *obsUsageLM) Provider() string { return "test" }

func (u *obsUsageLM) Stream(_ context.Context, _ fantasy.Call) (fantasy.StreamResponse, error) {
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

func (u *obsUsageLM) Generate(_ context.Context, _ fantasy.Call) (*fantasy.Response, error) {
	return &fantasy.Response{}, nil
}

func (u *obsUsageLM) GenerateObject(_ context.Context, _ fantasy.ObjectCall) (*fantasy.ObjectResponse, error) {
	return nil, nil
}

func (u *obsUsageLM) StreamObject(_ context.Context, _ fantasy.ObjectCall) (fantasy.ObjectStreamResponse, error) {
	return nil, nil
}

// longHistory builds an alternating user/assistant history of 400-char
// messages: 212 × 100 tokens = 21,200 tokens, over the 20,000-token default
// keep-recent budget so a real compaction run occurs. The first message is a
// distinct anchor.
func longHistory(n int) []sdk.Message {
	msg := strings.Repeat("x", 400)
	h := make([]sdk.Message, n)
	for i := range h {
		if i%2 == 0 {
			h[i] = sdk.Message{Role: sdk.RoleUser, Content: msg}
		} else {
			h[i] = sdk.Message{Role: sdk.RoleAssistant, Content: msg}
		}
	}
	if n > 0 {
		h[0].Content = "anchor task"
	}
	return h
}

// TestCompactHistory_UsageSurfaced verifies the summarization call's token
// usage is carried in the result on success and stays zero on no-op runs.
func TestCompactHistory_UsageSurfaced(t *testing.T) {
	lm := &compactTestLM{response: "the summary", inputTok: 4000, outputTok: 500}

	res, err := compactHistory(context.Background(), lm, longHistory(212), "", 0, CompactionTriggerUsage)
	if err != nil {
		t.Fatalf("compactHistory: %v", err)
	}
	if res.Usage.InputTokens != 4000 || res.Usage.OutputTokens != 500 {
		t.Errorf("usage = %+v, want InputTokens=4000 OutputTokens=500", res.Usage)
	}
	if res.Trigger != CompactionTriggerUsage {
		t.Errorf("trigger = %q, want %q", res.Trigger, CompactionTriggerUsage)
	}
	if res.Messages <= 0 {
		t.Errorf("messages_compacted = %d, want > 0", res.Messages)
	}

	// No-op run: usage and summary must stay zero.
	resNoop, err := compactHistory(context.Background(), lm, longHistory(2), "", 0, CompactionTriggerProactive)
	if err != nil {
		t.Fatalf("compactHistory (no-op): %v", err)
	}
	if resNoop.Summary != "" {
		t.Errorf("no-op run reported summary %q", resNoop.Summary)
	}
	if resNoop.Usage.InputTokens != 0 || resNoop.Usage.OutputTokens != 0 {
		t.Errorf("no-op run reported usage %+v, want zero", resNoop.Usage)
	}
}

// TestObserveCompaction_Summary_IncrementsCounter verifies that a compaction
// run that produced a summary increments the per-session counter.
func TestObserveCompaction_Summary_IncrementsCounter(t *testing.T) {
	a := &Agent{id: "obs-test", modelName: "obs-test"}
	if got := a.CompactionCount(); got != 0 {
		t.Fatalf("initial CompactionCount = %d, want 0", got)
	}

	res, err := compactHistory(context.Background(), &compactTestLM{response: "s", inputTok: 1, outputTok: 1}, longHistory(212), "", 0, CompactionTriggerUsage)
	if err != nil {
		t.Fatalf("compactHistory: %v", err)
	}
	if res.Summary == "" {
		t.Fatal("expected a summary from a long history")
	}
	a.observeCompaction(res)
	a.observeCompaction(res)

	if got := a.CompactionCount(); got != 2 {
		t.Errorf("CompactionCount after two compactions = %d, want 2", got)
	}
}

// TestObserveCompaction_NoOp_DoesNotCount verifies the invariant that runs
// which did not actually compact (empty summary) increment nothing.
func TestObserveCompaction_NoOp_DoesNotCount(t *testing.T) {
	a := &Agent{id: "obs-test", modelName: "obs-test"}

	res, err := compactHistory(context.Background(), &compactTestLM{response: "s"}, longHistory(2), "", 0, CompactionTriggerProactive)
	if err != nil {
		t.Fatalf("compactHistory: %v", err)
	}
	if res.Summary != "" {
		t.Fatalf("expected no-op compaction, got summary %q", res.Summary)
	}
	a.observeCompaction(res)

	if got := a.CompactionCount(); got != 0 {
		t.Errorf("CompactionCount after no-op compaction = %d, want 0", got)
	}
}

// TestExecuteTurn_CompactionCounterIncrementsAndDispatches drives a full turn
// on an agent whose seeded history forces proactive compaction, verifying the
// counter increments and the context-usage dispatcher observes it.
func TestExecuteTurn_CompactionCounterIncrementsAndDispatches(t *testing.T) {
	pool := NewPool()
	pool.SetContextWindow(200_000)

	lm := &obsUsageLM{tokens: []string{"response"}, inputTokens: 30_000, outputTokens: 100}
	a, err := pool.Spawn(MainAgentID, lm, SpawnOpts{ContextWindow: 200_000})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	type dispatchEvent struct {
		compacted   bool
		compactions int
	}
	dispatched := make(chan dispatchEvent, 1)
	pool.SetContextUsageDispatcher(func(cu sdk.ContextUsage, compact bool, thresholdPct float64, compactions int) {
		select {
		case dispatched <- dispatchEvent{compact, compactions}:
		default:
		}
	})

	// Seed a history that exceeds the compaction budget.
	a.historyMu.Lock()
	a.history = longHistory(212)
	a.historyMu.Unlock()

	// Seed prior-turn usage above the 0.80 threshold (160k of 200k) so the
	// usage-threshold trigger fires during this turn's preflight.
	a.setLastUsage(fantasy.Usage{InputTokens: 170_000})

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

	if got := a.CompactionCount(); got != 1 {
		t.Errorf("CompactionCount = %d, want 1 (one successful compaction)", got)
	}

	select {
	case ev := <-dispatched:
		if !ev.compacted {
			t.Error("expected Compacted=true in the context-usage event")
		}
		if ev.compactions != 1 {
			t.Errorf("dispatched Compactions = %d, want 1", ev.compactions)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for context-usage dispatch")
	}
}
