package agent

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// compaction_test.go tests the internal compaction helpers.
// It lives in package agent (white-box) to access unexported functions.

import (
	"context"
	"strings"
	"testing"

	"charm.land/fantasy"
	"github.com/mattdurham/wllr/modules/sdk"
)

// ---- contextWindowForModel ----

func TestContextWindowForModel_KnownModels_ReturnDefault(t *testing.T) {
	// contextWindowForModel looks up the model in the generated table (exact then
	// substring match). All currently-mapped models happen to return 1_000_000
	// which equals defaultContextWindow, so known and unknown names both return
	// defaultContextWindow today.
	for _, model := range []string{"claude-sonnet-4-6", "gpt-4o", "gemini-2.0", "", "unknown"} {
		got := contextWindowForModel(model)
		if got != defaultContextWindow {
			t.Errorf("contextWindowForModel(%q) = %d, want %d (default)", model, got, defaultContextWindow)
		}
	}
}

// ---- estimateTokens ----

func TestEstimateTokens_Empty(t *testing.T) {
	got := estimateTokens(nil)
	if got != 0 {
		t.Errorf("estimateTokens(nil) = %d, want 0", got)
	}
}

func TestEstimateTokens_CharsOver4(t *testing.T) {
	msgs := []sdk.Message{
		{Role: sdk.RoleUser, Content: "abcdefgh"},  // 8 chars → 2 tokens
		{Role: sdk.RoleAssistant, Content: "1234"}, // 4 chars → 1 token
	}
	got := estimateTokens(msgs)
	if got != 3 {
		t.Errorf("estimateTokens = %d, want 3", got)
	}
}

func TestEstimateTokens_SingleMessage(t *testing.T) {
	msgs := []sdk.Message{
		{Role: sdk.RoleUser, Content: strings.Repeat("x", 400)}, // 400 chars → 100 tokens
	}
	got := estimateTokens(msgs)
	if got != 100 {
		t.Errorf("estimateTokens = %d, want 100", got)
	}
}

// ---- shouldCompact ----

func TestShouldCompact_BelowThreshold_ReturnsFalse(t *testing.T) {
	// Very small history — nowhere near the limit.
	history := []sdk.Message{
		{Role: sdk.RoleUser, Content: "hello"},
		{Role: sdk.RoleAssistant, Content: "world"},
	}
	result := shouldCompact(history, "sys", "next", 200_000)
	if result {
		t.Error("shouldCompact returned true for small history, expected false")
	}
}

func TestShouldCompact_AboveThreshold_ReturnsTrue(t *testing.T) {
	// History that fills most of the context window.
	// contextWindow=200_000, reserveTokens=16_384 → threshold=183_616 tokens
	// 183_616 tokens * 4 chars/token = 734_464 chars
	longMsg := strings.Repeat("a", 750_000) // definitely over
	history := []sdk.Message{
		{Role: sdk.RoleUser, Content: longMsg},
	}
	result := shouldCompact(history, "", "", 200_000)
	if !result {
		t.Error("shouldCompact returned false for history above threshold, expected true")
	}
}

func TestShouldCompact_ZeroWindow_UsesDefault(t *testing.T) {
	// passing 0 should fall back to defaultContextWindow (1_000_000), not panic.
	history := []sdk.Message{
		{Role: sdk.RoleUser, Content: "tiny"},
	}
	// Should not panic.
	result := shouldCompact(history, "", "", 0)
	if result {
		t.Error("shouldCompact(tiny, 0 window) returned true, expected false")
	}
}

func TestShouldCompact_NegativeWindow_UsesDefault(t *testing.T) {
	history := []sdk.Message{
		{Role: sdk.RoleUser, Content: "tiny"},
	}
	result := shouldCompact(history, "", "", -1)
	if result {
		t.Error("shouldCompact(tiny, -1 window) returned true, expected false")
	}
}

