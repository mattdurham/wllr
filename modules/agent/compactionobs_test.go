package agent

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// compactionobs_test.go tests the compaction observability wiring: the
// per-session successful-compaction counter, the usage surfaced by a
// compaction run, and the counter propagated through the context-usage
// dispatch. It lives in package agent (white-box) to reach unexported state.

import (
	"context"
	"fmt"
	"strings"
	"sync"
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

	res, err := compactHistory(
		context.Background(),
		&compactTestLM{response: "s", inputTok: 1, outputTok: 1},
		longHistory(212),
		"",
		0,
		CompactionTriggerUsage,
	)
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

	res, err := compactHistory(
		context.Background(),
		&compactTestLM{response: "s"},
		longHistory(2),
		"",
		0,
		CompactionTriggerProactive,
	)
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
		cu          sdk.ContextUsage
		notice      *CompactionNotice
		compactions int
	}
	dispatched := make(chan dispatchEvent, 8)
	pool.SetContextUsageDispatcher(func(cu sdk.ContextUsage, notice *CompactionNotice, compactions int) {
		select {
		case dispatched <- dispatchEvent{cu, notice, compactions}:
		default:
		}
	})

	// Collect the streamed tokens so the test can assert the old fake
	// "[Compacting context…]" marker no longer pollutes the response stream.
	var tokenMu sync.Mutex
	var collected strings.Builder
	a.SetOnToken(func(tok string) {
		tokenMu.Lock()
		collected.WriteString(tok)
		tokenMu.Unlock()
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

	// Two dispatches are expected: the immediate post-compaction notice (with
	// the trigger, message count, and a post-compaction estimate) and the
	// end-of-turn usage event (nil notice). Drain until quiet, then require both.
	var sawNotice, sawTurnEnd bool
	for {
		select {
		case ev := <-dispatched:
			if ev.notice != nil {
				sawNotice = true
				if ev.notice.Trigger != CompactionTriggerUsage {
					t.Errorf("notice Trigger = %q, want %q", ev.notice.Trigger, CompactionTriggerUsage)
				}
				if ev.notice.MessagesCompacted <= 0 {
					t.Errorf("notice MessagesCompacted = %d, want > 0", ev.notice.MessagesCompacted)
				}
				if ev.notice.EstimatedInputTokens <= 0 {
					t.Errorf("notice EstimatedInputTokens = %d, want > 0 (post-compaction estimate)", ev.notice.EstimatedInputTokens)
				}
				if ev.cu.InputTokens <= 0 {
					t.Errorf("notice dispatch InputTokens = %d, want > 0", ev.cu.InputTokens)
				}
				if ev.cu.ContextWindow != 200_000 {
					t.Errorf("notice dispatch ContextWindow = %d, want 200000", ev.cu.ContextWindow)
				}
				if ev.compactions != 1 {
					t.Errorf("notice dispatch Compactions = %d, want 1", ev.compactions)
				}
			} else {
				sawTurnEnd = true
				if ev.compactions != 1 {
					t.Errorf("turn-end dispatch Compactions = %d, want 1", ev.compactions)
				}
			}
			continue
		case <-time.After(300 * time.Millisecond):
		}
		break
	}
	if !sawNotice {
		t.Error("no post-compaction notice dispatch observed")
	}
	if !sawTurnEnd {
		t.Error("no end-of-turn context-usage dispatch observed")
	}

	// The compaction must be recorded in the canonical transcript so recall can
	// surface the fact that older detail was summarized away.
	foundCompactionRecord := false
	for _, entry := range a.CanonicalTranscript().Snapshot() {
		if strings.Contains(entry.Content, "Context compacted:") {
			foundCompactionRecord = true
			break
		}
	}
	if !foundCompactionRecord {
		t.Error("canonical transcript missing compaction record")
	}

	tokenMu.Lock()
	streamed := collected.String()
	tokenMu.Unlock()
	if strings.Contains(streamed, "[Compacting context") {
		t.Error("fake '[Compacting context…]' token marker leaked into the response stream")
	}
}

// reactiveLM fails the first Stream call with a provider context-too-long
// error (the reactive compaction trigger), then serves normal streams for the
// compaction summarization call and the retried turn.
type reactiveLM struct {
	calls int
}

func (r *reactiveLM) Model() string    { return "reactive-model" }
func (r *reactiveLM) Provider() string { return "test" }

func (r *reactiveLM) Generate(_ context.Context, _ fantasy.Call) (*fantasy.Response, error) {
	return &fantasy.Response{}, nil
}

func (r *reactiveLM) GenerateObject(_ context.Context, _ fantasy.ObjectCall) (*fantasy.ObjectResponse, error) {
	return nil, nil
}

func (r *reactiveLM) StreamObject(_ context.Context, _ fantasy.ObjectCall) (fantasy.ObjectStreamResponse, error) {
	return nil, nil
}

func (r *reactiveLM) Stream(_ context.Context, _ fantasy.Call) (fantasy.StreamResponse, error) {
	r.calls++
	if r.calls == 1 {
		return nil, fmt.Errorf("prompt is too long: 250000 tokens > 200000 maximum")
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

// TestExecuteTurn_ReactiveCompaction_DispatchesNotice pins the reactive-path
// wiring: when the provider rejects the context, the compact-and-retry path
// must announce the compaction through the context-usage dispatcher (trigger
// "reactive") instead of streaming the old fake "[Context limit reached…]"
// token marker, which used to be persisted as part of the assistant message.
func TestExecuteTurn_ReactiveCompaction_DispatchesNotice(t *testing.T) {
	pool := NewPool()
	lm := &reactiveLM{}
	a, err := pool.Spawn(MainAgentID, lm, SpawnOpts{ContextWindow: 200_000})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	// History sized so the proactive checks stay quiet (estimate ≈21k tokens
	// vs the ~183k proactive threshold, no prior usage for the usage trigger)
	// while reactive compaction still has more than the 20k keep-recent budget
	// to fold.
	a.historyMu.Lock()
	a.history = longHistory(212)
	a.historyMu.Unlock()

	type dispatchEvent struct {
		cu     sdk.ContextUsage
		notice *CompactionNotice
	}
	dispatched := make(chan dispatchEvent, 8)
	pool.SetContextUsageDispatcher(func(cu sdk.ContextUsage, notice *CompactionNotice, _ int) {
		select {
		case dispatched <- dispatchEvent{cu, notice}:
		default:
		}
	})

	var tokenMu sync.Mutex
	var collected strings.Builder
	a.SetOnToken(func(tok string) {
		tokenMu.Lock()
		collected.WriteString(tok)
		tokenMu.Unlock()
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

	if lm.calls != 3 {
		t.Errorf("LM Stream calls = %d, want 3 (failed turn + summary + retried turn)", lm.calls)
	}

	var sawNotice bool
	for {
		select {
		case ev := <-dispatched:
			if ev.notice != nil {
				sawNotice = true
				if ev.notice.Trigger != CompactionTriggerReactive {
					t.Errorf("notice Trigger = %q, want %q", ev.notice.Trigger, CompactionTriggerReactive)
				}
				if ev.notice.MessagesCompacted <= 0 {
					t.Errorf("notice MessagesCompacted = %d, want > 0", ev.notice.MessagesCompacted)
				}
			}
			continue
		case <-time.After(300 * time.Millisecond):
		}
		break
	}
	if !sawNotice {
		t.Error("no reactive post-compaction notice dispatch observed")
	}

	tokenMu.Lock()
	streamed := collected.String()
	tokenMu.Unlock()
	if strings.Contains(streamed, "[Context limit reached") {
		t.Error("fake '[Context limit reached…]' token marker leaked into the response stream")
	}
	if !strings.Contains(streamed, "recovered") {
		t.Errorf("retried turn response missing from stream: %q", streamed)
	}
}
