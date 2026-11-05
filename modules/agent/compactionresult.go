package agent

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"time"

	"charm.land/fantasy"
	"github.com/mattdurham/wllr/modules/sdk"
)

// CompactionResult is the outcome of a compactHistory run. On no-op
// (history fits the budget, or no valid user boundary) Summary and Usage are
// zero-valued and History is the input unchanged; callers must treat
// Summary == "" as "compaction did not happen" and must not increment
// compaction counters or emit compaction log records for it.
type CompactionResult struct {
	// Summary is the raw summary text (empty on no-op or failure).
	Summary string
	// Trigger is the compaction trigger kind (see CompactionTrigger*) that
	// caused this run. Set for every result so callers can log it without
	// re-deriving it.
	Trigger string
	// Method is CompactionMethodSummary for an LLM summary or
	// CompactionMethodTruncate for the deterministic fallback. Empty on a
	// no-op.
	Method string
	// History is the post-compaction history (input unchanged on no-op or
	// failure).
	History []sdk.Message
	// Usage is the token cost of the summarization call (zero on no-op or
	// failure).
	Usage fantasy.Usage
	// Messages is the number of history messages folded into the summary
	// (zero on no-op or failure).
	Messages int
	// Latency is the wall-clock duration of the summarization call (zero on
	// no-op or failure).
	Latency time.Duration
	// EstimatedTokens is the estimate of the post-compaction context (system
	// prompt + tool definitions + the compacted history), used to re-base the
	// agent's carried usage baseline so the ctx indicator and the trigger
	// reflect the shrunken context instead of the pre-compaction peak. Zero
	// means unknown.
	EstimatedTokens int64
	// TokensBefore and TokensAfter size the request before and after the run
	// (including the system prompt, next message, and tool definitions). They
	// make the reduction observable even when the summary result is truncated.
	TokensBefore int64
	TokensAfter  int64
}

// carriesSummary reports whether Summary is a conversation summary that the
// agent should carry into the next compaction as priorSummary, rather than a
// deterministic truncation notice.
func (r CompactionResult) carriesSummary() bool {
	return r.Summary != "" && r.Method != CompactionMethodTruncate
}