func TestShouldCompactWithTools_IncludesToolDefinitions(t *testing.T) {
	tool := fantasy.NewAgentTool(
		"large_tool",
		strings.Repeat("description ", 3000),
		func(context.Context, map[string]any, fantasy.ToolCall) (fantasy.ToolResponse, error) {
			return fantasy.NewTextResponse("ok"), nil
		},
	)

	if !shouldCompactWithTools(nil, "", "", []fantasy.AgentTool{tool}, 20_000) {
		t.Error("shouldCompactWithTools returned false even though the tool definition fills the context budget")
	}
}

// ---- compactHistory (integration) ----

func TestCompactHistory_ShortHistory_ReturnsUnchanged(t *testing.T) {
	lm := &compactTestLM{response: "summary text"}
	// 20 messages × 100 tokens each = 2,000 tokens, well within the 20,000-token budget — no compaction.
	// Use 400-char content so token estimation is non-zero (400/4 = 100 tokens).
	msg := strings.Repeat("x", 400)
	history := make([]sdk.Message, keepMessages)
	for i := range history {
		history[i] = sdk.Message{Role: sdk.RoleUser, Content: msg}
	}

	result, err := compactHistory(context.Background(), lm, history, "", 0, CompactionTriggerProactive)
	if err != nil {
		t.Fatalf("compactHistory: %v", err)
	}
	// Should be returned unchanged — total tokens (2,000) fit within budget (20,000).
	if len(result.History) != len(history) {
		t.Errorf("expected %d messages, got %d", len(history), len(result.History))
	}
	// A no-op compaction must not report a summary or usage.
	if result.Summary != "" {
		t.Errorf("no-op compaction reported a summary: %q", result.Summary)
	}
}

func TestCompactHistory_LongHistory_ReturnsSummaryPlusRecent(t *testing.T) {
	lm := &compactTestLM{response: "This is the summary of the conversation."}

	// Each message is 400 chars → 100 tokens; 212 messages × 100 = 21,200 tokens > 20,000.
	// The token budget walk keeps the last ~200 tokens (2 messages), so many messages are compacted.
	msg := strings.Repeat("x", 400)
	history := make([]sdk.Message, 212)
	for i := range history {
		if i%2 == 0 {
			history[i] = sdk.Message{Role: sdk.RoleUser, Content: msg}
		} else {
			history[i] = sdk.Message{Role: sdk.RoleAssistant, Content: msg}
		}
	}
	// Override the first message to be identifiable as the anchor.
	history[0] = sdk.Message{Role: sdk.RoleUser, Content: "anchor message"}

	result, err := compactHistory(context.Background(), lm, history, "", 0, CompactionTriggerProactive)
	if err != nil {
		t.Fatalf("compactHistory: %v", err)
	}

	// Result should be: anchor + summary + recent messages (fewer than total).
	if len(result.History) >= len(history) {
		t.Errorf("expected fewer messages after compaction, got %d (same as input %d)", len(result.History), len(history))
	}
	if len(result.History) < 3 {
		t.Errorf("expected at least 3 messages (anchor + summary + 1 recent), got %d", len(result.History))
	}

	// First message is the preserved anchor (original task).
	if result.History[0].Content != "anchor message" {
		t.Errorf("first message should be the anchor, got: %q", result.History[0].Content)
	}

	// Second message should contain the summary.
	if !strings.Contains(result.History[1].Content, "summary") {
		t.Errorf("second message should contain summary text, got: %q", result.History[1].Content)
	}
}

func TestCompactHistory_EmptySummary_ReturnsOriginal(t *testing.T) {
	// LM that returns empty — compactHistory should return original.
	lm := &compactTestLM{response: ""}

	// Use large enough messages to trigger token-based compaction.
	msg := strings.Repeat("x", 400) // 100 tokens each
	history := make([]sdk.Message, 212)
	for i := range history {
		if i%2 == 0 {
			history[i] = sdk.Message{Role: sdk.RoleUser, Content: msg}
		} else {
			history[i] = sdk.Message{Role: sdk.RoleAssistant, Content: msg}
		}
	}

	result, err := compactHistory(context.Background(), lm, history, "", 0, CompactionTriggerProactive)
	// Should error and return original.
	if err == nil {
		t.Error("expected error for empty summary")
	}
	if len(result.History) != len(history) {
		t.Errorf("expected original %d messages, got %d", len(history), len(result.History))
	}
}

