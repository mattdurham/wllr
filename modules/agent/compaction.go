package agent

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"charm.land/fantasy"
	"github.com/mattdurham/wllr/modules/sdk"
)

// Compaction trigger identifiers. They are surfaced in the structured
// compaction log so operators can tell heuristic (token-estimate) compaction
// apart from usage-threshold and reactive (provider context-limit) compaction.
const (
	// CompactionTriggerProactive is set when the preflight token-estimate
	// check (shouldCompactWithTools) decided compaction was needed.
	CompactionTriggerProactive = "proactive"
	// CompactionTriggerUsage is set when the provider-reported usage crossed
	// the percentage threshold (shouldCompactByUsage).
	CompactionTriggerUsage = "usage_threshold"
	// CompactionTriggerReactive is set when the provider rejected the request
	// as context-too-long and the turn is compacted and retried.
	CompactionTriggerReactive = "reactive"
)

// builtInContextWindows covers model families whose limits are known without
// querying a provider. Provider- or endpoint-specific values are recorded on
// the pool and take precedence over this table.
var builtInContextWindows = map[string]int64{
	"claude-opus-4-8": 1_000_000, "claude-opus-4-7": 1_000_000,
	"claude-opus-4-6": 200_000, "claude-opus-4-5-20251101": 200_000,
	"claude-sonnet-5": 1_000_000, "claude-sonnet-4-6": 200_000,
	"claude-sonnet-4-5-20250929": 200_000, "claude-haiku-4-5-20251001": 200_000,
	"claude-opus-4-1-20250805": 200_000, "claude-opus-4-20250514": 200_000,
	"claude-sonnet-4-20250514": 200_000,
	"gpt-5.5":                  1_050_000, "gpt-5.5-pro": 1_050_000, "gpt-5.4": 1_050_000,
	"gpt-5.4-pro": 1_050_000, "gpt-5.4-mini": 400_000, "gpt-5.4-nano": 400_000,
	"gpt-5.3-codex": 400_000, "gpt-5.2": 400_000, "gpt-5.2-codex": 400_000,
	"gpt-5.1": 400_000, "gpt-5.1-codex": 400_000, "gpt-5.1-codex-max": 400_000,
	"gpt-5.1-codex-mini": 400_000, "gpt-5-codex": 400_000, "gpt-5": 400_000,
	"gpt-5-mini": 400_000, "gpt-5-nano": 400_000, "o4-mini": 200_000,
	"o3": 200_000, "gpt-4.1": 1_047_576, "gpt-4.1-mini": 1_047_576,
	"gpt-4.1-nano": 1_047_576, "o3-mini": 200_000, "gpt-4o": 128_000,
	"gpt-4o-mini": 128_000,
}

// CompactConfig controls the percentage-based compaction trigger.
// When Enabled is true and ThresholdPct > 0, shouldCompactByUsage is consulted
// before the heuristic shouldCompact. ThresholdPct is expressed as a fraction
// (0.80 = 80%); it is set from WLLR_COMPACT_THRESHOLD at pool creation.
type CompactConfig struct {
	// Enabled is true by default; set to false to disable the percentage trigger.
	Enabled bool
	// ThresholdPct is the fraction of the context window at which compaction fires.
	// Default is 0.80 (80%). Must be in (0, 1] for the trigger to be active.
	ThresholdPct float64
}

// shouldCompactByUsage returns true when the last real usage exceeds thresholdPct
// of the context window. Returns false when usage or window is zero (caller falls
// back to the heuristic shouldCompact for the first turn or when unconfigured).
//
// Note: thresholdPct is a fraction in (0, 1] (e.g. 0.80 = 80%). This is a different
// scale from ContextUsage.Percent, which is a percentage in [0, 100]. Do not compare
// ContextUsage.Percent directly to thresholdPct without converting.
func shouldCompactByUsage(lastUsage fantasy.Usage, contextWindow int64, thresholdPct float64) bool {
	if lastUsage.InputTokens == 0 || contextWindow == 0 || thresholdPct <= 0 {
		return false
	}
	pct := float64(lastUsage.InputTokens) / float64(contextWindow)
	return pct >= thresholdPct
}

