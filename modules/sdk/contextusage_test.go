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
