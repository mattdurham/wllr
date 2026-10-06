package sdk_test

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"testing"

	"charm.land/fantasy"
	"github.com/mattdurham/wllr/modules/sdk"
)

func TestContextUsagePercent(t *testing.T) {
	cu := sdk.ContextUsage{
		InputTokens:   80_000,
		ContextWindow: 100_000,
	}
	cu = sdk.ContextUsageFromFantasy(fantasy.Usage{InputTokens: cu.InputTokens}, cu.ContextWindow)
	if cu.Percent != 80.0 {
		t.Errorf("Percent = %f, want 80.0", cu.Percent)
	}
}

func TestContextUsagePercentZeroWindow(t *testing.T) {
	cu := sdk.ContextUsageFromFantasy(fantasy.Usage{InputTokens: 1000}, 0)
	if cu.Percent != 0 {
		t.Errorf("Percent with zero window = %f, want 0", cu.Percent)
	}
}

func TestContextUsageFromFantasyUsage(t *testing.T) {
	u := fantasy.Usage{
		InputTokens:  1000,
		OutputTokens: 200,
	}
	cu := sdk.ContextUsageFromFantasy(u, 200_000)

	if cu.InputTokens != 1000 {
		t.Errorf("InputTokens = %d, want 1000", cu.InputTokens)
	}
	if cu.OutputTokens != 200 {
		t.Errorf("OutputTokens = %d, want 200", cu.OutputTokens)
	}
	if cu.ContextWindow != 200_000 {
		t.Errorf("ContextWindow = %d, want 200000", cu.ContextWindow)
	}
	// Percent = 1000 / 200000 * 100 = 0.5
	want := 0.5
	if cu.Percent != want {
		t.Errorf("Percent = %f, want %f", cu.Percent, want)
	}
}

// TestContextUsageIncludesCacheTokens pins the display numerator: cached tokens
// count toward used context. Anthropic reports cache reads/creations additively
// to input_tokens; OpenAI-family providers subtract cached tokens out of
// input_tokens. Summing all three is the only formula that reconstructs the
// true prompt size on both conventions — and without it a fully cache-served
// turn displays zero used context.
func TestContextUsageIncludesCacheTokens(t *testing.T) {
	// OpenAI convention: input already excludes the cached portion.
	cu := sdk.ContextUsageFromFantasy(fantasy.Usage{
		InputTokens:     10_000,
		CacheReadTokens: 80_000,
		OutputTokens:    500,
	}, 200_000)
	if cu.InputTokens != 90_000 {
		t.Errorf("InputTokens = %d, want 90000 (input + cache read)", cu.InputTokens)
	}
	if cu.Percent != 45.0 {
		t.Errorf("Percent = %f, want 45.0", cu.Percent)
	}

	// Anthropic convention: cache reads AND creations are additive.
	cu = sdk.ContextUsageFromFantasy(fantasy.Usage{
		InputTokens:         10_000,
		CacheReadTokens:     70_000,
		CacheCreationTokens: 5_000,
		OutputTokens:        500,
	}, 200_000)
	if cu.InputTokens != 85_000 {
		t.Errorf("InputTokens = %d, want 85000 (input + read + creation)", cu.InputTokens)
	}

	// Fully cache-served turn (OpenAI: input − cached = 0) is still visible.
	cu = sdk.ContextUsageFromFantasy(fantasy.Usage{
		InputTokens:     0,
		CacheReadTokens: 90_000,
	}, 200_000)
	if cu.InputTokens != 90_000 || cu.Percent != 45.0 {
		t.Errorf("InputTokens/Percent = %d/%f, want 90000/45.0", cu.InputTokens, cu.Percent)
	}
}
