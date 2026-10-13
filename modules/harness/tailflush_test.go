package harness

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"charm.land/fantasy"
	"github.com/mattdurham/wllr/modules/agent"
	"github.com/mattdurham/wllr/modules/sdk"
	"github.com/mattdurham/wllr/modules/testutil"
)

// captureEvent is one recorded front-end effect.
type captureEvent struct {
	kind    string // "token", "tool", or "done"
	text    string
	agentID string
	tool    string
}

// captureSender records the effects a front-end sink receives, preserving
// order. It embeds sdk.FuncSink so the many methods it does not care about are
// no-ops and it satisfies sdk.UISink for the wiring under test.
type captureSender struct {
	sdk.FuncSink
	mu     sync.Mutex
	events []captureEvent
}

func (c *captureSender) Token(text string) {
	c.mu.Lock()
	c.events = append(c.events, captureEvent{kind: "token", text: text})
	c.mu.Unlock()
}

func (c *captureSender) ToolCallStart(agentID, id, toolName, input string) {
	c.mu.Lock()
	c.events = append(c.events, captureEvent{kind: "tool", agentID: agentID, tool: toolName})
	c.mu.Unlock()
}

func (c *captureSender) StreamDone(err error) {
	c.mu.Lock()
	c.events = append(c.events, captureEvent{kind: "done"})
	c.mu.Unlock()
}

func (c *captureSender) snapshot() []captureEvent {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]captureEvent, len(c.events))
	copy(out, c.events)
	return out
}

func (c *captureSender) tokens() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	var sb strings.Builder
	for _, e := range c.events {
		if e.kind == "token" {
			sb.WriteString(e.text)
		}
	}
	return sb.String()
}

// TestTokenBatcher_HoldsTailUntilFlush reproduces the "text cut off
// mid-sentence" bug: the batcher only sends when a token arrives ≥75ms after
// the previous send, so a fast burst after a send holds its tail until the
// NEXT token or an explicit flush. Without a flush at segment boundaries, the
// final narration before a long tool call rendered incomplete for minutes.
func TestTokenBatcher_HoldsTailUntilFlush(t *testing.T) {
	sender := &captureSender{}
	onToken, flush := makeBatchedOnToken(sender, nil)

	// First token sends immediately (lastSend is zero).
	onToken(" Waiting for ")
	// Second token arrives within the 75ms window — must be held.
	onToken("the brainstormer")

	if got := sender.tokens(); got != " Waiting for " {
		t.Fatalf("pre-flush tokens = %q, want only the first batch %q", got, " Waiting for ")
	}

	flush()

	if got := sender.tokens(); got != " Waiting for the brainstormer" {
		t.Fatalf("post-flush tokens = %q, want %q", got, " Waiting for the brainstormer")
	}
}

// TestTokenBatcher_FlushIdempotent verifies repeated flushes stay no-ops
// (tool-call flush + onDone flush on the same turn).
func TestTokenBatcher_FlushIdempotent(t *testing.T) {
	sender := &captureSender{}
	onToken, flush := makeBatchedOnToken(sender, nil)

	onToken("abc")
	flush()
	flush()
	flush()

	if got := sender.tokens(); got != "abc" {
		t.Fatalf("tokens after repeated flushes = %q, want exactly one delivery %q", got, "abc")
	}
}

// TestToolCallForwarder_FlushesTailBeforeToolCall pins the segment-boundary
// contract: when a tool call is dispatched, the still-buffered token tail must
// reach the front end before the tool call, so the narration reads complete
// before tool activity appears.
func TestToolCallForwarder_FlushesTailBeforeToolCall(t *testing.T) {
	sender := &captureSender{}
	onToken, flush := makeBatchedOnToken(sender, nil)
	onToken(" Waiting for ")
	onToken("the brainstormer")

	fwd := toolCallForwarder("main", sender, flush)
	fwd("call-1", "get_agent_status", `{"agent_id":"main/brainstormer"}`)

	events := sender.snapshot()
	tailIdx, callIdx := -1, -1
	for i, e := range events {
		if e.kind == "token" && tailIdx == -1 && e.text == "the brainstormer" {
			tailIdx = i
		}
		if e.kind == "tool" && callIdx == -1 {
			callIdx = i
			if e.agentID != "main" || e.tool != "get_agent_status" {
				t.Fatalf("tool call = %+v, want agent=main tool=get_agent_status", e)
			}
		}
	}
	if tailIdx == -1 {
		t.Fatal("batched token tail never delivered at tool-call dispatch")
	}
	if callIdx == -1 {
		t.Fatal("tool call never delivered")
	}
	if tailIdx > callIdx {
		t.Fatalf("tail (idx %d) delivered after tool call (idx %d); text would render incomplete", tailIdx, callIdx)
	}
}

