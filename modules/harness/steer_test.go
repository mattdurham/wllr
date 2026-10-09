package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/fantasy"

	"github.com/mattdurham/wllr/modules/agent"
	"github.com/mattdurham/wllr/modules/extension"
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

// containsMsg reports whether the sender captured a message matching want.
func containsMsg(sender *captureSender, want func(tea.Msg) bool) bool {
	for _, m := range sender.snapshot() {
		if want(m) {
			return true
		}
	}
	return false
}

var streamDoneMsg = func(m tea.Msg) bool {
	_, ok := m.(StreamDoneMsg)
	return ok
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

// TestSteerDispatch_MidTurnSteerFlagged pins the before_agent_start flag shape
// the history extension filters on: the directly-sent prompt carries no flags,
// while mid-turn steer guidance carries queued+steer so it is recorded as a
// user prompt while other queued inbox traffic stays filtered out.
func TestSteerDispatch_MidTurnSteerFlagged(t *testing.T) {
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

	extHost := extension.NewHost(nil)
	defer func() { _ = extHost.Close(context.Background()) }()
	events := make(chan sdk.Event, 16)
	extHost.Bus.Subscribe(sdk.EventBeforeAgentStart, func(_ context.Context, evt sdk.Event) error {
		events <- evt
		return nil
	})

	// Wire first: wireMainAgentCallbacks overwrites SetOnDone and SetToolsFn,
	// so the gate tool must be installed after it.
	m := New(pool, "main", nil)
	m.extHost = extHost
	sender := &captureSender{}
	m.wireMainAgentCallbacks(sender)
	a.SetToolsFn(func() []fantasy.AgentTool { return []fantasy.AgentTool{gate} })

	a.Submit(context.Background(), "start work")
	waitFor(t, func() bool { return a.Activity().ActiveToolName == "gate" }, "gate tool to start")
	m.submitSteer("mind the timeout")
	close(gate.release)
	waitFor(t, func() bool { return containsMsg(sender, streamDoneMsg) }, "turn completion")

	var steerFlagged, plainPrompt bool
	deadline := time.After(5 * time.Second)
	for !steerFlagged || !plainPrompt {
		select {
		case evt := <-events:
			var p sdk.BeforeAgentStartPayload
			if json.Unmarshal(evt.Payload, &p) != nil {
				t.Fatalf("bad before_agent_start payload: %s", evt.Payload)
			}
			if p.Steer {
				if p.Prompt != "mind the timeout" || !p.Queued {
					t.Fatalf("steer event = %+v, want the steer text with queued+steer", p)
				}
				steerFlagged = true
			} else {
				if p.Prompt != "start work" || p.Queued {
					t.Fatalf("prompt event = %+v, want the direct prompt without flags", p)
				}
				plainPrompt = true
			}
		case <-deadline:
			t.Fatalf("missing before_agent_start events: steer=%v plain=%v", steerFlagged, plainPrompt)
		}
	}
}

// TestSteerDispatch_IdleWakeSteerFlagged pins the idle path: a steer delivered
// to an idle agent wakes a turn that consumes it through the queued-inbox
// dispatch loop, so the event carries queued=true plus the steer flag — it
// must remain distinguishable from an unflagged queued team message.
func TestSteerDispatch_IdleWakeSteerFlagged(t *testing.T) {
	pool := agent.NewPool()
	lm := testutil.NewFakeLMWithResponses("done")
	_, err := pool.Spawn("main", lm, agent.SpawnOpts{ModelName: "fake-model", ContextWindow: 100_000})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	extHost := extension.NewHost(nil)
	defer func() { _ = extHost.Close(context.Background()) }()
	events := make(chan sdk.Event, 16)
	extHost.Bus.Subscribe(sdk.EventBeforeAgentStart, func(_ context.Context, evt sdk.Event) error {
		events <- evt
		return nil
	})

	m := New(pool, "main", nil)
	m.extHost = extHost
	sender := &captureSender{}
	m.wireMainAgentCallbacks(sender)

	m.submitSteer("do it this way")
	waitFor(t, func() bool { return containsMsg(sender, streamDoneMsg) }, "woken turn completion")

	select {
	case evt := <-events:
		var p sdk.BeforeAgentStartPayload
		if json.Unmarshal(evt.Payload, &p) != nil {
			t.Fatalf("bad before_agent_start payload: %s", evt.Payload)
		}
		if !p.Queued || !p.Steer || p.Prompt != "do it this way" {
			t.Fatalf("idle wake steer event = %+v, want the steer text with queued+steer", p)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no before_agent_start event for the idle-wake steer")
	}
}
