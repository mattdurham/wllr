package agent

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// compactionpipeline_test.go covers the adaptive pipeline layered above
// compactHistory: the recovery-band budget, the progress guard, budgeted
// summarizer input, and deterministic truncation when summarization cannot
// reduce the context.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"charm.land/fantasy"

	"github.com/mattdurham/wllr/modules/sdk"
)

// failingLM delegates every LanguageModel method to compactTestLM except
// Stream, which always fails.
type failingLM struct {
	*compactTestLM
	err error
}

var _ fantasy.LanguageModel = (*failingLM)(nil)

func (f *failingLM) Stream(_ context.Context, _ fantasy.Call) (fantasy.StreamResponse, error) {
	return nil, f.err
}

// countingLM counts summarization Stream calls.
type countingLM struct {
	*compactTestLM
	calls int
}

var _ fantasy.LanguageModel = (*countingLM)(nil)

func (c *countingLM) Stream(ctx context.Context, call fantasy.Call) (fantasy.StreamResponse, error) {
	c.calls++
	return c.compactTestLM.Stream(ctx, call)
}

func TestEstimateStr_NonASCII(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want int64
	}{
		{"", 0},
		{"abcd", 1},
		{"abcdefgh", 2},
		{"你好", 2},    // two non-ASCII runes → two tokens
		{"hi你好", 2},  // 2 ASCII chars → 0, plus 2 wide
		{"a你b好c", 2}, // ASCII floor plus two wide runes
	} {
		if got := estimateStr(tc.in); got != tc.want {
			t.Errorf("estimateStr(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestRunCompaction_TruncatesWhenSummarizerFails(t *testing.T) {
	lm := &failingLM{compactTestLM: &compactTestLM{}, err: errors.New("summarizer unavailable")}
	history := longHistory(300) // 30k tokens
	window := int64(20_000)
	budget := newCompactionBudget(window, CompactConfig{Enabled: true, ThresholdPct: 0.80})

	res, err := runCompaction(context.Background(), lm, compactionRun{
		History:       history,
		Window:        window,
		Config:        CompactConfig{Enabled: true, ThresholdPct: 0.80},
		Trigger:       CompactionTriggerProactive,
		Force:         true,
		AllowTruncate: true,
	})
	if err != nil {
		t.Fatalf("runCompaction returned error despite truncation fallback: %v", err)
	}
	if res.Method != CompactionMethodTruncate {
		t.Errorf("method = %q, want %q", res.Method, CompactionMethodTruncate)
	}
	if res.Summary == "" {
		t.Error("truncation result has no summary/notice")
	}
	if res.EstimatedTokens > budget.Safety {
		t.Errorf("truncated estimate = %d, want <= safety %d", res.EstimatedTokens, budget.Safety)
	}
	if len(res.History) >= len(history) {
		t.Errorf("truncation did not reduce history: %d >= %d", len(res.History), len(history))
	}
	if res.TokensBefore <= res.TokensAfter {
		t.Errorf("TokensBefore = %d, TokensAfter = %d, want a reduction", res.TokensBefore, res.TokensAfter)
	}
	// The newest message must survive so the turn can continue.
	last := history[len(history)-1]
	if got := res.History[len(res.History)-1]; got.Content != last.Content {
		t.Error("newest message was not preserved by truncation")
	}
}

func TestRunCompaction_ManualDoesNotTruncateOnFailure(t *testing.T) {
	lm := &failingLM{compactTestLM: &compactTestLM{}, err: errors.New("summarizer unavailable")}
	history := longHistory(300)

	res, err := runCompaction(context.Background(), lm, compactionRun{
		History:       history,
		Window:        20_000,
		Config:        CompactConfig{Enabled: true, ThresholdPct: 0.80},
		Trigger:       CompactionTriggerManual,
		Force:         true,
		AllowTruncate: false,
	})
	if err == nil {
		t.Fatal("manual compaction hid the summarizer failure")
	}
	if res.Summary != "" {
		t.Errorf("failed manual compaction reported summary %q", res.Summary)
	}
	if len(res.History) != len(history) {
		t.Errorf("failed manual compaction changed history length: %d != %d", len(res.History), len(history))
	}
}

func TestRunCompaction_ProgressGuardFoldsFurther(t *testing.T) {
	lm := &countingLM{compactTestLM: &compactTestLM{response: "summary"}}
	history := longHistory(300) // 30k tokens of foldable history
	// An 11.25k-token system prompt leaves the first pass (keep 2k) above the
	// 12k recovery band, forcing the guard's second pass with keep 1k, which
	// then fits under the 15k safety limit.
	systemPrompt := strings.Repeat("s", 45_000)

	res, err := runCompaction(context.Background(), lm, compactionRun{
		History:       history,
		SystemPrompt:  systemPrompt,
		Window:        20_000,
		Config:        CompactConfig{Enabled: true, ThresholdPct: 0.80},
		Trigger:       CompactionTriggerProactive,
		Force:         true,
		AllowTruncate: true,
	})
	if err != nil {
		t.Fatalf("runCompaction: %v", err)
	}
	if lm.calls != 2 {
		t.Errorf("summarization calls = %d, want 2 (initial + headroom pass)", lm.calls)
	}
	if res.Method != CompactionMethodSummary {
		t.Errorf("method = %q, want summary", res.Method)
	}
	if res.EstimatedTokens > newCompactionBudget(20_000, CompactConfig{Enabled: true, ThresholdPct: 0.80}).Safety {
		t.Errorf("post-compaction estimate = %d exceeds safety", res.EstimatedTokens)
	}
}

func TestRunCompaction_NonShrinkingSummaryNotCommitted(t *testing.T) {
	lm := &compactTestLM{response: "summary"}
	history := []sdk.Message{
		{Role: sdk.RoleUser, Content: "hi"},
		{Role: sdk.RoleAssistant, Content: "hello"},
		{Role: sdk.RoleUser, Content: "second"},
		{Role: sdk.RoleAssistant, Content: "ok"},
	}

	res, err := runCompaction(context.Background(), lm, compactionRun{
		History:       history,
		Window:        200_000,
		Config:        CompactConfig{Enabled: true, ThresholdPct: 0.80},
		Trigger:       CompactionTriggerProactive,
		Force:         true,
		AllowTruncate: true,
	})
	if err != nil {
		t.Fatalf("runCompaction: %v", err)
	}
	if res.Summary != "" {
		t.Errorf("non-shrinking summary was committed: %q", res.Summary)
	}
	if len(res.History) != len(history) {
		t.Errorf("history changed despite no reduction: %d != %d", len(res.History), len(history))
	}
}

func TestBuildSummarySource_PreservesFullTextWithinBudget(t *testing.T) {
	msgs := []sdk.Message{
		{Role: sdk.RoleUser, Content: strings.Repeat("u", 400)},
		{Role: sdk.RoleAssistant, Content: strings.Repeat("a", 400)},
	}
	src := buildSummarySource(msgs, "", 10_000)
	if strings.Contains(src, "[truncated]") {
		t.Error("source truncated despite ample budget")
	}
	if !strings.Contains(src, msgs[0].Content) || !strings.Contains(src, msgs[1].Content) {
		t.Error("full message text missing from source")
	}
}

func TestBuildSummarySource_TruncatesOverBudget(t *testing.T) {
	msgs := make([]sdk.Message, 10)
	for i := range msgs {
		msgs[i] = sdk.Message{Role: sdk.RoleUser, Content: strings.Repeat("x", 10_000)}
	}
	src := buildSummarySource(msgs, "", 1_000)
	if !strings.Contains(src, "[truncated]") {
		t.Error("over-budget source was not truncated")
	}
	if estimateStr(src) > 1_500 {
		t.Errorf("source estimate = %d, want near the 1000-token budget", estimateStr(src))
	}
}

func TestTruncateContent_PreservesHeadAndTail(t *testing.T) {
	content := "HEAD" + strings.Repeat("x", 10_000) + "TAIL"
	got := truncateContent(content, 100)
	if !strings.HasPrefix(got, "HEAD") {
		t.Error("truncation dropped the head")
	}
	if !strings.HasSuffix(got, "TAIL") {
		t.Error("truncation dropped the tail")
	}
	if !strings.Contains(got, "[truncated]") {
		t.Error("truncation marker missing")
	}
	if estimateStr(got) > 130 {
		t.Errorf("truncated content estimate = %d, want near 100", estimateStr(got))
	}
}
