package sdk

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// ContextUsagePayload is the payload for EventContextUsage.
// It is dispatched after each completed turn with the current context window usage.
// Compacted is true when history compaction ran during the turn that produced this event.
// Compactions is the cumulative number of successful compactions for the agent this
// session (additive field; zero when compaction has never run).
// ThresholdPct is the compaction trigger threshold (0.80 default) used to compute remaining-to-threshold.
type ContextUsagePayload struct {
	Usage        ContextUsage `json:"usage"`
	Compacted    bool         `json:"compacted"`
	Compactions  int          `json:"compactions,omitempty"`
	ThresholdPct float64      `json:"threshold_pct,omitempty"`
}
