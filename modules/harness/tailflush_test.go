package harness

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/fantasy"
	"github.com/mattdurham/wllr/modules/agent"
	"github.com/mattdurham/wllr/modules/testutil"
)

// captureSender records every tea.Msg sent, preserving order.
type captureSender struct {
	mu   sync.Mutex
	msgs []tea.Msg
}

func (c *captureSender) Send(msg tea.Msg) {
	c.mu.Lock()
	c.msgs = append(c.msgs, msg)
	c.mu.Unlock()
}

func (c *captureSender) snapshot() []tea.Msg {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]tea.Msg, len(c.msgs))
	copy(out, c.msgs)
	return out
}

func (c *captureSender) tokens() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	var sb strings.Builder
	for _, m := range c.msgs {
		if tm, ok := m.(TokenMsg); ok {
			sb.WriteString(tm.Token)
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
// reach the UI before the ToolCallStartMsg, so the narration reads complete
// before tool activity appears.
func TestToolCallForwarder_FlushesTailBeforeToolCall(t *testing.T) {
	sender := &captureSender{}
	onToken, flush := makeBatchedOnToken(sender, nil)
	onToken(" Waiting for ")
	onToken("the brainstormer")

	fwd := toolCallForwarder("main", sender, flush)
	fwd("call-1", "get_agent_status", `{"agent_id":"main/brainstormer"}`)

	msgs := sender.snapshot()
	tailIdx, callIdx := -1, -1
	for i, m := range msgs {
		if tm, ok := m.(TokenMsg); ok && tailIdx == -1 && tm.Token == "the brainstormer" {
			tailIdx = i
		}
		if tc, ok := m.(ToolCallStartMsg); ok && callIdx == -1 {
			callIdx = i
			if tc.AgentID != "main" || tc.ToolName != "get_agent_status" {
				t.Fatalf("ToolCallStartMsg = %+v, want agent=main tool=get_agent_status", tc)
			}
		}
	}
	if tailIdx == -1 {
		t.Fatal("batched token tail never delivered at tool-call dispatch")
	}
	if callIdx == -1 {
		t.Fatal("ToolCallStartMsg never sent")
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
	msgs := sender.snapshot()
	if len(msgs) != 1 {
		t.Fatalf("got %d messages, want 1 ToolCallStartMsg", len(msgs))
	}
	if tc, ok := msgs[0].(ToolCallStartMsg); !ok || tc.ToolName != "exec" {
		t.Fatalf("expected ToolCallStartMsg, got %T %+v", msgs[0], msgs[0])
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
// deltas followed by a tool call must deliver the COMPLETE text to the UI
// before the ToolCallStartMsg. Before the segment-boundary flush, the batcher
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
	m.wireMainAgentCallbacks(sender)

	pool.Get(agent.MainAgentID).Submit(context.Background(), "go")

	// Wait for the wired onDone to fire (StreamDoneMsg captured by sender).
	deadline := time.After(10 * time.Second)
	for {
		for _, msg := range sender.snapshot() {
			if _, ok := msg.(StreamDoneMsg); ok {
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
	// The complete response must have reached the UI, tail included.
	if got := sender.tokens(); got != response {
		t.Fatalf("tokens received = %q, want complete response %q", got, response)
	}
	// And the final token tail must precede the tool-call message.
	msgs := sender.snapshot()
	lastTokenIdx, callIdx := -1, -1
	for i, msg := range msgs {
		if _, ok := msg.(TokenMsg); ok {
			lastTokenIdx = i
		}
		if _, ok := msg.(ToolCallStartMsg); ok && callIdx == -1 {
			callIdx = i
		}
	}
	if lastTokenIdx == -1 || callIdx == -1 {
		t.Fatalf("expected tokens and ToolCallStartMsg; lastToken=%d call=%d", lastTokenIdx, callIdx)
	}
	if lastTokenIdx > callIdx {
		t.Fatalf("token tail (idx %d) arrived after ToolCallStartMsg (idx %d)", lastTokenIdx, callIdx)
	}
}
