package agent

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// CompactionNotice describes one completed compaction for EventContextUsage
// consumers. It accompanies the dispatch sent immediately after a successful
// compaction so the UI can surface a compaction message while the turn is
// still running (the end-of-turn dispatch carries a nil notice).
type CompactionNotice struct {
	// Trigger is the compaction trigger kind (see CompactionTrigger*).
	Trigger string
	// MessagesCompacted is the number of history messages folded into the
	// summary.
	MessagesCompacted int
	// EstimatedInputTokens is the chars/4 estimate of the post-compaction
	// context (history + system prompt + tools) when computable. Zero means
	// unknown — consumers must keep displaying their previous value.
	EstimatedInputTokens int64
}
