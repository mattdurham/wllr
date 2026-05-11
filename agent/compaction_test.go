package agent

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// compaction_test.go tests the internal compaction helpers.
// It lives in package agent (white-box) to access unexported functions.

import (
	"context"
	"strings"
	"testing"

	"charm.land/fantasy"
	"github.com/mattdurham/wllr/sdk"
)

// ---- contextWindowForModel ----

func TestContextWindowForModel_AlwaysReturnsDefault(t *testing.T) {
	// contextWindowForModel no longer uses model-name heuristics — it always
	// returns defaultContextWindow. Explicit overrides go through the pool
	// (SetContextWindow) or WLLR_CONTEXT_WINDOW env var.
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
		{Role: sdk.RoleUser, Content: "abcdefgh"}, // 8 chars → 2 tokens
		{Role: sdk.RoleAssistant, Content: "1234"},  // 4 chars → 1 token
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
	// passing 0 should fall back to defaultContextWindow (200_000), not panic.
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

// ---- compactHistory (integration) ----

func TestCompactHistory_ShortHistory_ReturnsUnchanged(t *testing.T) {
	lm := &compactTestLM{response: "summary text"}
	history := make([]sdk.Message, keepMessages) // exactly at threshold
	for i := range history {
		history[i] = sdk.Message{Role: sdk.RoleUser, Content: "msg"}
	}

	result, err := compactHistory(context.Background(), lm, history)
	if err != nil {
		t.Fatalf("compactHistory: %v", err)
	}
	// Should be returned unchanged (not enough messages to compact).
	if len(result) != len(history) {
		t.Errorf("expected %d messages, got %d", len(history), len(result))
	}
}

func TestCompactHistory_LongHistory_ReturnsSummaryPlusRecent(t *testing.T) {
	lm := &compactTestLM{response: "This is the summary of the conversation."}

	// Create more messages than keepMessages.
	totalMsgs := keepMessages + 5
	history := make([]sdk.Message, totalMsgs)
	for i := range history {
		history[i] = sdk.Message{Role: sdk.RoleUser, Content: "message content here"}
	}

	result, err := compactHistory(context.Background(), lm, history)
	if err != nil {
		t.Fatalf("compactHistory: %v", err)
	}

	// Result should be anchor (first msg) + 1 summary + keepMessages recent messages.
	expected := 1 + 1 + keepMessages
	if len(result) != expected {
		t.Errorf("expected %d messages, got %d", expected, len(result))
	}

	// First message is the preserved anchor (original task).
	if result[0].Content != "message content here" {
		t.Errorf("first message should be the anchor, got: %q", result[0].Content)
	}

	// Second message should contain the summary.
	if !strings.Contains(result[1].Content, "summary") {
		t.Errorf("second message should contain summary text, got: %q", result[1].Content)
	}
}

func TestCompactHistory_EmptySummary_ReturnsOriginal(t *testing.T) {
	// LM that returns empty — compactHistory should return original.
	lm := &compactTestLM{response: ""}

	totalMsgs := keepMessages + 5
	history := make([]sdk.Message, totalMsgs)
	for i := range history {
		history[i] = sdk.Message{Role: sdk.RoleUser, Content: "msg"}
	}

	result, err := compactHistory(context.Background(), lm, history)
	// Should error and return original.
	if err == nil {
		t.Error("expected error for empty summary")
	}
	if len(result) != len(history) {
		t.Errorf("expected original %d messages, got %d", len(history), len(result))
	}
}

// compactTestLM is a fantasy.LanguageModel used for compaction tests.
type compactTestLM struct {
	response string
}

func (c *compactTestLM) Model() string    { return "compact-test" }
func (c *compactTestLM) Provider() string { return "test" }

func (c *compactTestLM) Stream(_ context.Context, _ fantasy.Call) (fantasy.StreamResponse, error) {
	resp := c.response
	return func(yield func(fantasy.StreamPart) bool) {
		if resp != "" {
			yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextDelta, Delta: resp})
		}
		yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeFinish, FinishReason: fantasy.FinishReasonStop})
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
