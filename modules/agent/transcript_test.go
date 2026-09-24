package agent

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// transcript_test.go covers the canonical transcript: ID stability, the
// append-only contract, and the search/render behavior the recall tool depends
// on. These are pure unit tests — no LLM, no pool.

import (
	"strings"
	"testing"
)

func TestTranscript_RecordMessage_AssignsStableSequentialIDs(t *testing.T) {
	tr := NewTranscript()
	ids := []string{
		tr.RecordMessage("user", "first"),
		tr.RecordMessage("assistant", "second"),
		tr.RecordMessage("user", "third"),
	}
	want := []string{"u1", "a2", "u3"}
	for i, w := range want {
		if ids[i] != w {
			t.Errorf("id[%d] = %q, want %q", i, ids[i], w)
		}
	}
	if got := tr.Len(); got != 3 {
		t.Errorf("Len() = %d, want 3", got)
	}
}

func TestTranscript_IDsAreUniqueAcrossKinds(t *testing.T) {
	tr := NewTranscript()
	seen := map[string]bool{}
	for _, id := range []string{
		tr.RecordMessage("user", "m"),
		tr.RecordToolCall("exec", `{"command":"ls"}`),
		tr.RecordToolResult("exec", "ok"),
		tr.RecordMessage("assistant", "done"),
	} {
		if id == "" {
			t.Fatal("empty id recorded")
		}
		if seen[id] {
			t.Errorf("duplicate id %q", id)
		}
		seen[id] = true
	}
}

// Empty messages are rejected, matching the history contract: the provider API
// rejects empty text blocks, so an empty message is not worth recording.
func TestTranscript_RecordMessage_RejectsEmpty(t *testing.T) {
	tr := NewTranscript()
	for _, s := range []string{"", "   ", "\n\t"} {
		if id := tr.RecordMessage("user", s); id != "" {
			t.Errorf("RecordMessage(%q) = %q, want empty id", s, id)
		}
	}
	if got := tr.Len(); got != 0 {
		t.Errorf("Len() = %d, want 0", got)
	}
}

// A tool call with no arguments is still a record: the call happened, and its
// lack of arguments is itself information.
func TestTranscript_RecordToolCall_KeepsEmptyInput(t *testing.T) {
	tr := NewTranscript()
	if id := tr.RecordToolCall("list_agents", ""); id == "" {
		t.Error("empty-input tool call was not recorded")
	}
	if got := tr.Len(); got != 1 {
		t.Errorf("Len() = %d, want 1", got)
	}
}

func TestTranscript_SnapshotIsChronologicalAndCopied(t *testing.T) {
	tr := NewTranscript()
	tr.RecordMessage("user", "one")
	tr.RecordToolResult("exec", "two")

	snap := tr.Snapshot()
	if len(snap) != 2 || snap[0].Content != "one" || snap[1].Content != "two" {
		t.Fatalf("snapshot = %+v, want [one two]", snap)
	}
	// Mutating the snapshot must not affect the transcript.
	snap[0].Content = "mutated"
	if tr.Snapshot()[0].Content != "one" {
		t.Error("snapshot aliases transcript storage")
	}
}

func TestTranscript_SearchByTextIsCaseInsensitiveSubstring(t *testing.T) {
	tr := NewTranscript()
	tr.RecordMessage("user", "run the MIGRATION now")
	tr.RecordMessage("assistant", "unrelated text")

	got, total := tr.Search(RecallQuery{Text: "migration"})
	if total != 1 || len(got) != 1 {
		t.Fatalf("total=%d len=%d, want 1/1", total, len(got))
	}
	if got[0].Content != "run the MIGRATION now" {
		t.Errorf("matched %q", got[0].Content)
	}
}

func TestTranscript_SearchByToolMatchesOnlyThatTool(t *testing.T) {
	tr := NewTranscript()
	tr.RecordToolCall("exec", "cmd-a")
	tr.RecordToolResult("exec", "out-a")
	tr.RecordToolCall("edit_file", "cmd-b")
	tr.RecordMessage("user", "not a tool entry")

	got, total := tr.Search(RecallQuery{Tool: "EXEC"})
	if total != 2 || len(got) != 2 {
		t.Fatalf("total=%d len=%d, want 2/2", total, len(got))
	}
	for _, e := range got {
		if e.ToolName != "exec" {
			t.Errorf("tool filter leaked entry %+v", e)
		}
	}
}