// TestToolCallForwarder_NilFlushSafe verifies the forwarder tolerates a nil
// flush (wiring without a batcher).
func TestToolCallForwarder_NilFlushSafe(t *testing.T) {
	sender := &captureSender{}
	fwd := toolCallForwarder("main", sender, nil)
	fwd("call-1", "exec", `{}`)
	events := sender.snapshot()
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1 tool call", len(events))
	}
	if events[0].kind != "tool" || events[0].tool != "exec" {
		t.Fatalf("expected a tool call for exec, got %+v", events[0])
	}
}

// noopTool is a minimal fantasy.AgentTool for scripted tool-call turns.
type noopTool struct{}

func (s *noopTool) Info() fantasy.ToolInfo {
	return fantasy.ToolInfo{Name: "noop", Description: "does nothing", Parallel: true}
}

func (s *noopTool) Run(context.Context, fantasy.ToolCall) (fantasy.ToolResponse, error) {
	return fantasy.NewTextResponse("ok"), nil
}

func (s *noopTool) ProviderOptions() fantasy.ProviderOptions   { return nil }
func (s *noopTool) SetProviderOptions(fantasy.ProviderOptions) {}

// TestWireMainAgentCallbacks_TurnFlushesTailAtToolCall is the end-to-end
// regression for text cut off mid-sentence: a scripted response of fast word
// deltas followed by a tool call must deliver the COMPLETE text to the front
// end before the tool call. Before the segment-boundary flush, the batcher
// held the last <75ms of tokens ("the brainstormer") until turn end, so long
// orchestrator turns rendered narration frozen mid-sentence for minutes.
func TestWireMainAgentCallbacks_TurnFlushesTailAtToolCall(t *testing.T) {
	const response = " Waiting for the brainstormer"
	prov := testutil.NewFakeProvider()
	prov.LM().SetScript([]testutil.ScriptedTurn{{
		Text: response,
		ToolCalls: []testutil.ScriptedToolCall{{
			ID: "tc1", Name: "noop", Input: json.RawMessage(`{}`),
		}},
	}})
	pool := agent.NewPool()
	pool.SetProvider(prov)
	pool.SetDefaultModelName("fake-model")
	pool.SetModelContextWindow("fake-model", 100000)

	lm, err := pool.LanguageModelForModel(context.Background(), "fake-model")
	if err != nil {
		t.Fatalf("LanguageModelForModel: %v", err)
	}
	if _, err := pool.Spawn(agent.MainAgentID, lm, agent.SpawnOpts{
		ModelName:     "fake-model",
		ContextWindow: 100000,
		Tools:         []fantasy.AgentTool{&noopTool{}},
	}); err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	m := New(pool, agent.MainAgentID, nil)
	sender := &captureSender{}
	m.WireMainAgentCallbacks(sender)

	pool.Get(agent.MainAgentID).Submit(context.Background(), "go")

	// Wait for the wired onDone to fire (the "done" effect captured by sender).
	deadline := time.After(10 * time.Second)
	for {
		for _, e := range sender.snapshot() {
			if e.kind == "done" {
				goto done
			}
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for turn completion")
		case <-time.After(10 * time.Millisecond):
		}
	}
done:
	// The complete response must have reached the front end, tail included.
	if got := sender.tokens(); got != response {
		t.Fatalf("tokens received = %q, want complete response %q", got, response)
	}
	// And the final token tail must precede the tool call.
	events := sender.snapshot()
	lastTokenIdx, callIdx := -1, -1
	for i, e := range events {
		if e.kind == "token" {
			lastTokenIdx = i
		}
		if e.kind == "tool" && callIdx == -1 {
			callIdx = i
		}
	}
	if lastTokenIdx == -1 || callIdx == -1 {
		t.Fatalf("expected tokens and a tool call; lastToken=%d call=%d", lastTokenIdx, callIdx)
	}
	if lastTokenIdx > callIdx {
		t.Fatalf("token tail (idx %d) arrived after the tool call (idx %d)", lastTokenIdx, callIdx)
	}
}
