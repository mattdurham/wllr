package agent

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"charm.land/fantasy"

	"github.com/mattdurham/wllr/modules/sdk"
	"github.com/mattdurham/wllr/modules/testutil"
)

// gateTool blocks inside Run until its channel is released, giving the test a
// deterministic window between step 1's request and step 2's prepare to queue
// a steer message mid-turn.
type gateTool struct {
	release chan struct{}
}

func (g *gateTool) Info() fantasy.ToolInfo {
	return fantasy.ToolInfo{Name: "gate", Description: "blocks", Parallel: false}
}

func (g *gateTool) Run(_ context.Context, _ fantasy.ToolCall) (fantasy.ToolResponse, error) {
	<-g.release
	return fantasy.NewTextResponse("released"), nil
}

func (g *gateTool) ProviderOptions() fantasy.ProviderOptions   { return nil }
func (g *gateTool) SetProviderOptions(fantasy.ProviderOptions) {}

// TestDrainSteer_PreservesOtherMessages verifies the selective drain: steer
// messages come out, everything else stays queued in order. A full drain at a
// step boundary would strand shutdown requests that finishTurn expects.
func TestDrainSteer_PreservesOtherMessages(t *testing.T) {
	a := &Agent{id: "t"}
	a.AppendInbox(sdk.Message{Role: sdk.RoleUser, Content: "normal one"})
	a.AppendInbox(sdk.Message{Role: sdk.RoleUser, Content: "steer me", Type: sdk.MessageTypeSteer})
	a.AppendInbox(sdk.Message{Role: sdk.RoleUser, Content: "normal two"})

	steered := a.inbox.drainSteer()
	if len(steered) != 1 || steered[0].Content != "steer me" {
		t.Fatalf("drainSteer = %v, want [steer me]", steered)
	}
	rest := a.SnapshotInbox()
	if len(rest) != 2 || rest[0].Content != "normal one" || rest[1].Content != "normal two" {
		t.Fatalf("inbox after drainSteer = %v, want the two normal messages in order", rest)
	}
	// Second drain is empty: delivery is exactly once.
	if again := a.inbox.drainSteer(); len(again) != 0 {
		t.Fatalf("second drainSteer = %v, want empty", again)
	}
}

// TestSteer_QueuePreservedThroughSubmitRaces verifies a steer message survives
// the Submit CAS-fail requeue: when it lands while a turn is running it must
// keep its type and wait for the injector, not be re-typed as normal.
func TestSteer_QueuePreservedThroughSubmitRaces(t *testing.T) {
	a := &Agent{id: "t"}
	// Simulate a running turn: the CAS fails and Submit re-queues everything.
	a.isRunning.Store(true)
	a.AppendInbox(sdk.Message{Role: sdk.RoleUser, Content: "steer me", Type: sdk.MessageTypeSteer})
	a.Submit(context.Background(), "hello")

	rest := a.SnapshotInbox()
	if len(rest) != 2 {
		t.Fatalf("inbox = %d messages, want 2 (steer + re-queued content)", len(rest))
	}
	foundSteer := false
	for _, m := range rest {
		if m.Type == sdk.MessageTypeSteer && m.Content == "steer me" {
			foundSteer = true
		}
	}
	if !foundSteer {
		t.Fatalf("steer message lost or re-typed: %v", rest)
	}
}

// TestSteer_DeliveredAtStepBoundary is the end-to-end regression: a steer
// message queued during a running turn is injected into the NEXT step's
// provider request, re-injected on every later step of the same turn, recorded
// in history between the turn's prompt and its response, mirrored to the
// canonical transcript, and reported through SetOnSteer.
func TestSteer_DeliveredAtStepBoundary(t *testing.T) {
	lm := testutil.NewFakeLM()
	lm.SetScript([]testutil.ScriptedTurn{
		{
			Text: "working on it",
			ToolCalls: []testutil.ScriptedToolCall{
				{ID: "tc1", Name: "gate", Input: json.RawMessage(`{}`)},
			},
		},
		{Text: "done"},
	})

	pool := NewPool()
	a, err := pool.Spawn("steer-test", lm, SpawnOpts{ModelName: "fake-model", ContextWindow: 100_000})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	gate := &gateTool{release: make(chan struct{})}
	a.SetToolsFn(func() []fantasy.AgentTool { return []fantasy.AgentTool{gate} })

	steered := make(chan string, 4)
	a.SetOnSteer(func(content string) { steered <- content })

	done := make(chan error, 1)
	a.SetOnDone(func(err error) { done <- err })

	a.Submit(context.Background(), "start the work")

	// Wait until the gate tool is executing (step 1 in flight), then steer.
	deadline := time.Now().Add(5 * time.Second)
	for a.Activity().ActiveToolName != "gate" {
		if time.Now().After(deadline) {
			t.Fatal("gate tool never started")
		}
		time.Sleep(5 * time.Millisecond)
	}
	a.AppendInbox(sdk.Message{Role: sdk.RoleUser, Content: "skip the slow part", Type: sdk.MessageTypeSteer})
	close(gate.release)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("turn errored: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("turn did not finish")
	}

	// The second provider request must carry the steer as its last user message.
	calls := lm.Calls()
	if len(calls) < 2 {
		t.Fatalf("got %d provider requests, want >= 2", len(calls))
	}
	last := calls[len(calls)-1]
	if len(last.Messages) == 0 || !strings.HasSuffix(last.Messages[len(last.Messages)-1], "user: skip the slow part") {
		t.Fatalf("steer not injected at the end of the second request: %v", last.Messages)
	}
	// The first request must NOT contain it (queued after step 1 started).
	first := calls[0]
	for _, m := range first.Messages {
		if strings.Contains(m, "skip the slow part") {
			t.Fatalf("steer leaked into the first request: %v", first.Messages)
		}
	}

	// History records it between the prompt and the assistant response.
	hist := a.History()
	steerIdx, promptIdx, assistantIdx := -1, -1, -1
	for i, m := range hist {
		switch {
		case m.Type == sdk.MessageTypeSteer:
			steerIdx = i
		case m.Role == sdk.RoleUser && m.Content == "start the work":
			promptIdx = i
		case m.Role == sdk.RoleAssistant && strings.Contains(m.Content, "done"):
			assistantIdx = i
		}
	}
	if steerIdx < 0 || promptIdx < 0 || assistantIdx < 0 {
		t.Fatalf("history incomplete: %+v", hist)
	}
	if promptIdx >= steerIdx || steerIdx >= assistantIdx {
		t.Fatalf("history ordering wrong: prompt=%d steer=%d assistant=%d", promptIdx, steerIdx, assistantIdx)
	}

	// Canonical transcript mirrors the delivery.
	foundCanonical := false
	for _, e := range a.CanonicalTranscript().Snapshot() {
		if e.Kind == TranscriptKindMessage && strings.Contains(e.Content, "skip the slow part") {
			foundCanonical = true
		}
	}
	if !foundCanonical {
		t.Fatal("steer not recorded in the canonical transcript")
	}

	// The render callback fired exactly once.
	select {
	case got := <-steered:
		if got != "skip the slow part" {
			t.Fatalf("onSteer = %q", got)
		}
	default:
		t.Fatal("onSteer never fired")
	}
	select {
	case extra := <-steered:
		t.Fatalf("onSteer fired twice: %q", extra)
	default:
	}
}