// compactTestLM is a fantasy.LanguageModel used for compaction tests.
type compactTestLM struct {
	response  string
	inputTok  int64
	outputTok int64
}

func (c *compactTestLM) Model() string    { return "compact-test" }
func (c *compactTestLM) Provider() string { return "test" }

func (c *compactTestLM) Stream(_ context.Context, _ fantasy.Call) (fantasy.StreamResponse, error) {
	resp := c.response
	return func(yield func(fantasy.StreamPart) bool) {
		if resp != "" {
			yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextDelta, Delta: resp})
		}
		yield(fantasy.StreamPart{
			Type:         fantasy.StreamPartTypeFinish,
			FinishReason: fantasy.FinishReasonStop,
			Usage: fantasy.Usage{
				InputTokens:  c.inputTok,
				OutputTokens: c.outputTok,
				TotalTokens:  c.inputTok + c.outputTok,
			},
		})
	}, nil
}

func (c *compactTestLM) Generate(_ context.Context, _ fantasy.Call) (*fantasy.Response, error) {
	return &fantasy.Response{
		Content:      fantasy.ResponseContent{fantasy.TextContent{Text: c.response}},
		FinishReason: fantasy.FinishReasonStop,
	}, nil
}

func (c *compactTestLM) GenerateObject(_ context.Context, _ fantasy.ObjectCall) (*fantasy.ObjectResponse, error) {
	return nil, nil
}

func (c *compactTestLM) StreamObject(_ context.Context, _ fantasy.ObjectCall) (fantasy.ObjectStreamResponse, error) {
	return nil, nil
}

// ---- findCutPoint ----

func TestFindCutPoint_SmallHistory_ReturnsZero(t *testing.T) {
	history := []sdk.Message{
		{Role: sdk.RoleUser, Content: "hello"},
		{Role: sdk.RoleAssistant, Content: "world"},
		{Role: sdk.RoleUser, Content: "next"},
	}
	got := findCutPoint(history, 20_000)
	if got != 0 {
		t.Errorf("findCutPoint small history = %d, want 0", got)
	}
}

func TestFindCutPoint_SnapToUserBoundary(t *testing.T) {
	// Build: user0, asst0, user1, asst1, user2, asst2
	// Each message is 400 chars → 100 tokens.
	// Budget = 250 tokens → fits exactly 2.5 messages from the end.
	// Walking back: asst2 (acc=100), user2 (acc=200), asst1 (acc=300 > 250 → bust at i=3).
	// Snap forward from j=3 (assistant) to j=4 (user2). Cut point = 4 → keep history[4:].
	msg := strings.Repeat("x", 400) // 100 tokens each
	history := []sdk.Message{
		{Role: sdk.RoleUser, Content: msg},      // 0
		{Role: sdk.RoleAssistant, Content: msg}, // 1
		{Role: sdk.RoleUser, Content: msg},      // 2
		{Role: sdk.RoleAssistant, Content: msg}, // 3
		{Role: sdk.RoleUser, Content: msg},      // 4
		{Role: sdk.RoleAssistant, Content: msg}, // 5
	}
	got := findCutPoint(history, 250)
	if got != 4 {
		t.Errorf("findCutPoint snap-to-user = %d, want 4", got)
	}
}

func TestFindCutPoint_AllFitInBudget_ReturnsZero(t *testing.T) {
	history := []sdk.Message{
		{Role: sdk.RoleUser, Content: "a"},
		{Role: sdk.RoleAssistant, Content: "b"},
		{Role: sdk.RoleUser, Content: "c"},
	}
	got := findCutPoint(history, 1_000_000)
	if got != 0 {
		t.Errorf("findCutPoint huge budget = %d, want 0", got)
	}
}

