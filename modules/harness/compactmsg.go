package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// compactMsg requests an on-demand context compaction of the main agent
// (the /compact command). Handled by running AgentPool.CompactMainAgent and
// surfacing the outcome as a notification.
type compactMsg struct{}
