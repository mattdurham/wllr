package agent

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// transcript.go implements the canonical session transcript: an append-only,
// never-compacted record of everything an agent saw and did, plus the search
// and bounded rendering used by the recall tool (see recall.go).
//
// Compaction rewrites the model-visible history (a.history) so older turns are
// replaced by a summary. That is what keeps a long session runnable, but it is
// lossy: exact commands, paths, and error text vanish from the model's context.
// The canonical transcript is the other half of the hybrid model — it keeps the
// originals verbatim so compaction only ever changes what the model *sees*, not
// what the session *records*.
//
// Invariants:
//   - Entries are append-only. Nothing removes or rewrites an entry, so a
//     source pointer (entry ID) stays valid for the life of the session.
//   - The transcript is never passed to compactHistory and compaction never
//     mutates it. This is the "compaction does not destroy the canonical
//     transcript" guarantee.
//   - Entry IDs are stable and unique within an agent: a role prefix plus a
//     monotonic sequence ("u1", "a2", "t3", "r4"). The numbering matches the
//     history extension's JSONL IDs, so a recalled entry can be correlated with
//     the persisted session file.
//
// The entry shape is in transcriptentry.go and the query filter set in
// recallquery.go.

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// Transcript is an append-only, never-compacted record of one agent's session.
// It is safe for concurrent use: the agent's turn goroutine appends while the
// recall tool and status surfaces may read.
type Transcript struct {
	entries []TranscriptEntry
	mu      sync.RWMutex
	seq     int
}

// NewTranscript returns an empty canonical transcript.
func NewTranscript() *Transcript {
	return &Transcript{}
}

// RecordMessage appends a user or assistant message and returns its entry ID.
// Empty content is rejected (returns "") to match the history contract: the
// provider API rejects empty text blocks, so an empty message is not a record
// worth keeping.
func (t *Transcript) RecordMessage(role, content string) string {
	if t == nil {
		return ""
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return ""
	}
	prefix := "m"
	switch role {
	case "user":
		prefix = "u"
	case "assistant":
		prefix = "a"
	}
	return t.append(
		TranscriptEntry{Role: role, Kind: TranscriptKindMessage, Content: content},
		prefix,
	)
}

// RecordToolCall appends a tool invocation and returns its entry ID.
// An empty input is still recorded: the call happened, and its absence of
// arguments is itself information.
func (t *Transcript) RecordToolCall(toolName, input string) string {
	if t == nil {
		return ""
	}
	return t.append(
		TranscriptEntry{
			Kind:     TranscriptKindToolCall,
			ToolName: toolName,
			Content:  strings.TrimSpace(input),
		},
		"t",
	)
}

// RecordToolResult appends a tool's output and returns its entry ID. This is
// where command output becomes retrievable: only the input reached the
// persisted session file before, so an exact error message raised by a command
// was unrecoverable once compaction folded the turn away.
func (t *Transcript) RecordToolResult(toolName, output string) string {
	if t == nil {
		return ""
	}
	output = strings.TrimSpace(output)
	if output == "" {
		return ""
	}
	return t.append(
		TranscriptEntry{
			Kind:     TranscriptKindToolResult,
			ToolName: toolName,
			Content:  output,
		},
		"r",
	)
}

// append assigns the sequence number and ID, then stores the entry. The caller
// supplies everything but Seq/ID/Timestamp.
func (t *Transcript) append(e TranscriptEntry, prefix string) string {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.seq++
	e.Seq = t.seq
	e.ID = fmt.Sprintf("%s%d", prefix, t.seq)
	e.Timestamp = time.Now()
	t.entries = append(t.entries, e)
	return e.ID
}

// Len returns the number of entries recorded. Never decreases.
func (t *Transcript) Len() int {
	if t == nil {
		return 0
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.entries)
}