func TestFindCutPoint_NoUserBoundaryFound_ReturnsFallback(t *testing.T) {
	// Pathological: all assistant messages, budget = 1 token.
	// No user boundary exists — findCutPoint must return -1 (skip compaction sentinel)
	// so the caller never produces a history slice starting with an assistant message.
	history := []sdk.Message{
		{Role: sdk.RoleAssistant, Content: strings.Repeat("x", 400)},
		{Role: sdk.RoleAssistant, Content: strings.Repeat("x", 400)},
		{Role: sdk.RoleAssistant, Content: strings.Repeat("x", 400)},
	}
	got := findCutPoint(history, 1)
	if got != -1 {
		t.Errorf("findCutPoint no-user-boundary fallback = %d, want -1", got)
	}
}

// ---- extractFilePaths ----

func TestExtractFilePaths_AbsolutePaths(t *testing.T) {
	msgs := []sdk.Message{
		{Role: sdk.RoleAssistant, Content: `read_file /home/user/project/main.go`},
		{Role: sdk.RoleUser, Content: `wrote /tmp/output.txt to disk`},
	}
	got := extractFilePaths(msgs)
	want := map[string]bool{
		"/home/user/project/main.go": true,
		"/tmp/output.txt":            true,
	}
	if len(got) != len(want) {
		t.Fatalf("extractFilePaths count = %d, want %d; got %v", len(got), len(want), got)
	}
	for _, p := range got {
		if !want[p] {
			t.Errorf("unexpected path %q", p)
		}
	}
}

func TestExtractFilePaths_Deduplication(t *testing.T) {
	msgs := []sdk.Message{
		{Role: sdk.RoleAssistant, Content: `/home/user/main.go read`},
		{Role: sdk.RoleAssistant, Content: `/home/user/main.go written`},
	}
	got := extractFilePaths(msgs)
	if len(got) != 1 {
		t.Errorf("extractFilePaths dedup = %d paths, want 1; got %v", len(got), got)
	}
}

func TestExtractFilePaths_NoMatches_ReturnsEmpty(t *testing.T) {
	msgs := []sdk.Message{
		{Role: sdk.RoleUser, Content: "hello world, no file here"},
	}
	got := extractFilePaths(msgs)
	if len(got) != 0 {
		t.Errorf("extractFilePaths empty = %v, want []", got)
	}
}

// ---- compactHistory with priorSummary and res.Summary ----

func TestCompactHistory_WithPriorSummary_IncludesPriorInPrompt(t *testing.T) {
	var capturedPrompt string
	lm := &captureLM{onPrompt: func(s string) { capturedPrompt = s }}

	// Each message is 400 chars → 100 tokens; 212 messages × 100 = 21,200 tokens > 20,000.
	msg := strings.Repeat("x", 400)
	history := make([]sdk.Message, 212)
	for i := range history {
		if i%2 == 0 {
			history[i] = sdk.Message{Role: sdk.RoleUser, Content: msg}
		} else {
			history[i] = sdk.Message{Role: sdk.RoleAssistant, Content: msg}
		}
	}

	priorSummary := "## Goal\nPrior work summary."
	res, err := compactHistory(context.Background(), lm, history, priorSummary, 0, CompactionTriggerProactive)
	if err != nil {
		t.Fatalf("compactHistory: %v", err)
	}
	if !strings.Contains(capturedPrompt, priorSummary) {
		t.Errorf("prompt does not contain prior summary; prompt = %q", capturedPrompt[:min(200, len(capturedPrompt))])
	}
	if res.Summary == "" {
		t.Error("expected non-empty res.Summary")
	}
}

func TestCompactHistory_ReturnsSummaryString(t *testing.T) {
	lm := &compactTestLM{response: "This is the summary text."}
	msg := strings.Repeat("x", 400)
	history := make([]sdk.Message, 212)
	for i := range history {
		if i%2 == 0 {
			history[i] = sdk.Message{Role: sdk.RoleUser, Content: msg}
		} else {
			history[i] = sdk.Message{Role: sdk.RoleAssistant, Content: msg}
		}
	}
	res, err := compactHistory(context.Background(), lm, history, "", 0, CompactionTriggerProactive)
	if err != nil {
		t.Fatalf("compactHistory: %v", err)
	}
	if res.Summary != "This is the summary text." {
		t.Errorf("res.Summary = %q, want %q", res.Summary, "This is the summary text.")
	}
}

