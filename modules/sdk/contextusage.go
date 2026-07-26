package sdk

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"log/slog"

	"charm.land/fantasy"
)

// ContextUsage describes how much of the model's context window is consumed.
// Percent is InputTokens / ContextWindow * 100; it is 0 when ContextWindow is zero
// to avoid division by zero on the first turn before the window is configured.
type ContextUsage struct {
	InputTokens   int64   `json:"input_tokens"`
	OutputTokens  int64   `json:"output_tokens"`
	ContextWindow int64   `json:"context_window"`
	Percent       float64 `json:"percent"`
}

// ContextUsageFromFantasy constructs a ContextUsage from a fantasy.Usage and the
// model's context window size. Percent is computed safely: a zero window yields
// Percent == 0 rather than a divide-by-zero panic.
func ContextUsageFromFantasy(u fantasy.Usage, window int64) ContextUsage {
	var pct float64
	if window > 0 {
		pct = float64(u.InputTokens) / float64(window) * 100.0
	}

	// Clamp InputTokens to prevent negative percentages due to int64 overflow
	inputTokens := u.InputTokens
	if inputTokens < 0 {
		slog.Warn("negative InputTokens detected, clamping to 0",
			"input_tokens", inputTokens,
			"output_tokens", u.OutputTokens,
			"context_window", window)
		inputTokens = 0
	}

	// Clamp Percent to reasonable range (0-200%) to catch calculation errors
	pctClamped := pct
	if pct < 0 {
		slog.Warn("negative Percent calculated, clamping to 0",
			"input_tokens", inputTokens,
			"context_window", window,
			"calculated_pct", pct)
		pctClamped = 0
	} else if pct > 200 {
		slog.Warn("Percent > 200% calculated, clamping to 200%",
			"input_tokens", inputTokens,
			"context_window", window,
			"calculated_pct", pct)
		pctClamped = 200
	}

	return ContextUsage{
		InputTokens:   inputTokens,
		OutputTokens:  u.OutputTokens,
		ContextWindow: window,
		Percent:       pctClamped,
	}
}
