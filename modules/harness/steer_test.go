package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"charm.land/fantasy"

	"github.com/mattdurham/wllr/modules/agent"
	"github.com/mattdurham/wllr/modules/sdk"
	"github.com/mattdurham/wllr/modules/testutil"
)

// steerGateTool blocks inside Run so the test holds an agent mid-turn while
// submitting steer guidance against it.
type steerGateTool struct {
	release chan struct{}
}

func (g *steerGateTool) Info() fantasy.ToolInfo {
	return fantasy.ToolInfo{Name: "gate", Description: "blocks", Parallel: false}
}

func (g *steerGateTool) Run(_ context.Context, _ fantasy.ToolCall) (fantasy.ToolResponse, error) {
	<-g.release
	return fantasy.NewTextResponse("released"), nil
}

func (g *steerGateTool) ProviderOptions() fantasy.ProviderOptions   { return nil }
func (g *steerGateTool) SetProviderOptions(fantasy.ProviderOptions) {}

// TestSteerCommand_RegisteredAndParsesArgs verifies /steer is a builtin and
// that its handler carries the joined argument text to the update loop.
func TestSteerCommand_RegisteredAndParsesArgs(t *testing.T) {
	r := NewRegistry()
	registerBuiltins(r)

	cmd, ok := r.Get("steer")
	if !ok {
		t.Fatal("/steer is not registered as a builtin command")
	}
	if !cmd.Instant {
		t.Fatal("/steer must be Instant (no WASM dispatch round-trip)")
	}
	steer, ok := cmd.Handler([]string{"skip", "the", "slow", "part"})().(steerSubmitMsg)
	if !ok {
		t.Fatalf("handler returned %T, want steerSubmitMsg", cmd.Handler(nil))
	}
	if steer.Content != "skip the slow part" {
		t.Fatalf("content = %q, want the joined args", steer.Content)
	}
	// Empty text yields a usage notification, not a submit.
	if _, ok := cmd.Handler(nil)().(NotifyMsg); !ok {
		t.Fatalf("empty args returned %T, want NotifyMsg usage hint", cmd.Handler(nil)())
	}
}

// TestSubmitSteer_RunningAgentQueuesSteer verifies the running path: the steer
// is queued in the agent's inbox (visible in the queued pane) without
// disturbing the running turn, and the running turn's provider request never
// sees it — the next turn does.
func TestSubmitSteer_RunningAgentQueuesSteer(t *testing.T) {
	lm := testutil.NewFakeLM()
	lm.SetScript([]testutil.ScriptedTurn{
		{
			Text: "working",
			ToolCalls: []testutil.ScriptedToolCall{
				{ID: "tc1", Name: "gate", Input: json.RawMessage(`{}`)},
			},
		},
		{Text: "done"},
	})
	pool := agent.NewPool()
	a, err := pool.Spawn("main", lm, agent.SpawnOpts{ModelName: "fake-model", ContextWindow: 100_000})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	gate := &steerGateTool{release: make(chan struct{})}
	a.SetToolsFn(func() []fantasy.AgentTool { return []fantasy.AgentTool{gate} })
	done := make(chan error, 1)
	a.SetOnDone(func(err error) { done <- err })

	a.Submit(context.Background(), "start work")
	waitFor(t, func() bool { return a.Activity().ActiveToolName == "gate" }, "gate tool to start")

	m := New(pool, "main", nil)
	m.submitSteer("mind the timeout")

	// Queued, steer-typed, and the running request was untouched.
	queued := a.SnapshotInbox()
	if len(queued) != 1 || queued[0].Type != sdk.MessageTypeSteer || queued[0].Content != "mind the timeout" {
		t.Fatalf("queued message = %+v, want steer-typed content", queued)
	}
	close(gate.release)
	<-done

	for _, msg := range lm.Calls()[0].Messages {
		if strings.Contains(msg, "mind the timeout") {
			t.Fatalf("steer leaked into the running turn's request: %v", lm.Calls()[0].Messages)
		}
	}
}

// TestSubmitSteer_IdleAgentWakesTurn verifies the idle path: Deliver(wake=true)
// starts a turn that consumes the steer as its message, so guidance typed into
// an idle agent is not stranded in the inbox.
func TestSubmitSteer_IdleAgentWakesTurn(t *testing.T) {
	pool := agent.NewPool()
	lm := testutil.NewFakeLMWithResponses("done")
	a, err := pool.Spawn("main", lm, agent.SpawnOpts{ModelName: "fake-model", ContextWindow: 100_000})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	m := New(pool, "main", nil)

	done := make(chan error, 1)
	a.SetOnDone(func(err error) { done <- err })

	m.submitSteer("do it this way")

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("steered turn errored: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("steered turn did not finish")
	}
	found := false
	for _, msg := range lm.LastCall().Messages {
		if strings.Contains(msg, "do it this way") {
			found = true
		}
	}
	if !found {
		t.Fatalf("steer never reached the provider: %v", lm.LastCall().Messages)
	}
	if n := a.InboxLen(); n != 0 {
		t.Fatalf("inbox = %d after the turn, want 0", n)
	}
}

// waitFor polls cond until true or the deadline, failing the test with label.
func waitFor(t *testing.T, cond func() bool, label string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", label)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
