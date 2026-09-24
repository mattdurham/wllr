package agent

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// recallquery.go defines the filter set for canonical-transcript retrieval.
// The Transcript that executes a query lives in transcript.go.

// RecallQuery selects entries from a canonical transcript. Every non-zero
// field is an additional AND filter; an empty query matches every entry.
type RecallQuery struct {
	// Text matches entries whose content contains this substring,
	// case-insensitively. This is the primary retrieval mode.
	Text string
	// Tool matches entries whose ToolName equals this value,
	// case-insensitively. Restricts results to one tool's calls/results.
	Tool string
	// Path matches entries whose content contains this substring,
	// case-insensitively. Used to find material touching a file path.
	Path string
	// From and To bound the sequence range, inclusive. Zero means unbounded on
	// that side. Lets the model pull a contiguous span it already knows about.
	From int
	To   int
	// Limit caps the number of returned entries. Zero applies
	// DefaultRecallLimit. A negative value means no limit, which the caller
	// should pair with a token budget (see RenderRecall).
	Limit int
}

// DefaultRecallLimit bounds how many entries a single recall returns when the
// caller does not set Limit. The token budget is the real bound; this is a
// cheap upper bound that keeps a broad query from scanning the whole
// transcript into memory.
const DefaultRecallLimit = 200