// Snapshot returns a copy of every entry in chronological order.
func (t *Transcript) Snapshot() []TranscriptEntry {
	if t == nil {
		return nil
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	out := make([]TranscriptEntry, len(t.entries))
	copy(out, t.entries)
	return out
}

// Search returns the entries matching q, in chronological order, capped by
// q.Limit. It also reports the total number of matches before the cap so a
// caller can tell the model that a query was too broad.
func (t *Transcript) Search(q RecallQuery) (matches []TranscriptEntry, total int) {
	if t == nil {
		return nil, 0
	}
	limit := q.Limit
	if limit == 0 {
		limit = DefaultRecallLimit
	}
	needle := strings.ToLower(q.Text)
	tool := strings.ToLower(q.Tool)
	path := strings.ToLower(q.Path)

	t.mu.RLock()
	defer t.mu.RUnlock()
	for _, e := range t.entries {
		if q.From > 0 && e.Seq < q.From {
			continue
		}
		if q.To > 0 && e.Seq > q.To {
			continue
		}
		if tool != "" && strings.ToLower(e.ToolName) != tool {
			continue
		}
		if needle != "" || path != "" {
			content := strings.ToLower(e.Content)
			if needle != "" && !strings.Contains(content, needle) {
				continue
			}
			if path != "" && !strings.Contains(content, path) {
				continue
			}
		}
		total++
		if limit < 0 || len(matches) < limit {
			matches = append(matches, e)
		}
	}
	return matches, total
}

// recallNotice is prepended to every recall result. The model must know the
// material is the exact historical record and predates whatever summary it is
// currently holding, otherwise it may treat recalled detail as contradicting
// its context instead of supplementing it (issue #42).
const recallNotice = "Content below is retrieved from the canonical session " +
	"transcript. It is the exact pre-compaction record — commands, paths, and " +
	"output as they were. It may contain detail that the summary in your " +
	"context omitted, and it takes precedence over that summary."

// recallNoMatches is returned when a query matches nothing. It suggests the
// recovery the model most likely needs rather than leaving it to conclude the
// material was never recorded.
const recallNoMatches = "No transcript entries matched. Try a shorter or " +
	"different search term, or drop the tool/path filters."

// recallFooterReserve is the space held back from the content budget for the
// trailing summary line. The footer is written after the content, so without a
// reservation a full content budget would push the total past maxChars and the
// final clamp would clip the very hint that tells the model to narrow its query.
const recallFooterReserve = 200

// RenderRecall formats matched entries as bounded plain text for the model.
// Output never exceeds budgetTokens (using the chars/4 heuristic the compaction
// code already uses), so recall cannot overflow the model's context window.
// A non-positive budget applies DefaultRecallTokenBudget.
//
// total is the pre-cap match count from Search. When more matched than fit, the
// result says so and tells the model to narrow the query, rather than silently
// presenting a partial answer as if it were complete.
func RenderRecall(entries []TranscriptEntry, total int, budgetTokens int64) string {
	if budgetTokens <= 0 {
		budgetTokens = DefaultRecallTokenBudget
	}
	maxChars := int(budgetTokens) * 4

	var head strings.Builder
	head.WriteString(recallNotice)
	head.WriteString("\n\n")
	if len(entries) == 0 {
		head.WriteString(recallNoMatches)
		return truncateToChars(head.String(), maxChars)
	}
	fmt.Fprintf(
		&head,
		"%s, oldest first:\n",
		pluralEntries(total, "matching entry", "matching entries"),
	)

	contentBudget := maxChars - head.Len() - recallFooterReserve
	if contentBudget < 1 {
		// Pathologically small budget: the notice alone fills it, so return
		// what fits rather than overrunning the cap.
		return truncateToChars(strings.TrimRight(head.String(), "\n"), maxChars)
	}

	var content strings.Builder
	shown := 0
	for _, e := range entries {
		block := renderRecallEntry(e)
		if content.Len()+len(block) > contentBudget {
			// Always emit at least one entry, truncated, so a single large
			// record stays retrievable instead of yielding an empty result.
			if shown == 0 {
				content.WriteString(truncateToChars(block, contentBudget))
				shown++
			}
			break
		}
		content.WriteString(block)
		shown++
	}

	footer := fmt.Sprintf(
		"\n[recall: %s shown within a ~%d token budget]",
		pluralEntries(shown, "entry", "entries"),
		budgetTokens,
	)
	if total > shown {
		footer = fmt.Sprintf(
			"\n[recall: %s shown within a ~%d token budget; %d more matched — "+
				"narrow the query or raise limit]",
			pluralEntries(shown, "entry", "entries"), budgetTokens, total-shown,
		)
	}

	// head + content fit within maxChars - recallFooterReserve by construction,
	// so this clamp only fires if the footer ever outgrows the reserve.
	return truncateToChars(head.String()+content.String()+footer, maxChars)
}

// pluralEntries renders n with the singular or plural noun.
func pluralEntries(n int, singular, plural string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", singular)
	}
	return fmt.Sprintf("%d %s", n, plural)
}

// renderRecallEntry formats one entry with its source pointer and a stable
// header line the model can cite.
func renderRecallEntry(e TranscriptEntry) string {
	var b strings.Builder
	b.WriteString("\n[")
	b.WriteString(e.ID)
	b.WriteString("] ")

	switch e.Kind {
	case TranscriptKindToolCall:
		b.WriteString("tool_call ")
		b.WriteString(e.ToolName)
		fmt.Fprintf(&b, " (seq %d) — input:\n", e.Seq)
	case TranscriptKindToolResult:
		b.WriteString("tool_result ")
		b.WriteString(e.ToolName)
		fmt.Fprintf(&b, " (seq %d) — output:\n", e.Seq)
	default:
		role := e.Role
		if role == "" {
			role = "message"
		}
		b.WriteString(role)
		fmt.Fprintf(&b, " (seq %d):\n", e.Seq)
	}
	b.WriteString(e.Content)
	b.WriteString("\n")
	return b.String()
}

// truncateToChars cuts s to at most maxChars bytes, appending a marker when it
// had to truncate. It returns s unchanged when maxChars is non-positive so a
// caller can never be handed an empty block for a non-empty entry.
func truncateToChars(s string, maxChars int) string {
	if maxChars <= 0 || len(s) <= maxChars {
		return s
	}
	const marker = "…[truncated]"
	if maxChars <= len(marker) {
		return s[:maxChars]
	}
	return s[:maxChars-len(marker)] + marker
}

// DefaultRecallTokenBudget bounds a recall result when the caller has no
// context window to derive a smaller one from. It is deliberately small
// relative to any real context window: recall is a lookup, not a context dump.
const DefaultRecallTokenBudget int64 = 4_000

// recallBudgetForWindow derives the token budget for one recall from the
// agent's resolved context window. The result is a small fraction of the window
// so recall can never crowd out the conversation it is meant to support, with a
// floor so a tiny window still returns something useful.
//
// A non-positive window falls back to DefaultRecallTokenBudget: an unresolved
// window is not a license to return unbounded text.
func recallBudgetForWindow(contextWindow int64) int64 {
	if contextWindow <= 0 {
		return DefaultRecallTokenBudget
	}
	budget := contextWindow / 20
	if budget > DefaultRecallTokenBudget {
		budget = DefaultRecallTokenBudget
	}
	if budget < 500 {
		budget = 500
	}
	return budget
}