func TestTranscript_SearchByPath(t *testing.T) {
	tr := NewTranscript()
	tr.RecordToolCall("edit_file", `{"path":"/src/main.go"}`)
	tr.RecordToolCall("edit_file", `{"path":"/src/other.go"}`)

	got, total := tr.Search(RecallQuery{Path: "/src/main.go"})
	if total != 1 || len(got) != 1 {
		t.Fatalf("total=%d len=%d, want 1/1", total, len(got))
	}
}

func TestTranscript_SearchByRangeIsInclusive(t *testing.T) {
	tr := NewTranscript()
	tr.RecordMessage("user", "seq1")
	tr.RecordMessage("assistant", "seq2")
	tr.RecordMessage("user", "seq3")

	got, total := tr.Search(RecallQuery{From: 2, To: 3})
	if total != 2 || len(got) != 2 {
		t.Fatalf("total=%d len=%d, want 2/2", total, len(got))
	}
	if got[0].Seq != 2 || got[1].Seq != 3 {
		t.Errorf("seqs = %d,%d, want 2,3", got[0].Seq, got[1].Seq)
	}
}

// A filtered search with no text/tool/path still honors the range alone, which
// is how the model pulls a contiguous span it already knows about.
func TestTranscript_SearchRangeOnly(t *testing.T) {
	tr := NewTranscript()
	for i := 0; i < 5; i++ {
		tr.RecordMessage("user", "msg")
	}
	got, total := tr.Search(RecallQuery{From: 4})
	if total != 2 || len(got) != 2 {
		t.Fatalf("total=%d len=%d, want 2/2", total, len(got))
	}
}

// Limit caps the returned entries but total still reports every match, so the
// caller can tell the model its query was too broad.
func TestTranscript_SearchLimitKeepsTotal(t *testing.T) {
	tr := NewTranscript()
	for i := 0; i < 10; i++ {
		tr.RecordMessage("user", "needle")
	}
	got, total := tr.Search(RecallQuery{Text: "needle", Limit: 3})
	if total != 10 {
		t.Errorf("total = %d, want 10", total)
	}
	if len(got) != 3 {
		t.Errorf("len(got) = %d, want 3", len(got))
	}
}

func TestTranscript_SearchNoMatchReturnsNilAndZero(t *testing.T) {
	tr := NewTranscript()
	tr.RecordMessage("user", "hello")
	got, total := tr.Search(RecallQuery{Text: "absent"})
	if total != 0 || len(got) != 0 {
		t.Errorf("total=%d len=%d, want 0/0", total, len(got))
	}
}

func TestTranscript_NilTranscriptIsSafe(t *testing.T) {
	var tr *Transcript
	if tr.Len() != 0 {
		t.Error("nil Len() != 0")
	}
	if tr.Snapshot() != nil {
		t.Error("nil Snapshot() != nil")
	}
	if id := tr.RecordMessage("user", "x"); id != "" {
		t.Error("nil RecordMessage returned an id")
	}
	if got, total := tr.Search(RecallQuery{Text: "x"}); total != 0 || got != nil {
		t.Error("nil Search returned results")
	}
}

// ─── Rendering and budgeting ─────────────────────────────────────────────────

// The rendered result must fit the budget exactly, header and footer included,
// so recall cannot overflow the model's context window no matter how much text
// it matches.
func TestRenderRecall_RespectsTokenBudgetExactly(t *testing.T) {
	tr := NewTranscript()
	big := strings.Repeat("x", 5_000)
	for i := 0; i < 20; i++ {
		tr.RecordToolResult("exec", big)
	}
	entries, total := tr.Search(RecallQuery{Tool: "exec"})

	const budget int64 = 500
	out := RenderRecall(entries, total, budget)
	if int64(len(out)) > budget*4 {
		t.Errorf("rendered %d chars, budget allows %d", len(out), budget*4)
	}
}

