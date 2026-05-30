package sdk

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// ContextUsagePayload is the payload for EventContextUsage.
// It is dispatched after each completed turn with the current context window usage.
// Compacted is true when history compaction ran during the turn that produced this event.
type ContextUsagePayload struct {
	Usage     ContextUsage `json:"usage"`
	Compacted bool         `json:"compacted"`
}