const (
	// defaultContextWindow is retained for legacy helper callers and tests. The
	// agent turn path must resolve a positive per-model value before streaming.
	defaultContextWindow int64 = 1_000_000
	// reserveTokens is kept free for the model's output response.
	reserveTokens int64 = 16_384
	// keepMessages is retained for tests that construct the historical default
	// message count; production compaction uses a token budget.
	keepMessages = 20
	// defaultKeepRecentTokens is the token budget for recent messages kept verbatim
	// after proactive compaction. Messages beyond this budget (oldest first) are
	// summarized. Uses the chars/4 heuristic for estimation.
	defaultKeepRecentTokens int64 = 20_000
)

// contextWindowForModel returns the context window for a model from the
// generated table, or 0 when metadata is unavailable.
func contextWindowForModel(modelName string) int64 {
	lower := strings.ToLower(modelName)
	// Exact match first.
	if w, ok := builtInContextWindows[lower]; ok {
		return w
	}
	var builtInKey string
	var builtInWindow int64
	for id, w := range builtInContextWindows {
		if strings.Contains(lower, id) && len(id) > len(builtInKey) {
			builtInKey, builtInWindow = id, w
		}
	}
	if builtInKey != "" {
		return builtInWindow
	}
	if w, ok := modelContextWindows[lower]; ok {
		return w
	}
	// Substring match for versioned aliases (e.g. "claude-sonnet-4-6" matches
	// "claude-sonnet-4-6-20250514"). Map iteration order is non-deterministic, so
	// we prefer the longest (most specific) matching key to ensure stable results
	// when multiple entries match the same model name string.
	var bestKey string
	var bestW int64
	for id, w := range modelContextWindows {
		if strings.Contains(lower, id) || strings.Contains(id, lower) {
			if len(id) > len(bestKey) {
				bestKey = id
				bestW = w
			}
		}
	}
	if bestKey != "" {
		return bestW
	}
	return 0
}

// estimateTokens estimates token count using the chars/4 heuristic.
// Intentionally overestimates to stay safely under limits.
func estimateTokens(msgs []sdk.Message) int64 {
	var chars int64
	for _, m := range msgs {
		chars += int64(len(m.Content))
	}
	return chars / 4
}

// estimateStr estimates token count for a single string using the chars/4 heuristic.
func estimateStr(s string) int64 { return int64(len(s)) / 4 }

// estimateToolTokens estimates the tokens occupied by the tool definitions
// sent with a provider request. Tool schemas can be a substantial part of the
// context, especially when many extensions are enabled.
func estimateToolTokens(tools []fantasy.AgentTool) int64 {
	var chars int64
	for _, tool := range tools {
		raw, err := json.Marshal(tool.Info())
		if err != nil {
			continue
		}
		chars += int64(len(raw))
	}
	return chars / 4
}

// shouldCompact returns true when the estimated total context (history +
// system prompt + next message) is close enough to the window limit that
// compaction should run before the next API call.
func shouldCompact(history []sdk.Message, systemPrompt, nextMessage string, contextWindow int64) bool {
	return shouldCompactWithTools(history, systemPrompt, nextMessage, nil, contextWindow)
}

// shouldCompactWithTools is the preflight compaction check used before a
// provider request. It includes tool definitions because providers count them
// against the same context window as messages and system instructions.
func shouldCompactWithTools(
	history []sdk.Message,
	systemPrompt, nextMessage string,
	tools []fantasy.AgentTool,
	contextWindow int64,
) bool {
	if contextWindow <= 0 {
		contextWindow = defaultContextWindow
	}
	used := estimateTokens(history) +
		estimateStr(systemPrompt) +
		estimateStr(nextMessage) +
		estimateToolTokens(tools)
	return used > contextWindow-reserveTokens
}

