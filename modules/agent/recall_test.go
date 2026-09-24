package agent

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// recall_test.go covers the recall tool surface: schema, filter validation, and
// that its output is bounded by the agent's resolved context window.

import (
	"context"
	"strings"
	"testing"

	"charm.land/fantasy"
)

// recallAgent builds an agent with a context window already resolved, plus a
// transcript seeded with one distinctive tool failure. The window matters
// because the tool derives its output budget from it.
func recallAgent(t *testing.T, contextWindow int64) *Agent {
	t.Helper()
	a := &Agent{id: "recall-test", modelName: "recall-test"}
	a.contextWindow = contextWindow
	tr := a.CanonicalTranscript()
	tr.RecordMessage("user", "run the migration")
	tr.RecordToolCall("exec", `{"command":"go test ./..."}`)
	tr.RecordToolResult("exec", "FAIL: connection refused on port 5433")
	tr.RecordMessage("assistant", "the database was unreachable")
	return a
}

func runRecall(t *testing.T, a *Agent, input string) fantasy.ToolResponse {
	t.Helper()
	tool := a.RecallTool()
	resp, err := tool.Run(context.Background(), fantasy.ToolCall{ID: "c1", Name: RecallToolName, Input: input})
	if err != nil {
		t.Fatalf("Run returned a Go error: %v", err)
	}
	return resp
}

func TestRecallTool_InfoShape(t *testing.T) {
	tool := recallAgent(t, 200_000).RecallTool()
	info := tool.Info()
	if info.Name != RecallToolName {
		t.Errorf("Name = %q, want %q", info.Name, RecallToolName)
	}
	if info.Description == "" {
		t.Error("Description is empty")
	}
	// Every filter is optional, but at least one must be supplied; that rule is
	// enforced in Run, so Required stays empty and the schema stays permissive.
	if len(info.Required) != 0 {
		t.Errorf("Required = %v, want empty", info.Required)
	}
	for _, p := range []string{"query", "tool", "path", "from", "to", "limit"} {
		if _, ok := info.Parameters[p]; !ok {
			t.Errorf("parameter %q missing from schema", p)
		}
	}
}

func TestRecallTool_Run_FindsExactPreCompactionDetail(t *testing.T) {
	a := recallAgent(t, 200_000)
	resp := runRecall(t, a, `{"query":"port 5433"}`)
	if resp.IsError {
		t.Fatalf("unexpected error response: %s", resp.Content)
	}
	for _, want := range []string{"connection refused on port 5433", "[r3]", "tool_result exec"} {
		if !strings.Contains(resp.Content, want) {
			t.Errorf("result missing %q:\n%s", want, resp.Content)
		}
	}
}

func TestRecallTool_Run_FilterByTool(t *testing.T) {
	a := recallAgent(t, 200_000)
	resp := runRecall(t, a, `{"tool":"exec"}`)
	if resp.IsError {
		t.Fatalf("unexpected error response: %s", resp.Content)
	}
	if !strings.Contains(resp.Content, "tool_call exec") || !strings.Contains(resp.Content, "tool_result exec") {
		t.Errorf("tool filter did not return both call and result:\n%s", resp.Content)
	}
	if strings.Contains(resp.Content, "run the migration") {
		t.Errorf("tool filter leaked a message entry:\n%s", resp.Content)
	}
}

func TestRecallTool_Run_FilterByPath(t *testing.T) {
	a := recallAgent(t, 200_000)
	a.CanonicalTranscript().RecordToolCall("edit_file", `{"path":"/src/main.go"}`)
	resp := runRecall(t, a, `{"path":"/src/main.go"}`)
	if resp.IsError {
		t.Fatalf("unexpected error response: %s", resp.Content)
	}
	if !strings.Contains(resp.Content, "/src/main.go") {
		t.Errorf("path filter returned nothing relevant:\n%s", resp.Content)
	}
}

func TestRecallTool_Run_MessageRange(t *testing.T) {
	a := recallAgent(t, 200_000)
	// Seeds in order: u1 (user), t2 (tool call), r3 (tool result), a4 (assistant).
	resp := runRecall(t, a, `{"from":2,"to":4}`)
	if resp.IsError {
		t.Fatalf("unexpected error response: %s", resp.Content)
	}
	for _, want := range []string{"[t2]", "[r3]", "[a4]"} {
		if !strings.Contains(resp.Content, want) {
			t.Errorf("range result missing %q:\n%s", want, resp.Content)
		}
	}
	if strings.Contains(resp.Content, "[u1]") {
		t.Errorf("range result included an out-of-range entry:\n%s", resp.Content)
	}
}

// A recall with no filter at all would dump the session and waste the context
// it exists to protect, so it is refused with guidance.
func TestRecallTool_Run_RequiresAtLeastOneFilter(t *testing.T) {
	a := recallAgent(t, 200_000)
	for _, input := range []string{`{}`, `{"query":""}`, `{"query":"   "}`} {
		resp := runRecall(t, a, input)
		if !resp.IsError {
			t.Errorf("input %s: expected an error response", input)
		}
		if !strings.Contains(resp.Content, "at least one") {
			t.Errorf("input %s: error not actionable: %s", input, resp.Content)
		}
	}
}