func TestCompactHistory_FilePathsAppendedToSummaryMessage(t *testing.T) {
	lm := &compactTestLM{response: "summary"}
	msg := strings.Repeat("x", 400)
	history := make([]sdk.Message, 212)
	for i := range history {
		if i%2 == 0 {
			history[i] = sdk.Message{Role: sdk.RoleUser, Content: msg}
		} else {
			history[i] = sdk.Message{Role: sdk.RoleAssistant, Content: msg}
		}
	}
	// Inject a file path into one of the messages that will be summarized.
	history[2] = sdk.Message{Role: sdk.RoleAssistant, Content: "read_file /home/user/project/foo.go result: ok"}

	result, err := compactHistory(context.Background(), lm, history, "", 0, CompactionTriggerProactive)
	if err != nil {
		t.Fatalf("compactHistory: %v", err)
	}
	// The summary message is result.History[1] (index 0 is anchor).
	summaryMsg := result.History[1].Content
	if !strings.Contains(summaryMsg, "/home/user/project/foo.go") {
		t.Errorf("summary message does not list file path; content = %q", summaryMsg[:min(200, len(summaryMsg))])
	}
}

// captureLM records the prompt passed to Stream.
type captureLM struct {
	onPrompt func(string)
	response string
}

func (c *captureLM) Model() string    { return "capture-test" }
func (c *captureLM) Provider() string { return "test" }

func (c *captureLM) Stream(_ context.Context, call fantasy.Call) (fantasy.StreamResponse, error) {
	if c.onPrompt != nil {
		var sb strings.Builder
		for _, m := range call.Prompt {
			for _, p := range m.Content {
				if tp, ok := p.(fantasy.TextPart); ok {
					sb.WriteString(tp.Text)
				}
			}
		}
		c.onPrompt(sb.String())
	}
	resp := c.response
	if resp == "" {
		resp = "LM summary output"
	}
	return func(yield func(fantasy.StreamPart) bool) {
		yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextDelta, Delta: resp})
		yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeFinish, FinishReason: fantasy.FinishReasonStop})
	}, nil
}

func (c *captureLM) Generate(_ context.Context, _ fantasy.Call) (*fantasy.Response, error) {
	resp := c.response
	if resp == "" {
		resp = "LM summary output"
	}
	return &fantasy.Response{
		Content:      fantasy.ResponseContent{fantasy.TextContent{Text: resp}},
		FinishReason: fantasy.FinishReasonStop,
	}, nil
}

func (c *captureLM) GenerateObject(_ context.Context, _ fantasy.ObjectCall) (*fantasy.ObjectResponse, error) {
	return nil, nil
}

func (c *captureLM) StreamObject(_ context.Context, _ fantasy.ObjectCall) (fantasy.ObjectStreamResponse, error) {
	return nil, nil
}

// ---- shouldCompactByUsage ----

// TestShouldCompactByUsage_BelowThreshold verifies that usage below the threshold returns false.
func TestShouldCompactByUsage_BelowThreshold(t *testing.T) {
	// 79% of window with 0.80 threshold: should not compact.
	u := fantasy.Usage{InputTokens: 158_000, OutputTokens: 100}
	got := shouldCompactByUsage(u, 200_000, 0.80)
	if got {
		t.Error("expected false at 79% with threshold 0.80, got true")
	}
}

// TestShouldCompactByUsage_AtThreshold verifies that usage at exactly the threshold returns true.
func TestShouldCompactByUsage_AtThreshold(t *testing.T) {
	// 80% of window with 0.80 threshold: should compact.
	u := fantasy.Usage{InputTokens: 160_000, OutputTokens: 100}
	got := shouldCompactByUsage(u, 200_000, 0.80)
	if !got {
		t.Error("expected true at 80% with threshold 0.80, got false")
	}
}