// findCutPoint returns the index into history such that history[idx:] fits within
// keepTokens (using the chars/4 heuristic). The cut always lands on a RoleUser
// message so the kept slice begins a valid turn. Returns:
//   - 0: everything fits within the budget (no cut needed)
//   - j>0: cut before index j, which is guaranteed to be a RoleUser message
//   - -1: budget is exceeded but no valid user boundary exists (skip compaction)
func findCutPoint(history []sdk.Message, keepTokens int64) int {
	if keepTokens <= 0 {
		keepTokens = defaultKeepRecentTokens
	}
	var acc int64
	for i := len(history) - 1; i >= 0; i-- {
		acc += int64(len(history[i].Content)) / 4
		if acc > keepTokens {
			// Budget exhausted at i. Snap forward to the nearest user message,
			// starting at i itself (the bust point may be a user message).
			for j := i; j < len(history); j++ {
				if history[j].Role == sdk.RoleUser {
					return j // j==0 means cut at the very start (valid, distinct from "fits")
				}
			}
			// No user boundary found — caller must skip compaction.
			return -1
		}
	}
	// Everything fits in budget.
	return 0
}

// filePathRe matches absolute Unix paths. Designed to be conservative:
// false positives in the file list are harmless; false negatives lose a path.
var filePathRe = regexp.MustCompile(`(?:^|[\s"])(/[^\s"'<>]+\.[a-zA-Z0-9]+)`)

