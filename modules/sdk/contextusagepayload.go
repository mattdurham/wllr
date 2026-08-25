package sdk

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// ContextUsagePayload is the payload for EventContextUsage.
// It is dispatched after each completed turn with the current context window usage.
// Compacted is true when history compaction ran during the turn that produced this event.
// Compactions is the cumulative number of successful compactions for the agent this
// session (additive field; zero when compaction has never run).
// ThresholdPct is the compaction trigger threshold expressed as a fraction of the
// context window (0.80 = 80%); it is 0 when the compaction trigger is disabled
// (WLLR_COMPACT_THRESHOLD=0). Extensions render remaining-to-threshold as
// (thresholdPct*100 - percent), clamped at 0.
type ContextUsagePayload struct {
	Usage        ContextUsage `json:"usage"`
	Compacted    bool         `json:"compacted"`
	Compactions  int          `json:"compactions,omitempty"`
	ThresholdPct float64      `json:"threshold_pct,omitempty"`
}
