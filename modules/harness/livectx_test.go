package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/mattdurham/wllr/modules/agent"
	"github.com/mattdurham/wllr/modules/testutil"
)

// TestRefreshContextStatus_WritesLiveSegments verifies the statusline's ctx
// segments are derivable from the pool whenever the main agent's window is
// known — including before any turn ends. This is the display half of the
// missing-ctx fix: the value comes from per-step usage recording
// (agent.observeStepUsage), and the 100ms tick paints it.
func TestRefreshContextStatus_WritesLiveSegments(t *testing.T) {
	lm := testutil.NewFakeLMWithResponses("ok")
	pool := agent.NewPool()
	a, err := pool.Spawn(agent.MainAgentID, lm, agent.SpawnOpts{ModelName: "fake-model", ContextWindow: 250_000})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	done := make(chan error, 1)
	a.SetOnDone(func(err error) { done <- err })
	a.Submit(context.Background(), "hi there, please record a usage snapshot long enough for tokens")
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("turn errored: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("turn did not finish")
	}

	m := New(pool, agent.MainAgentID, nil)
	if !m.refreshContextStatus() {
		t.Fatal("refreshContextStatus returned false despite a known window")
	}
	cu := pool.MainAgentContextUsage()
	want := fmt.Sprintf("%d/%d", cu.InputTokens, cu.ContextWindow)
	if got := m.live.getStatus("ctx"); got != want {
		t.Fatalf("ctx status = %q, want %q", got, want)
	}
	if got := m.live.getStatus("ctx rem"); got != "250000" {
		t.Fatalf("ctx rem status = %q, want 250000", got)
	}
	if cu.InputTokens <= 0 {
		t.Fatalf("pool usage = %+v, want non-zero input after a completed turn", cu)
	}
}

// TestRefreshContextStatus_UnknownWindowReturnsFalse verifies the clear-path
// contract: with no resolvable window the helper writes nothing and reports
// false, so StreamDoneMsg's clear branch fires instead.
func TestRefreshContextStatus_UnknownWindowReturnsFalse(t *testing.T) {
	pool := agent.NewPool()
	// Spawn without a context window: an unknown model leaves the window
	// unresolved, which is the hide-segment condition.
	if _, err := pool.Spawn(agent.MainAgentID, testutil.NewFakeLMWithResponses("ok"), agent.SpawnOpts{ModelName: "never-heard-of-it"}); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	m := New(pool, agent.MainAgentID, nil)
	m.live.setStatus("ctx", "500/1000") // stale value from a previous state
	if m.refreshContextStatus() {
		t.Fatal("refreshContextStatus returned true with an unknown window")
	}
	if got := m.live.getStatus("ctx"); got != "500/1000" {
		t.Fatalf("helper must not write when the window is unknown; ctx = %q", got)
	}
}

// TestStreamTick_PaintsContextStatus verifies the 100ms tick wiring: the tick
// handler refreshes the ctx segments from the pool, so a long-running turn
// paints ctx as its steps complete — the regression path for the session
// where ctx never appeared because the orchestrator turn lasted hours.
func TestStreamTick_PaintsContextStatus(t *testing.T) {
	lm := testutil.NewFakeLMWithResponses("ok")
	pool := agent.NewPool()
	a, err := pool.Spawn(agent.MainAgentID, lm, agent.SpawnOpts{ModelName: "fake-model", ContextWindow: 100_000})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	done := make(chan error, 1)
	a.SetOnDone(func(err error) { done <- err })
	a.Submit(context.Background(), "tick paints ctx from the usage recorded while this turn was running along")
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("turn errored: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("turn did not finish")
	}

	m := New(pool, agent.MainAgentID, nil)
	m.streaming = true // tick re-arms and paints while a turn runs
	m2, _, handled := m.updateStream(streamTickMsg{})
	if !handled {
		t.Fatal("streamTickMsg not handled")
	}
	if got := m2.live.getStatus("ctx"); got == "" {
		t.Fatal("tick did not paint ctx; segment would stay hidden during long turns")
	}
}
