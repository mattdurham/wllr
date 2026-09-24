package agent

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// canonical_test.go covers the central guarantee of issue #42: compaction
// rewrites the model-visible history, but the canonical transcript survives it,
// so the exact pre-compaction detail stays retrievable through recall.
//
// These tests drive real turns (rather than calling the helpers directly) because
// the invariant is a property of the turn path: executeTurn is where compaction
// replaces a.history, and it is where the canonical transcript is written. A
// unit test of compactHistory alone could not show that.

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

// stubTool is a minimal client-side tool whose output we control, so a scripted
// tool call produces a deterministic tool result to record.
type stubTool struct {
	name   string
	output string
}

func (s *stubTool) Info() fantasy.ToolInfo {
	return fantasy.ToolInfo{Name: s.name, Description: "stub", Parallel: true}
}

func (s *stubTool) Run(context.Context, fantasy.ToolCall) (fantasy.ToolResponse, error) {
	return fantasy.NewTextResponse(s.output), nil
}

func (s *stubTool) ProviderOptions() fantasy.ProviderOptions { return nil }

func (s *stubTool) SetProviderOptions(fantasy.ProviderOptions) {}

// runTurn submits one turn and blocks until it completes or the test times out.
func runTurn(t *testing.T, a *Agent, content string) {
	t.Helper()
	done := make(chan error, 1)
	a.SetOnDone(func(err error) {
		select {
		case done <- err:
		default:
		}
	})
	a.Submit(context.Background(), content)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("turn error: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for turn")
	}
}

// TestExecuteTurn_CompactionPreservesCanonicalTranscript is the acceptance test
// for "compaction does not destroy the canonical transcript" and "the model can
// retrieve exact historical details after compaction".
//
// It forces a real compaction, then asserts the same detail is gone from the
// model-visible history but still present — and still findable by recall — in
// the canonical transcript.
func TestExecuteTurn_CompactionPreservesCanonicalTranscript(t *testing.T) {
	const detail = "FAIL: connection refused on port 5433"

	pool := NewPool()
	pool.SetContextWindow(200_000)
	a, err := pool.Spawn(MainAgentID, &compactTestLM{response: "summary"}, SpawnOpts{ContextWindow: 200_000})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	// Seed the canonical transcript with the exact material a later turn would
	// need: a command, its input, and its failing output.
	tr := a.CanonicalTranscript()
	tr.RecordMessage("user", "run the migration test")
	tr.RecordToolCall("exec", `{"command":"go test ./..."}`)
	tr.RecordToolResult("exec", detail)
	recordedBefore := tr.Len()

	// Seed a history long enough to force compaction, with the same detail in an
	// old message that compaction will fold away.
	history := longHistory(212)
	history[10].Content = "earlier turn: exec go test -> " + detail
	a.historyMu.Lock()
	a.history = history
	a.historyMu.Unlock()
	a.setLastUsage(fantasy.Usage{InputTokens: 170_000})

	runTurn(t, a, "continue")

	if got := a.CompactionCount(); got != 1 {
		t.Fatalf("CompactionCount = %d, want 1 (compaction must actually have run)", got)
	}

	// Compaction shrank the model-visible history and removed the detail from it.
	post := a.History()
	if len(post) >= len(history) {
		t.Errorf("history was not compacted: %d messages, was %d", len(post), len(history))
	}
	for _, m := range post {
		if strings.Contains(m.Content, "port 5433") {
			t.Fatalf("model-visible history still holds the detail; compaction did not fold it away")
		}
	}

	// The canonical transcript is untouched, plus this turn's own two records.
	if got := tr.Len(); got != recordedBefore+2 {
		t.Errorf("canonical transcript has %d entries, want %d (unchanged + this turn)", got, recordedBefore+2)
	}

	// And the exact detail is still retrievable.
	resp, rerr := a.RecallTool().Run(
		context.Background(),
		fantasy.ToolCall{ID: "c1", Name: RecallToolName, Input: `{"query":"port 5433"}`},
	)
	if rerr != nil {
		t.Fatalf("recall returned a Go error: %v", rerr)
	}
	if resp.IsError {
		t.Fatalf("recall error response: %s", resp.Content)
	}
	for _, want := range []string{detail, "tool_result exec"} {
		if !strings.Contains(resp.Content, want) {
			t.Errorf("recall after compaction missing %q:\n%s", want, resp.Content)
		}
	}
}

// A compaction may not see the transcript's own entries as summarizable input:
// they are recorded beside history, not into it.
func TestExecuteTurn_CanonicalTranscriptDoesNotEnterHistory(t *testing.T) {
	a := &Agent{id: "leak-test"}
	a.SetModel(testutil.NewFakeLM(), "fake-model", 200_000)

	a.CanonicalTranscript().RecordMessage("user", "transcript-only-marker")
	runTurn(t, a, "hello")

	for _, m := range a.History() {
		if strings.Contains(m.Content, "transcript-only-marker") {
			t.Fatal("canonical transcript leaked into model-visible history")
		}
	}
}

