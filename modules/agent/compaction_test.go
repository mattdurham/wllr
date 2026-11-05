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

func TestContextWindowForModel_KnownModels(t *testing.T) {
	for model, want := range map[string]int64{"claude-sonnet-4-6": 200_000, "gpt-4o": 128_000} {
		if got := contextWindowForModel(model); got != want {
			t.Errorf("contextWindowForModel(%q) = %d, want %d", model, got, want)
		}
	}
}

func TestContextWindowForModel_UnknownRequiresResolution(t *testing.T) {
	if got := contextWindowForModel("unknown-model"); got != 0 {
		t.Fatalf("contextWindowForModel(unknown) = %d, want 0", got)
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

// ---- compaction budget / preflight ----

func TestShouldCompactByBudget_BelowTrigger(t *testing.T) {
	b := newCompactionBudget(200_000, CompactConfig{Enabled: true, ThresholdPct: 0.80})
	if shouldCompactByBudget(100_000, b) {
		t.Error("100k estimate fired the 80% trigger on a 200k window")
	}
}

func TestShouldCompactByBudget_AtTrigger(t *testing.T) {
	b := newCompactionBudget(200_000, CompactConfig{Enabled: true, ThresholdPct: 0.80})
	if !shouldCompactByBudget(b.Trigger, b) {
		t.Errorf("estimate at trigger %d did not fire", b.Trigger)
	}
}

func TestShouldCompactByBudget_DisabledUsesSafety(t *testing.T) {
	b := newCompactionBudget(200_000, CompactConfig{Enabled: false})
	if b.Trigger != b.Safety {
		t.Fatalf("trigger = %d, want safety %d when the percentage trigger is disabled", b.Trigger, b.Safety)
	}
	if shouldCompactByBudget(b.Safety-1, b) {
		t.Error("estimate below the safety limit fired with the percentage trigger disabled")
	}
	if !shouldCompactByBudget(b.Safety, b) {
		t.Error("estimate at the safety limit did not fire with the percentage trigger disabled")
	}
}

func TestNewCompactionBudget_RecoveryBand(t *testing.T) {
	for _, tc := range []struct {
		name   string
		window int64
	}{
		{"200k", 200_000},
		{"1M", 1_000_000},
		{"32k", 32_000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := newCompactionBudget(tc.window, CompactConfig{Enabled: true, ThresholdPct: 0.80})
			if b.KeepRecent <= 0 || b.KeepRecent > b.Trigger/2 {
				t.Fatalf("KeepRecent = %d, want in (0, trigger/2=%d]", b.KeepRecent, b.Trigger/2)
			}
			if b.Recovery != int64(float64(b.Trigger)*0.8) {
				t.Fatalf("Recovery = %d, want 80%% of trigger %d", b.Recovery, b.Trigger)
			}
			if b.Safety < b.Trigger {
				t.Fatalf("Safety = %d, want >= Trigger %d", b.Safety, b.Trigger)
			}
		})
	}
}

func TestShouldCompactByBudget_IncludesToolDefinitions(t *testing.T) {
	tool := fantasy.NewAgentTool(
		"large_tool",
		strings.Repeat("description ", 6000),
		func(context.Context, map[string]any, fantasy.ToolCall) (fantasy.ToolResponse, error) {
			return fantasy.NewTextResponse("ok"), nil
		},
	)
	b := newCompactionBudget(20_000, CompactConfig{Enabled: true, ThresholdPct: 0.80})
	estimate := contextEstimate(nil, "", "", []fantasy.AgentTool{tool})
	if !shouldCompactByBudget(estimate, b) {
		t.Error("tool definition estimate did not fill the budget")
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

	result, err := compactHistory(context.Background(), lm, compactionRequest{
		History:          history,
		KeepRecentTokens: 0,
		Trigger:          CompactionTriggerProactive,
	})
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

	result, err := compactHistory(context.Background(), lm, compactionRequest{
		History:          history,
		KeepRecentTokens: 0,
		Trigger:          CompactionTriggerProactive,
	})
	if err != nil {
		t.Fatalf("compactHistory: %v", err)
	}

	// Result should be: anchor + summary + recent messages (fewer than total).
	if len(result.History) >= len(history) {
		t.Errorf(
			"expected fewer messages after compaction, got %d (same as input %d)",
			len(result.History),
			len(history),
		)
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

	result, err := compactHistory(context.Background(), lm, compactionRequest{
		History:          history,
		KeepRecentTokens: 0,
		Trigger:          CompactionTriggerProactive,
	})
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

func TestFindCutPoint_SmallHistory_Fits(t *testing.T) {
	history := []sdk.Message{
		{Role: sdk.RoleUser, Content: "hello"},
		{Role: sdk.RoleAssistant, Content: "world"},
		{Role: sdk.RoleUser, Content: "next"},
	}
	got, ok := findCutPoint(history, 20_000)
	if ok || got != 0 {
		t.Errorf("findCutPoint small history = (%d, %v), want (0, false)", got, ok)
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
	got, ok := findCutPoint(history, 250)
	if !ok || got != 4 {
		t.Errorf("findCutPoint snap-to-user = (%d, %v), want (4, true)", got, ok)
	}
}

func TestFindCutPoint_AllFitInBudget_ReturnsZero(t *testing.T) {
	history := []sdk.Message{
		{Role: sdk.RoleUser, Content: "a"},
		{Role: sdk.RoleAssistant, Content: "b"},
		{Role: sdk.RoleUser, Content: "c"},
	}
	got, ok := findCutPoint(history, 1_000_000)
	if ok || got != 0 {
		t.Errorf("findCutPoint huge budget = (%d, %v), want (0, false)", got, ok)
	}
}

func TestFindCutPoint_NoUserBoundaryFound_ReturnsFallback(t *testing.T) {
	// Pathological: all assistant messages, budget = 1 token.
	// No user boundary exists, so there is no safe cut: the caller must skip
	// compaction rather than produce a kept slice starting with an assistant.
	history := []sdk.Message{
		{Role: sdk.RoleAssistant, Content: strings.Repeat("x", 400)},
		{Role: sdk.RoleAssistant, Content: strings.Repeat("x", 400)},
		{Role: sdk.RoleAssistant, Content: strings.Repeat("x", 400)},
	}
	got, ok := findCutPoint(history, 1)
	if ok || got != 0 {
		t.Errorf("findCutPoint no-user-boundary = (%d, %v), want (0, false)", got, ok)
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
	res, err := compactHistory(context.Background(), lm, compactionRequest{
		History:          history,
		PriorSummary:     priorSummary,
		KeepRecentTokens: 0,
		Trigger:          CompactionTriggerProactive,
	})
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
	res, err := compactHistory(context.Background(), lm, compactionRequest{
		History:          history,
		KeepRecentTokens: 0,
		Trigger:          CompactionTriggerProactive,
	})
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

	result, err := compactHistory(context.Background(), lm, compactionRequest{
		History:          history,
		KeepRecentTokens: 0,
		Trigger:          CompactionTriggerProactive,
	})
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
