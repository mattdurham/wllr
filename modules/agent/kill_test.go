package agent

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"charm.land/fantasy"

	"github.com/mattdurham/wllr/modules/testutil"
)

// killGateTool blocks inside Run but honors context cancellation — like a
// real tool (shell, wait loops) must. When the pool's hard-kill cancels the
// turn context, Run returns immediately with the cancellation error instead
// of blocking until its release channel closes.
type killGateTool struct {
	release chan struct{}
}

func (g *killGateTool) Info() fantasy.ToolInfo {
	return fantasy.ToolInfo{Name: "gate", Description: "blocks until released or cancelled", Parallel: false}
}

func (g *killGateTool) Run(ctx context.Context, _ fantasy.ToolCall) (fantasy.ToolResponse, error) {
	select {
	case <-g.release:
		return fantasy.NewTextResponse("released"), nil
	case <-ctx.Done():
		return fantasy.ToolResponse{}, ctx.Err()
	}
}

func (g *killGateTool) ProviderOptions() fantasy.ProviderOptions   { return nil }
func (g *killGateTool) SetProviderOptions(fantasy.ProviderOptions) {}

// TestKill_RunningAgentInterruptsTurnImmediately is the hard-kill contract:
// pool.Close on a mid-turn agent cancels its context so the in-flight tool
// call returns without being released, the turn ends, and the agent disappears
// from the pool at once — no graceful drain.
func TestKill_RunningAgentInterruptsTurnImmediately(t *testing.T) {
	lm := testutil.NewFakeLM()
	lm.SetScript([]testutil.ScriptedTurn{
		{
			Text: "working",
			ToolCalls: []testutil.ScriptedToolCall{
				{ID: "tc1", Name: "gate", Input: json.RawMessage(`{}`)},
			},
		},
		{Text: "unreachable"},
	})

	pool := NewPool()
	a, err := pool.Spawn("victim", lm, SpawnOpts{ModelName: "fake-model", ContextWindow: 100_000})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	gate := &killGateTool{release: make(chan struct{})}
	a.SetToolsFn(func() []fantasy.AgentTool { return []fantasy.AgentTool{gate} })
	defer close(gate.release) // safety: never leak a blocked goroutine

	done := make(chan error, 1)
	a.SetOnDone(func(err error) { done <- err })

	a.Submit(context.Background(), "start the work")

	// Wait until the gate tool is executing, then hard-kill the agent.
	deadline := time.Now().Add(5 * time.Second)
	for a.Activity().ActiveToolName != "gate" {
		if time.Now().After(deadline) {
			t.Fatal("gate tool never started")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err := pool.Close("victim"); err != nil {
		t.Fatalf("Close: %v", err)
	}

	select {
	case err := <-done:
		// The turn must end because the context was cancelled — the error is
		// the cancellation, not a normal completion.
		if err == nil {
			t.Fatal("killed turn completed normally; want a cancellation error")
		}
		t.Logf("kill ended the turn with: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("killed agent's turn did not end within 5s")
	}

	if pool.Get("victim") != nil {
		t.Error("killed agent still present in the pool")
	}
	if err := pool.Close("victim"); !errors.Is(err, ErrAgentNotFound) {
		t.Fatalf("second Close err = %v, want ErrAgentNotFound", err)
	}
}

// TestKill_CascadesSubtree verifies a kill removes the agent's descendants
// (they only exist to serve it) and leaves siblings and the root untouched.
func TestKill_CascadesSubtree(t *testing.T) {
	pool := NewPool()
	pool.SetModelContextWindow("fake-model", 100_000)
	for _, id := range []string{"main", "main/a", "main/a/b", "main/z"} {
		if _, err := pool.Spawn(id, testutil.NewFakeLMWithResponses("ok"), SpawnOpts{
			ModelName: "fake-model", ContextWindow: 100_000,
		}); err != nil {
			t.Fatalf("spawn %s: %v", id, err)
		}
	}
	if err := pool.Close("main/a"); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if pool.Get("main/a") != nil || pool.Get("main/a/b") != nil {
		t.Error("kill must remove the agent and its subtree")
	}
	if pool.Get("main") == nil || pool.Get("main/z") == nil {
		t.Error("kill must not touch the root or siblings")
	}
}

// TestKill_SubtreeRunningAgentsCancelled verifies a kill also cancels the
// in-flight turns of descendants, not just the targeted agent.
func TestKill_SubtreeRunningAgentsCancelled(t *testing.T) {
	lm := testutil.NewFakeLM()
	lm.SetScript([]testutil.ScriptedTurn{
		{
			Text: "child working",
			ToolCalls: []testutil.ScriptedToolCall{
				{ID: "tc-child", Name: "gate", Input: json.RawMessage(`{}`)},
			},
		},
		{Text: "unreachable"},
	})

	pool := NewPool()
	if _, err := pool.Spawn("main/worker", testutil.NewFakeLMWithResponses("parent ok"), SpawnOpts{ModelName: "fake-model", ContextWindow: 100_000}); err != nil {
		t.Fatalf("spawn parent: %v", err)
	}
	kid, err := pool.Spawn("main/worker/kid", lm, SpawnOpts{ModelName: "fake-model", ContextWindow: 100_000})
	if err != nil {
		t.Fatalf("spawn kid: %v", err)
	}
	gate := &killGateTool{release: make(chan struct{})}
	kid.SetToolsFn(func() []fantasy.AgentTool { return []fantasy.AgentTool{gate} })
	defer close(gate.release)

	kidDone := make(chan error, 1)
	kid.SetOnDone(func(err error) { kidDone <- err })
	kid.Submit(context.Background(), "child work")

	deadline := time.Now().Add(5 * time.Second)
	for kid.Activity().ActiveToolName != "gate" {
		if time.Now().After(deadline) {
			t.Fatal("gate tool never started")
		}
		time.Sleep(5 * time.Millisecond)
	}

	// Kill the parent: the kid's running turn must be cancelled too.
	if err := pool.Close("main/worker"); err != nil {
		t.Fatalf("Close parent: %v", err)
	}

	select {
	case err := <-kidDone:
		if err == nil {
			t.Fatal("descendant's turn completed normally after its parent was killed")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("descendant's turn did not end after parent kill")
	}
	if pool.Get("main/worker") != nil || pool.Get("main/worker/kid") != nil {
		t.Error("kill must remove the whole subtree from the pool")
	}
}