func TestRenderRecall_IncludesSourcePointers(t *testing.T) {
	tr := NewTranscript()
	tr.RecordToolCall("exec", `{"command":"go test ./..."}`)
	tr.RecordToolResult("exec", "FAIL: connection refused on port 5433")
	entries, total := tr.Search(RecallQuery{Tool: "exec"})

	out := RenderRecall(entries, total, 4_000)
	for _, want := range []string{"[t1]", "[r2]", "tool_call exec", "tool_result exec", "port 5433"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

// The model must be told the material is the canonical pre-compaction record,
// otherwise it may treat recalled detail as contradicting its context.
func TestRenderRecall_StatesProvenance(t *testing.T) {
	tr := NewTranscript()
	tr.RecordMessage("user", "hello")
	entries, total := tr.Search(RecallQuery{Text: "hello"})
	out := RenderRecall(entries, total, 4_000)
	if !strings.Contains(out, "canonical session transcript") {
		t.Errorf("output does not state provenance:\n%s", out)
	}
	if !strings.Contains(out, "pre-compaction") {
		t.Errorf("output does not mention pre-compaction:\n%s", out)
	}
}

func TestRenderRecall_NoMatchesIsActionable(t *testing.T) {
	out := RenderRecall(nil, 0, 4_000)
	if !strings.Contains(out, "No transcript entries matched") {
		t.Errorf("no-match output not actionable:\n%s", out)
	}
	if int64(len(out)) > 4_000*4 {
		t.Error("no-match output exceeds budget")
	}
}

// A single entry larger than the whole budget must still be returned, truncated,
// rather than producing an empty result the model cannot act on.
func TestRenderRecall_TruncatesSingleOversizeEntry(t *testing.T) {
	tr := NewTranscript()
	tr.RecordToolResult("exec", strings.Repeat("y", 50_000))
	entries, total := tr.Search(RecallQuery{Tool: "exec"})

	out := RenderRecall(entries, total, 200)
	if !strings.Contains(out, "[r1]") {
		t.Errorf("oversize entry was dropped entirely:\n%s", out)
	}
	if int64(len(out)) > 200*4 {
		t.Errorf("rendered %d chars, budget allows %d", len(out), 200*4)
	}
}

func TestRenderRecall_SignalsTruncatedMatches(t *testing.T) {
	tr := NewTranscript()
	for i := 0; i < 50; i++ {
		tr.RecordMessage("user", strings.Repeat("z", 400))
	}
	entries, total := tr.Search(RecallQuery{Text: "z"})

	out := RenderRecall(entries, total, 300)
	if !strings.Contains(out, "more matched") {
		t.Errorf("truncation not signaled:\n%s", out)
	}
}

func TestRecallBudgetForWindow(t *testing.T) {
	cases := []struct {
		name   string
		window int64
		want   int64
	}{
		{"unresolved falls back to default", 0, DefaultRecallTokenBudget},
		{"negative falls back to default", -1, DefaultRecallTokenBudget},
		{"large window is capped", 1_000_000, DefaultRecallTokenBudget},
		{"small window scales down", 4_000, 500},
		{"tiny window hits the floor", 2_000, 500},
	}
	for _, tc := range cases {
		if got := recallBudgetForWindow(tc.window); got != tc.want {
			t.Errorf("%s: budget = %d, want %d", tc.name, got, tc.want)
		}
	}
}

// A budget of zero or less must not mean "unbounded".
func TestRenderRecall_NonPositiveBudgetUsesDefault(t *testing.T) {
	tr := NewTranscript()
	tr.RecordMessage("user", "hello")
	entries, total := tr.Search(RecallQuery{Text: "hello"})
	for _, budget := range []int64{0, -1} {
		out := RenderRecall(entries, total, budget)
		if int64(len(out)) > DefaultRecallTokenBudget*4 {
			t.Errorf("budget %d produced %d chars", budget, len(out))
		}
	}
}
