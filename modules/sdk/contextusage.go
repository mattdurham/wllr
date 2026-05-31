package sdk

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import "charm.land/fantasy"

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
	return ContextUsage{
		InputTokens:   u.InputTokens,
		OutputTokens:  u.OutputTokens,
		ContextWindow: window,
		Percent:       pct,
	}
}
