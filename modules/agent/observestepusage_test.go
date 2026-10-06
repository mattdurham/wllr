package agent

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"charm.land/fantasy"

	"github.com/mattdurham/wllr/modules/sdk"
	"github.com/mattdurham/wllr/modules/testutil"
)

// waitForLiveCtx polls cond until true or the deadline, failing with label.
func waitForLiveCtx(t *testing.T, cond func() bool, label string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", label)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// namedGate is a blocking tool with a configurable name so two gates can be
// distinguished via Activity().ActiveToolName.
type namedGate struct {
	name    string
	release chan struct{}
}

func (g *namedGate) Info() fantasy.ToolInfo {
	return fantasy.ToolInfo{Name: g.name, Description: "blocks", Parallel: false}
}

func (g *namedGate) Run(_ context.Context, _ fantasy.ToolCall) (fantasy.ToolResponse, error) {
	<-g.release
	return fantasy.NewTextResponse("released"), nil
}

func (g *namedGate) ProviderOptions() fantasy.ProviderOptions   { return nil }
func (g *namedGate) SetProviderOptions(fantasy.ProviderOptions) {}

// TestObserveStepUsage_LiveMidTurn reproduces the missing-ctx bug: a long
// orchestrator turn used to leave LastUsage at its previous-turn value and
// EventContextUsage undispatched until the turn ended — so the statusline
// never showed ctx at all in sessions whose main turn ran for hours.
// Per-step recording must make both live while the turn is still running.
//
// Step ordering (probed): OnStepFinish for step N fires after step N's tools
// complete, before step N+1's tools start — exactly when an orchestrator is
// between sleep/status-poll loops.
func TestObserveStepUsage_LiveMidTurn(t *testing.T) {
	lm := testutil.NewFakeLM()
	lm.SetScript([]testutil.ScriptedTurn{
		// Step 1: dispatch gate_a, which returns immediately. Its finish part
		// carries step 1's usage, recorded by observeStepUsage.
		{ToolCalls: []testutil.ScriptedToolCall{
			{ID: "tc1", Name: "gate_a", Input: json.RawMessage(`{}`)},
		}},
		// Step 2: dispatch gate_b, which blocks — the "hours-long turn" stand-in.
		{ToolCalls: []testutil.ScriptedToolCall{
			{ID: "tc2", Name: "gate_b", Input: json.RawMessage(`{}`)},
		}},
		{Text: "done"},
	})
	pool := NewPool()

	var mu sync.Mutex
	var dispatches []sdk.ContextUsage
	pool.SetContextUsageDispatcher(func(cu sdk.ContextUsage, _ *CompactionNotice, _ int) {
		mu.Lock()
		dispatches = append(dispatches, cu)
		mu.Unlock()
	})

	a, err := pool.Spawn(MainAgentID, lm, SpawnOpts{ModelName: "fake-model", ContextWindow: 100_000})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	relA := make(chan struct{})
	relB := make(chan struct{})
	close(relA)
	a.SetToolsFn(func() []fantasy.AgentTool {
		return []fantasy.AgentTool{
			&namedGate{name: "gate_a", release: relA},
			&namedGate{name: "gate_b", release: relB},
		}
	})
	done := make(chan error, 1)
	a.SetOnDone(func(err error) { done <- err })

	a.Submit(context.Background(), "start work on something reasonably long please")
	waitForLiveCtx(t, func() bool { return a.Activity().ActiveToolName == "gate_b" }, "gate_b to start")

	// Mid-turn: step 1 has fully finished (its OnStepFinish ran before gate_b
	// could start), so its usage must already be visible and the per-step
	// dispatch must have fired — while the turn is still running.
	mid := a.LastUsage()
	if mid.InputTokens <= 0 {
		t.Fatalf("mid-turn LastUsage = %+v, want step 1 input usage > 0", mid)
	}
	mu.Lock()
	midDispatches := len(dispatches)
	var windowOK bool
	for _, cu := range dispatches {
		if cu.ContextWindow == 100_000 && cu.InputTokens > 0 {
			windowOK = true
		}
	}
	mu.Unlock()
	if midDispatches == 0 || !windowOK {
		t.Fatalf("mid-turn dispatches = %d (windowOK=%v), want >=1 with the agent's window and usage", midDispatches, windowOK)
	}

	close(relB)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("turn errored: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("turn did not finish")
	}

	// Turn end: the stored peak is retained (the fake's input usage derives
	// from the last user message, so it is constant across steps here —
	// equality is the growth-free expectation), and the dispatch count grew
	// with step 2's per-step dispatch plus the turn-end dispatch.
	if final := a.LastUsage(); final.InputTokens < mid.InputTokens {
		t.Fatalf("final LastUsage input = %d, want >= mid-turn %d", final.InputTokens, mid.InputTokens)
	}
	mu.Lock()
	total := len(dispatches)
	mu.Unlock()
	if total <= midDispatches {
		t.Fatalf("dispatches after turn = %d, want > mid-turn %d (step 2 + turn end)", total, midDispatches)
	}
}

// TestObserveStepUsage_SubAgentDoesNotDispatch verifies the main-indicator
// guard: a sub-agent's per-step usage updates its own LastUsage but never
// dispatches EventContextUsage, matching the turn-end dispatch restriction.
func TestObserveStepUsage_SubAgentDoesNotDispatch(t *testing.T) {
	lm := testutil.NewFakeLMWithResponses("ok")
	pool := NewPool()
	var mu sync.Mutex
	dispatched := false
	pool.SetContextUsageDispatcher(func(sdk.ContextUsage, *CompactionNotice, int) {
		mu.Lock()
		dispatched = true
		mu.Unlock()
	})
	a, err := pool.Spawn("main/kid", lm, SpawnOpts{ModelName: "fake-model", ContextWindow: 100_000})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	done := make(chan error, 1)
	a.SetOnDone(func(err error) { done <- err })
	// The fake derives input usage from the last user message's length, so the
	// prompt must be long enough to produce a non-zero token count.
	a.Submit(context.Background(), "record a sub-agent usage snapshot for the live context indicator test")
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("turn errored: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("turn did not finish")
	}
	if a.LastUsage().InputTokens <= 0 {
		t.Fatalf("sub-agent LastUsage = %+v, want usage recorded", a.LastUsage())
	}
	mu.Lock()
	defer mu.Unlock()
	if dispatched {
		t.Fatal("sub-agent turn dispatched EventContextUsage; only main may drive the indicator")
	}
}

// TestObserveStepUsage_Guard verifies the zero-usage and peak-retention rules
// directly: a step with no input side is ignored, and a smaller later step
// never shrinks the stored peak.
func TestObserveStepUsage_Guard(t *testing.T) {
	a := &Agent{id: MainAgentID}
	a.observeStepUsage(fantasy.Usage{}, nil, 0)
	if a.LastUsage() != (fantasy.Usage{}) {
		t.Fatalf("zero-usage step changed lastUsage: %+v", a.LastUsage())
	}
	big := fantasy.Usage{InputTokens: 50_000}
	a.observeStepUsage(big, nil, 0)
	if a.LastUsage() != big {
		t.Fatalf("big step not stored: %+v", a.LastUsage())
	}
	small := fantasy.Usage{InputTokens: 10_000}
	a.observeStepUsage(small, nil, 0)
	if a.LastUsage() != big {
		t.Fatalf("smaller step shrank the stored peak: %+v", a.LastUsage())
	}
}