// The turn path must record both sides of the conversation, so recall can find
// what the user asked and what the agent answered.
func TestExecuteTurn_RecordsMessagesInCanonicalTranscript(t *testing.T) {
	a := &Agent{id: "record-test"}
	a.SetModel(testutil.NewFakeLMWithResponses("the answer"), "fake-model", 200_000)

	runTurn(t, a, "the question")

	entries := a.CanonicalTranscript().Snapshot()
	if len(entries) != 2 {
		t.Fatalf("recorded %d entries, want 2: %+v", len(entries), entries)
	}
	if entries[0].Kind != TranscriptKindMessage || entries[0].Role != "user" || entries[0].Content != "the question" {
		t.Errorf("user entry = %+v", entries[0])
	}
	if entries[1].Kind != TranscriptKindMessage || entries[1].Role != "assistant" {
		t.Errorf("assistant entry = %+v", entries[1])
	}
}

// Tool results are the half the persisted session file never recorded, so this
// is the coverage for "command output" retrieval: a real client-side tool call
// must land its output in the canonical transcript.
func TestExecuteTurn_RecordsToolCallAndResultInCanonicalTranscript(t *testing.T) {
	const toolOutput = "FAIL: TestNeedsMigration — dial tcp :5433: connection refused"

	lm := testutil.NewFakeLM()
	lm.SetScript([]testutil.ScriptedTurn{
		{
			Text: "running the tests",
			ToolCalls: []testutil.ScriptedToolCall{
				{ID: "tc1", Name: "exec", Input: json.RawMessage(`{"command":"go test ./..."}`)},
			},
		},
		{Text: "the tests failed"},
	})

	a := &Agent{id: "tool-record-test"}
	a.SetModel(lm, "fake-model", 200_000)
	a.SetToolsFn(func() []fantasy.AgentTool {
		return []fantasy.AgentTool{&stubTool{name: "exec", output: toolOutput}}
	})

	runTurn(t, a, "run the tests")

	var calls, results []TranscriptEntry
	for _, e := range a.CanonicalTranscript().Snapshot() {
		switch e.Kind {
		case TranscriptKindToolCall:
			calls = append(calls, e)
		case TranscriptKindToolResult:
			results = append(results, e)
		}
	}
	if len(calls) != 1 {
		t.Fatalf("recorded %d tool calls, want 1", len(calls))
	}
	if !strings.Contains(calls[0].Content, "go test ./...") || calls[0].ToolName != "exec" {
		t.Errorf("tool call entry = %+v", calls[0])
	}
	if len(results) != 1 {
		t.Fatalf("recorded %d tool results, want 1", len(results))
	}
	if results[0].Content != toolOutput {
		t.Errorf("tool result content = %q, want %q", results[0].Content, toolOutput)
	}

	// The output must be findable by its own distinctive text.
	resp, err := a.RecallTool().Run(
		context.Background(),
		fantasy.ToolCall{ID: "c1", Name: RecallToolName, Input: `{"tool":"exec"}`},
	)
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	if !strings.Contains(resp.Content, "connection refused") {
		t.Errorf("recall did not return the tool output:\n%s", resp.Content)
	}
}

// Recall must see entries recorded before it was constructed, and the transcript
// must be usable after the agent exists in a zero-value form (as tests build it).
func TestCanonicalTranscript_SurvivesAcrossTurns(t *testing.T) {
	a := &Agent{id: "across-turns"}
	a.SetModel(testutil.NewFakeLMWithResponses("first answer", "second answer"), "fake-model", 200_000)

	runTurn(t, a, "first question")
	first := a.CanonicalTranscript().Len()
	runTurn(t, a, "second question")

	if got := a.CanonicalTranscript().Len(); got != first+2 {
		t.Errorf("transcript length = %d, want %d", got, first+2)
	}
	resp, err := a.RecallTool().Run(
		context.Background(),
		fantasy.ToolCall{ID: "c1", Name: RecallToolName, Input: `{"query":"first question"}`},
	)
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	if !strings.Contains(resp.Content, "first question") {
		t.Errorf("earlier turn not retrievable:\n%s", resp.Content)
	}
}

// The transcript records conversation, not Go-level control traffic: a system
// message delivered alongside real work must not become recallable content.
//
// The inbox deliberately mixes a system message with a normal one, because a
// batch of only control messages never reaches the recording code at all (the
// turn short-circuits to finishTurn). Mixing them is what puts the system
// message through the filter under test.
func TestCanonicalTranscript_ExcludesSystemMessages(t *testing.T) {
	a := &Agent{id: "system-filter"}
	a.SetModel(testutil.NewFakeLMWithResponses("ack"), "fake-model", 200_000)

	a.AppendInbox(sdk.Message{
		Role:    sdk.RoleUser,
		Type:    sdk.MessageTypeSystem,
		Content: `{"event":"steering","note":"system-internal-marker"}`,
	})
	a.AppendInbox(sdk.Message{
		Role:    sdk.RoleUser,
		Type:    sdk.MessageTypeProtocol,
		Content: "child is idle",
	})

	// Empty prompt so the drain path — the one that copies inbox messages into
	// the transcript — is the code under test.
	runTurn(t, a, "")

	entries := a.CanonicalTranscript().Snapshot()
	for _, e := range entries {
		if strings.Contains(e.Content, "system-internal-marker") {
			t.Errorf("system control message was recorded in the transcript: %+v", e)
		}
	}
	if len(entries) == 0 {
		t.Fatal("nothing was recorded; the filter test is vacuous")
	}
}