// TestShouldCompactByUsage_ZeroUsage verifies that zero InputTokens always returns false.
func TestShouldCompactByUsage_ZeroUsage(t *testing.T) {
	// First turn before any usage is recorded: InputTokens is 0.
	u := fantasy.Usage{}
	got := shouldCompactByUsage(u, 200_000, 0.80)
	if got {
		t.Error("expected false when InputTokens is zero, got true")
	}
}

// TestShouldCompactByUsage_ZeroContextWindow verifies that a zero window returns false.
func TestShouldCompactByUsage_ZeroContextWindow(t *testing.T) {
	// No context window configured — fall back to heuristic.
	u := fantasy.Usage{InputTokens: 500_000, OutputTokens: 100}
	got := shouldCompactByUsage(u, 0, 0.80)
	if got {
		t.Error("expected false when ContextWindow is zero, got true")
	}
}

// TestAutoCompactEnvDefault verifies that NewPool reads WLLR_COMPACT_THRESHOLD and
// sets a threshold of 0.80 when the env var is unset.
func TestAutoCompactEnvDefault(t *testing.T) {
	t.Setenv("WLLR_COMPACT_THRESHOLD", "")
	p := NewPool()
	cfg := p.CompactConfig()
	if cfg.ThresholdPct != 0.80 {
		t.Errorf("default ThresholdPct = %f, want 0.80", cfg.ThresholdPct)
	}
	if !cfg.Enabled {
		t.Error("expected Enabled to be true by default")
	}
}

// TestAutoCompactEnvCustom verifies that WLLR_COMPACT_THRESHOLD=90 sets threshold to 0.90.
func TestAutoCompactEnvCustom(t *testing.T) {
	t.Setenv("WLLR_COMPACT_THRESHOLD", "90")
	p := NewPool()
	cfg := p.CompactConfig()
	if cfg.ThresholdPct != 0.90 {
		t.Errorf("ThresholdPct = %f, want 0.90", cfg.ThresholdPct)
	}
}

// TestAutoCompactEnvFraction verifies that WLLR_COMPACT_THRESHOLD=0.90 (fraction, ≤1) is
// accepted as-is without the divide-by-100 conversion.
func TestAutoCompactEnvFraction(t *testing.T) {
	t.Setenv("WLLR_COMPACT_THRESHOLD", "0.90")
	p := NewPool()
	cfg := p.CompactConfig()
	if cfg.ThresholdPct != 0.90 {
		t.Errorf("ThresholdPct = %f, want 0.90", cfg.ThresholdPct)
	}
}

// TestAutoCompactEnvNegative verifies that a negative WLLR_COMPACT_THRESHOLD falls back
// to the default 0.80 (rejected by the parsed > 0 guard).
func TestAutoCompactEnvNegative(t *testing.T) {
	t.Setenv("WLLR_COMPACT_THRESHOLD", "-5")
	p := NewPool()
	cfg := p.CompactConfig()
	if cfg.ThresholdPct != 0.80 {
		t.Errorf("ThresholdPct = %f, want 0.80 (default)", cfg.ThresholdPct)
	}
}

// TestAutoCompactEnvInvalid verifies that an unparseable WLLR_COMPACT_THRESHOLD falls
// back to the default 0.80.
func TestAutoCompactEnvInvalid(t *testing.T) {
	t.Setenv("WLLR_COMPACT_THRESHOLD", "abc")
	p := NewPool()
	cfg := p.CompactConfig()
	if cfg.ThresholdPct != 0.80 {
		t.Errorf("ThresholdPct = %f, want 0.80 (default)", cfg.ThresholdPct)
	}
}

// TestAutoCompactEnvOutOfRange verifies that WLLR_COMPACT_THRESHOLD=200 (which becomes
// 2.0 after divide-by-100) is rejected with a warning and falls back to 0.80.
func TestAutoCompactEnvOutOfRange(t *testing.T) {
	t.Setenv("WLLR_COMPACT_THRESHOLD", "200")
	p := NewPool()
	cfg := p.CompactConfig()
	if cfg.ThresholdPct != 0.80 {
		t.Errorf("ThresholdPct = %f, want 0.80 (default after out-of-range rejection)", cfg.ThresholdPct)
	}
}