// TestSteer_IdleAgentConsumedAsMessage verifies the idle path: an agent with a
// queued steer and no running turn treats it as an ordinary model-visible
// message — the turn runs with it as the prompt-side user message and records
// it in history.
func TestSteer_IdleAgentConsumedAsMessage(t *testing.T) {
	lm := testutil.NewFakeLMWithResponses("steered reply")
	pool := NewPool()
	a, err := pool.Spawn("idle-steer", lm, SpawnOpts{ModelName: "fake-model", ContextWindow: 100_000})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	done := make(chan error, 1)
	a.SetOnDone(func(err error) { done <- err })

	a.AppendInbox(sdk.Message{Role: sdk.RoleUser, Content: "do it this way", Type: sdk.MessageTypeSteer})
	a.Submit(context.Background(), "")

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("turn errored: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("turn did not finish")
	}

	call := lm.LastCall()
	found := false
	for _, m := range call.Messages {
		if strings.Contains(m, "do it this way") {
			found = true
		}
	}
	if !found {
		t.Fatalf("idle steer not delivered to the LLM: %v", call.Messages)
	}
	for _, m := range a.History() {
		if m.Type == sdk.MessageTypeSteer && m.Content == "do it this way" {
			return
		}
	}
	t.Fatalf("idle steer not recorded in history: %+v", a.History())
}

// TestSteerInjector_ReinjectsOnEveryStep verifies the core fantasy constraint:
// each step's request is rebuilt from the fixed initial prompt, so the
// injector must re-append everything it has delivered on every subsequent
// prepare or the steer would vanish from step N+1's request.
func TestSteerInjector_ReinjectsOnEveryStep(t *testing.T) {
	a := &Agent{id: "t"}
	si := newSteerInjector(a)
	a.AppendInbox(sdk.Message{Role: sdk.RoleUser, Content: "guidance", Type: sdk.MessageTypeSteer})

	base := []fantasy.Message{
		{Role: fantasy.MessageRoleUser, Content: []fantasy.MessagePart{fantasy.TextPart{Text: "orig"}}},
	}
	wrapped := si.wrap(nil)

	// Step 0: injects.
	_, res0, err := wrapped(context.Background(), fantasy.PrepareStepFunctionOptions{Messages: base})
	if err != nil {
		t.Fatalf("step 0: %v", err)
	}
	if len(res0.Messages) != 2 {
		t.Fatalf("step 0 messages = %d, want 2 (orig + steer)", len(res0.Messages))
	}

	// Step 1: fantasy rebuilt the request WITHOUT the injected message (only
	// the original + accumulated step content); the injector must re-add it.
	_, res1, err := wrapped(context.Background(), fantasy.PrepareStepFunctionOptions{Messages: base})
	if err != nil {
		t.Fatalf("step 1: %v", err)
	}
	if len(res1.Messages) != 2 {
		t.Fatalf("step 1 messages = %d, want 2 (re-injected)", len(res1.Messages))
	}
	last := res1.Messages[len(res1.Messages)-1]
	tp, ok := last.Content[0].(fantasy.TextPart)
	if !ok || tp.Text != "guidance" {
		t.Fatalf("re-injected message = %+v, want guidance", last)
	}

	// The base slice must not be mutated (fantasy reuses its backing array).
	if len(base) != 1 {
		t.Fatalf("input messages mutated: %d entries, want 1", len(base))
	}
}