func TestRecallTool_Run_RejectsMalformedInput(t *testing.T) {
	a := recallAgent(t, 200_000)
	resp := runRecall(t, a, `{"query":`)
	if !resp.IsError {
		t.Fatalf("expected an error response, got: %s", resp.Content)
	}
	if !strings.Contains(resp.Content, "invalid input JSON") {
		t.Errorf("error not actionable: %s", resp.Content)
	}
}

func TestRecallTool_Run_RejectsInvertedRange(t *testing.T) {
	a := recallAgent(t, 200_000)
	resp := runRecall(t, a, `{"from":9,"to":2}`)
	if !resp.IsError {
		t.Fatalf("expected an error response, got: %s", resp.Content)
	}
	if !strings.Contains(resp.Content, "must not exceed") {
		t.Errorf("error not actionable: %s", resp.Content)
	}
}

func TestRecallTool_Run_RejectsNegativeBounds(t *testing.T) {
	a := recallAgent(t, 200_000)
	resp := runRecall(t, a, `{"from":-1}`)
	if !resp.IsError {
		t.Fatalf("expected an error response, got: %s", resp.Content)
	}
}

// An empty transcript is a legitimate state (nothing recorded yet, or the query
// simply missed); it must be a readable result, not an error.
func TestRecallTool_Run_EmptyTranscriptIsNotAnError(t *testing.T) {
	a := &Agent{id: "empty", modelName: "empty"}
	a.contextWindow = 200_000
	resp := runRecall(t, a, `{"query":"anything"}`)
	if resp.IsError {
		t.Fatalf("empty transcript produced an error: %s", resp.Content)
	}
	if !strings.Contains(resp.Content, "No transcript entries matched") {
		t.Errorf("unexpected empty result: %s", resp.Content)
	}
}

// The tool's output is bounded by the agent's context window, so a small window
// yields a smaller recall than a large one — the guarantee that recall cannot
// crowd out the conversation.
func TestRecallTool_Run_BoundedByContextWindow(t *testing.T) {
	seed := func(a *Agent) {
		tr := a.CanonicalTranscript()
		for i := 0; i < 200; i++ {
			tr.RecordToolResult("exec", strings.Repeat("payload ", 100))
		}
	}

	small := &Agent{id: "small", modelName: "small"}
	small.contextWindow = 2_000
	seed(small)

	large := &Agent{id: "large", modelName: "large"}
	large.contextWindow = 1_000_000
	seed(large)

	smallOut := runRecall(t, small, `{"tool":"exec"}`)
	largeOut := runRecall(t, large, `{"tool":"exec"}`)

	smallBudget := recallBudgetForWindow(small.ContextWindow())
	if int64(len(smallOut.Content)) > smallBudget*4 {
		t.Errorf("small-window output %d chars exceeds budget %d", len(smallOut.Content), smallBudget*4)
	}
	largeBudget := recallBudgetForWindow(large.ContextWindow())
	if int64(len(largeOut.Content)) > largeBudget*4 {
		t.Errorf("large-window output %d chars exceeds budget %d", len(largeOut.Content), largeBudget*4)
	}
	if len(smallOut.Content) >= len(largeOut.Content) {
		t.Errorf(
			"small window (%d chars) should return less than large window (%d chars)",
			len(smallOut.Content),
			len(largeOut.Content),
		)
	}
}

// A tool built before the agent's transcript exists still works, because the
// transcript is created lazily and the tool reads it per call.
func TestRecallTool_ReadsTranscriptLazily(t *testing.T) {
	a := &Agent{id: "lazy", modelName: "lazy"}
	a.contextWindow = 200_000
	a.RecallTool() // built before anything is recorded

	if resp := runRecall(t, a, `{"query":"x"}`); !strings.Contains(resp.Content, "No transcript entries matched") {
		t.Fatalf("expected empty result, got: %s", resp.Content)
	}

	a.CanonicalTranscript().RecordMessage("user", "recorded later x")
	resp := runRecall(t, a, `{"query":"x"}`)
	if !strings.Contains(resp.Content, "recorded later x") {
		t.Errorf("tool did not see later transcript entries: %s", resp.Content)
	}
}

// A tool with no agent bound must report the problem rather than panicking.
func TestRecallTool_NilAgentIsReportedNotPanicked(t *testing.T) {
	tool := &recallTool{}
	resp, err := tool.Run(context.Background(), fantasy.ToolCall{Name: RecallToolName, Input: `{"query":"x"}`})
	if err != nil {
		t.Fatalf("Run returned a Go error: %v", err)
	}
	if !resp.IsError {
		t.Fatalf("expected an error response, got: %s", resp.Content)
	}
	if !strings.Contains(resp.Content, "no agent bound") {
		t.Errorf("error not actionable: %s", resp.Content)
	}
}
