package agent

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// compactnow_test.go covers the compaction usage re-base: a successful
// compaction must re-point the carried usage baseline at the post-compaction
// context size (so the ctx indicator and the usage-threshold trigger no longer
// stay pinned to the pre-compaction peak), and the manual /compact path must
// fold history through the same machinery.

import (
	"context"
	"testing"
	"time"

	"charm.land/fantasy"

	"github.com/mattdurham/wllr/modules/sdk"
)

// TestObserveCompaction_RebasesUsageBaseline pins the core fix: a successful
// compaction replaces the carried (peak) usage with the post-compaction
// estimate instead of leaving the indicator pinned to the pre-compaction peak.
func TestObserveCompaction_RebasesUsageBaseline(t *testing.T) {
	a := &Agent{id: "rebase-test", modelName: "rebase-test"}
	a.lastUsage = fantasy.Usage{InputTokens: 180_000}
	a.usageReported = true

	a.observeCompaction(CompactionResult{
		Summary:         "folded",
		Trigger:         CompactionTriggerProactive,
		Messages:        100,
		EstimatedTokens: 12_000,
	})

	got := a.LastUsage().InputTokens
	if got != 12_000 {
		t.Errorf("LastUsage after compaction = %d, want 12000 (post-compaction estimate)", got)
	}
	a.lastUsageMu.RLock()
	est := a.usageIsEstimate
	a.lastUsageMu.RUnlock()
	if !est {
		t.Error("usageIsEstimate = false after compaction, want true (post-compaction usage is an estimate)")
	}
}

// TestObserveCompaction_NoOp_DoesNotRebase verifies that a no-op compaction
// (empty summary) leaves the carried usage untouched.
func TestObserveCompaction_NoOp_DoesNotRebase(t *testing.T) {
	a := &Agent{id: "rebase-noop", modelName: "rebase-noop"}
	a.lastUsage = fantasy.Usage{InputTokens: 180_000}
	a.usageReported = true

	a.observeCompaction(CompactionResult{Trigger: CompactionTriggerProactive, EstimatedTokens: 12_000})

	if got := a.LastUsage().InputTokens; got != 180_000 {
		t.Errorf("LastUsage after no-op compaction = %d, want 180000 unchanged", got)
	}
}

// TestRebaseUsageAfterCompaction_NextSmallerStepReplaces verifies the estimate
// latch: after a compaction re-base, a subsequent provider step whose total is
// smaller than the (overestimating) baseline still replaces it outright,
// instead of the peak rule pinning the display to the overestimate.
func TestRebaseUsageAfterCompaction_NextSmallerStepReplaces(t *testing.T) {
	a := &Agent{id: "rebase-latch", modelName: "rebase-latch"}
	a.lastUsage = fantasy.Usage{InputTokens: 8_000}

	// The compaction estimate deliberately overestimates the real size.
	a.rebaseUsageAfterCompaction(50_000)

	// The first real post-compaction step is smaller than the estimate but must
	// win (replace outright), matching the seed rule.
	a.observeStepUsage(fantasy.Usage{InputTokens: 20_000, TotalTokens: 20_000}, nil, 0)

	if got := a.LastUsage().InputTokens; got != 20_000 {
		t.Errorf("LastUsage after first real step = %d, want 20000 (replace outright)", got)
	}
}

// TestCompactNow_FoldsHistoryAndRebases drives the manual /compact path end to
// end on a spawned agent: it must fold history, increment the compaction
// counter, tag the trigger as manual, and re-base the carried usage baseline.
func TestCompactNow_FoldsHistoryAndRebases(t *testing.T) {
	pool := NewPool()
	lm := &compactTestLM{response: "the manual summary"}
	a, err := pool.Spawn(MainAgentID, lm, SpawnOpts{ContextWindow: 200_000})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	a.historyMu.Lock()
	a.history = longHistory(212)
	before := len(a.history)
	a.historyMu.Unlock()

	// A pre-compaction peak well above the post-compaction size.
	a.setLastUsage(fantasy.Usage{InputTokens: 190_000})
	a.markUsageReported()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := a.CompactNow(ctx)
	if err != nil {
		t.Fatalf("CompactNow: %v", err)
	}
	if result.Summary == "" {
		t.Fatal("CompactNow returned an empty summary for a long history")
	}
	if result.Trigger != CompactionTriggerManual {
		t.Errorf("trigger = %q, want %q", result.Trigger, CompactionTriggerManual)
	}
	if got := a.CompactionCount(); got != 1 {
		t.Errorf("CompactionCount = %d, want 1", got)
	}
	if after := len(a.History()); after >= before {
		t.Errorf("history length after compaction = %d, want < %d", after, before)
	}
	if got := a.LastUsage().InputTokens; got <= 0 || got >= 190_000 {
		t.Errorf("LastUsage after manual compaction = %d, want > 0 and < 190000", got)
	}
}

// TestCompactNow_NoOpWhenHistoryFits verifies the manual path reports a no-op
// (empty summary) without touching history or the counter when nothing is
// foldable.
func TestCompactNow_NoOpWhenHistoryFits(t *testing.T) {
	pool := NewPool()
	lm := &compactTestLM{response: "unused"}
	a, err := pool.Spawn(MainAgentID, lm, SpawnOpts{ContextWindow: 200_000})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	a.historyMu.Lock()
	a.history = []sdk.Message{{Role: sdk.RoleUser, Content: "hi"}, {Role: sdk.RoleAssistant, Content: "hello"}}
	a.historyMu.Unlock()

	result, err := a.CompactNow(context.Background())
	if err != nil {
		t.Fatalf("CompactNow: %v", err)
	}
	if result.Summary != "" {
		t.Errorf("summary = %q, want empty (nothing to fold)", result.Summary)
	}
	if got := a.CompactionCount(); got != 0 {
		t.Errorf("CompactionCount = %d, want 0", got)
	}
}

// TestCompactMainAgent_RefusesWhileRunning verifies the pool wrapper refuses to
// compact a mid-turn agent (compaction rewrites history a running turn holds)
// and reports nothing to compact otherwise.
func TestCompactMainAgent_RefusesWhileRunning(t *testing.T) {
	pool := NewPool()
	lm := &compactTestLM{response: "unused"}
	a, err := pool.Spawn(MainAgentID, lm, SpawnOpts{ContextWindow: 200_000})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	a.historyMu.Lock()
	a.history = longHistory(212)
	a.historyMu.Unlock()

	a.isRunning.Store(true)
	_, msg, err := pool.CompactMainAgent(context.Background())
	if err != nil {
		t.Fatalf("CompactMainAgent (running): %v", err)
	}
	if msg == "" {
		t.Error("expected a refusal message while a turn is running")
	}
	if got := a.CompactionCount(); got != 0 {
		t.Errorf("CompactionCount = %d, want 0 (refused)", got)
	}

	a.isRunning.Store(false)
	_, msg, err = pool.CompactMainAgent(context.Background())
	if err != nil {
		t.Fatalf("CompactMainAgent (idle): %v", err)
	}
	if msg == "" {
		t.Error("expected a success message when idle")
	}
	if got := a.CompactionCount(); got != 1 {
		t.Errorf("CompactionCount = %d, want 1", got)
	}
}