// extractFilePaths scans msgs for absolute path-like strings and returns a
// deduplicated, sorted slice. Only absolute paths (starting with /) are matched.
func extractFilePaths(msgs []sdk.Message) []string {
	seen := make(map[string]bool)
	for _, m := range msgs {
		matches := filePathRe.FindAllStringSubmatch(m.Content, -1)
		for _, match := range matches {
			if len(match) >= 2 {
				seen[match[1]] = true
			}
		}
	}
	if len(seen) == 0 {
		return nil
	}
	paths := make([]string, 0, len(seen))
	for p := range seen {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	return paths
}

// compactionSummaryPrompt asks the model to produce a structured summary
// of the conversation history provided.  Matches pi's format so skills that
// reference this format work correctly.
const compactionSummaryPrompt = `Summarize the new conversation messages above (after the previous summary, if any) into a structured context summary. Use this EXACT format:

## Goal
[What we're trying to accomplish]

## Progress
### Done
- [x] [Completed items — be specific]

### In Progress
- [ ] [Current work]

## Key Decisions
- **[Decision]**: [Brief rationale]

## Next Steps
1. [Ordered list of what should happen next]

## Critical Context
- [Any data, exact file paths, function names, or error messages needed to continue]
- (none) if not applicable

Keep each section concise. Preserve exact file paths, function names, and error messages verbatim.`

// CompactionResult is the outcome of a compactHistory run. On no-op
// (history fits the budget, or no valid user boundary) Summary and Usage are
// zero-valued and History is the input unchanged; callers must treat
// Summary == "" as "compaction did not happen" and must not increment
// compaction counters or emit compaction log records for it.
type CompactionResult struct {
	// History is the post-compaction history (input unchanged on no-op or
	// failure).
	History []sdk.Message
	// Summary is the raw summary text (empty on no-op or failure).
	Summary string
	// Messages is the number of history messages folded into the summary
	// (zero on no-op or failure).
	Messages int
	// Usage is the token cost of the summarization call (zero on no-op or
	// failure).
	Usage fantasy.Usage
	// Latency is the wall-clock duration of the summarization call (zero on
	// no-op or failure).
	Latency time.Duration
	// Trigger is the compaction trigger kind (see CompactionTrigger*) that
	// caused this run. Set for every result so callers can log it without
	// re-deriving it.
	Trigger string
}

// compactHistory summarizes the oldest messages using the LLM and returns the
// run outcome: compacted history, raw summary text for the caller to store as
// priorSummary, the number of messages folded into the summary, and the token
// usage of the summarization call itself.
//
// trigger identifies the trigger kind (see CompactionTrigger*); it is carried
// in the return value so callers can log it without re-deriving it.
//
// keepRecentTokens controls how many recent tokens (chars/4 heuristic) are
// kept verbatim. Pass 0 to use defaultKeepRecentTokens (20,000).
//
// priorSummary, when non-empty, is prepended to the compaction prompt so the
// model can build an incremental summary instead of starting from scratch.
//
// If summarisation fails, returns the original history unchanged with an empty
// summary string.
//
// The first message is always kept verbatim (user's original task anchor).
func compactHistory(
	ctx context.Context,
	lm fantasy.LanguageModel,
	history []sdk.Message,
	priorSummary string,
	keepRecentTokens int64,
	trigger string,
) (CompactionResult, error) {
	if keepRecentTokens <= 0 {
		keepRecentTokens = defaultKeepRecentTokens
	}

	if len(history) < 2 {
		return noOpCompaction(history, trigger), nil
	}

	// Always preserve the first message (original task) outside the summary.
	anchor := history[:1]
	rest := history[1:]

	if len(rest) == 0 {
		return noOpCompaction(history, trigger), nil
	}

	// Find the token-budget cut point within rest.
	// 0 = fits, j>0 = cut before j, -1 = no valid boundary (skip).
	cutIdx := findCutPoint(rest, keepRecentTokens)
	if cutIdx == 0 || cutIdx == -1 {
		// Everything fits, or no valid user boundary exists — skip compaction.
		return noOpCompaction(history, trigger), nil
	}

	toSummarize := rest[:cutIdx]
	toKeep := rest[cutIdx:]

	// Build a compact representation of the messages to summarize.
	var src strings.Builder

	// Prepend prior summary context when available (iterative compaction).
	if priorSummary != "" {
		src.WriteString("[Prior summary from previous compaction]\n")
		src.WriteString(priorSummary)
		src.WriteString("\n---\nUpdate the above summary to include the new messages below.\n\n")
	}

	for _, m := range toSummarize {
		src.WriteString(string(m.Role))
		src.WriteString(": ")
		content := m.Content
		if runes := []rune(content); len(runes) > 2000 {
			content = string(runes[:2000]) + "…[truncated]"
		}
		src.WriteString(content)
		src.WriteString("\n\n")
	}

	// Stream the summary from the model. The returned result carries the
	// summarization call's token usage, so its cost is observable instead of
	// silently folded into the session total.
	var summary strings.Builder
	fa := fantasy.NewAgent(lm)
	streamStart := time.Now()
	res, err := fa.Stream(ctx, fantasy.AgentStreamCall{
		Prompt: src.String() + "\n\n---\n\n" + compactionSummaryPrompt,
		OnTextDelta: func(_, text string) error {
			summary.WriteString(text)
			return nil
		},
	})
	if err != nil {
		return noOpCompaction(history, trigger), fmt.Errorf("compaction: summarize: %w", err)
	}
	if summary.Len() == 0 {
		return noOpCompaction(history, trigger), fmt.Errorf("compaction: empty summary")
	}

	var usage fantasy.Usage
	if res != nil {
		usage = res.TotalUsage
	}

	summaryText := summary.String()

	// Build summary message content: header + LLM output + optional file list.
	var msgContent strings.Builder
	fmt.Fprintf(&msgContent, "[Previous conversation summary — %d messages compacted]\n\n", len(toSummarize))
	msgContent.WriteString(summaryText)

	// Append file paths touched in the compacted span if any are detected.
	filePaths := extractFilePaths(toSummarize)
	if len(filePaths) > 0 {
		msgContent.WriteString("\n\n### Files referenced in compacted span\n")
		for _, fp := range filePaths {
			msgContent.WriteString("- ")
			msgContent.WriteString(fp)
			msgContent.WriteString("\n")
		}
	}

	summaryMsg := sdk.Message{
		Role:    sdk.RoleUser,
		Content: msgContent.String(),
	}
	compacted := append([]sdk.Message{summaryMsg}, toKeep...)
	return CompactionResult{
		History:  append(anchor, compacted...),
		Summary:  summaryText,
		Messages: len(toSummarize),
		Usage:    usage,
		Latency:  time.Since(streamStart),
		Trigger:  trigger,
	}, nil
}

// noOpCompaction builds a CompactionResult for runs that did not compact
// (history fits, no valid boundary, or summarization failure). Summary is
// empty, which callers treat as "compaction did not happen".
func noOpCompaction(history []sdk.Message, trigger string) CompactionResult {
	return CompactionResult{History: history, Trigger: trigger}
}
