package agent

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"strings"
	"testing"

	"charm.land/fantasy"

	"github.com/mattdurham/wllr/modules/sdk"
	"github.com/mattdurham/wllr/modules/testutil"
)

// captureContextDispatches installs a dispatcher that records every dispatch.
// dispatchContextUsage invokes the callback synchronously, so the slice is
// populated by the time the call under test returns.
func captureContextDispatches(pool *AgentPool) *[]sdk.ContextUsage {
	var got []sdk.ContextUsage
	pool.SetContextUsageDispatcher(func(cu sdk.ContextUsage, _ *CompactionNotice, _ int) {
		got = append(got, cu)
	})
	return &got
}

// TestSeedContextBaseline_MainDispatchesEstimate verifies that a session-start
// seed populates the ctx indicator before any turn runs: the baseline reflects
// the assembled system prompt and the main agent's window is the denominator.
func TestSeedContextBaseline_MainDispatchesEstimate(t *testing.T) {
	pool := NewPool()
	a, err := pool.Spawn(
		MainAgentID,
		testutil.NewFakeLMWithResponses("ok"),
		SpawnOpts{ModelName: "fake-model", ContextWindow: 200_000},
	)
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	a.SetSystemPrompt(strings.Repeat("x", 4000)) // ~1000 tokens by chars/4

	got := captureContextDispatches(pool)
	a.SeedContextBaseline(pool, false)

	if len(*got) != 1 {
		t.Fatalf("dispatches = %d, want 1", len(*got))
	}
	cu := (*got)[0]
	if cu.InputTokens <= 0 {
		t.Errorf("dispatched InputTokens = %d, want > 0 (system prompt seed)", cu.InputTokens)
	}
	if cu.ContextWindow != 200_000 {
		t.Errorf("dispatched ContextWindow = %d, want 200000", cu.ContextWindow)
	}
	if a.LastUsage().InputTokens != cu.InputTokens {
		t.Errorf("LastUsage() = %d, want the dispatched estimate %d", a.LastUsage().InputTokens, cu.InputTokens)
	}
}

// TestSetAgentHistory_ReseedsStaleUsage is the history-restore fix: replacing
// the conversation must discard the previous conversation's provider-reported
// usage and reseed from the restored history, so the statusline does not show
// the old conversation's context size until the next turn.
func TestSetAgentHistory_ReseedsStaleUsage(t *testing.T) {
	pool := NewPool()
	a, err := pool.Spawn(
		MainAgentID,
		testutil.NewFakeLMWithResponses("ok"),
		SpawnOpts{ModelName: "fake-model", ContextWindow: 200_000},
	)
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	a.SetSystemPrompt(strings.Repeat("x", 400))

	// Simulate a completed turn on the previous conversation.
	a.setLastUsage(fantasy.Usage{InputTokens: 999_999})
	a.markUsageReported()

	got := captureContextDispatches(pool)
	if err := pool.SetAgentHistory(MainAgentID, []sdk.Message{
		{Role: sdk.RoleUser, Content: strings.Repeat("y", 8000)},
		{Role: sdk.RoleAssistant, Content: strings.Repeat("z", 8000)},
	}); err != nil {
		t.Fatalf("SetAgentHistory: %v", err)
	}

	lu := a.LastUsage().InputTokens
	if lu <= 0 || lu >= 999_999 {
		t.Fatalf("LastUsage after restore = %d, want a fresh estimate in (0, 999999)", lu)
	}
	if len(*got) != 1 {
		t.Fatalf("dispatches = %d, want 1 (restore reseed)", len(*got))
	}
	if (*got)[0].InputTokens != lu {
		t.Errorf("dispatched = %d, want LastUsage %d", (*got)[0].InputTokens, lu)
	}

	a.lastUsageMu.RLock()
	defer a.lastUsageMu.RUnlock()
	if a.usageReported {
		t.Error("usageReported still true after restore; stale usage would not be replaced")
	}
	if !a.usageIsEstimate {
		t.Error("usageIsEstimate false after restore; first real report would peak instead of replace")
	}
}

// TestSeedContextBaseline_ReportedUsageIsRetained verifies the latch contract:
// with resetReported false, a seed must never clobber provider-reported usage.
func TestSeedContextBaseline_ReportedUsageIsRetained(t *testing.T) {
	pool := NewPool()
	a, err := pool.Spawn(
		MainAgentID,
		testutil.NewFakeLMWithResponses("ok"),
		SpawnOpts{ModelName: "fake-model", ContextWindow: 100_000},
	)
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	a.SetSystemPrompt(strings.Repeat("x", 400))
	a.setLastUsage(fantasy.Usage{InputTokens: 5555})
	a.markUsageReported()

	a.SeedContextBaseline(pool, false)

	if got := a.LastUsage().InputTokens; got != 5555 {
		t.Errorf("LastUsage = %d, want retained 5555 (seed is a no-op once usage is reported)", got)
	}
}

// TestSeedContextBaseline_SubAgentNoDispatch verifies the main-indicator guard:
// a sub-agent's baseline is recorded on itself but never dispatched, matching
// every other context-usage dispatch.
func TestSeedContextBaseline_SubAgentNoDispatch(t *testing.T) {
	pool := NewPool()
	a, err := pool.Spawn("worker", testutil.NewFakeLMWithResponses("ok"), SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	a.SetSystemPrompt(strings.Repeat("x", 400))

	got := captureContextDispatches(pool)
	a.SeedContextBaseline(pool, false)

	if len(*got) != 0 {
		t.Fatalf("sub-agent seed dispatched %d times; only main may drive the indicator", len(*got))
	}
	if a.LastUsage().InputTokens <= 0 {
		t.Error("sub-agent baseline should still be recorded on the agent itself")
	}
}
