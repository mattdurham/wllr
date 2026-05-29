package agent

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"charm.land/fantasy"
	"github.com/mattdurham/wllr/modules/sdk"
)

const (
	// defaultContextWindow is the fallback when no explicit window is configured.
	// Modern models (Claude 4.x, Gemini 2.x) all support >= 1M tokens; set
	// WLLR_CONTEXT_WINDOW to reduce this for older or limited-tier models.
	defaultContextWindow int64 = 1_000_000
	// reserveTokens is kept free for the model's output response.
	reserveTokens int64 = 16_384
	// keepMessages is how many recent messages are kept verbatim after compaction.
	keepMessages = 20
	// defaultKeepRecentTokens is the token budget for recent messages kept verbatim
	// after proactive compaction. Messages beyond this budget (oldest first) are
	// summarized. Uses the chars/4 heuristic for estimation.
	defaultKeepRecentTokens int64 = 20_000
)

// contextWindowForModel returns the context window for a model from the
// generated table, defaulting to 1M for unknown models.
func contextWindowForModel(modelName string) int64 {
	lower := strings.ToLower(modelName)
	// Exact match first.
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
	return defaultContextWindow
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

// shouldCompact returns true when the estimated total context (history +
// system prompt + next message) is close enough to the window limit that
// compaction should run before the next API call.
func shouldCompact(history []sdk.Message, systemPrompt, nextMessage string, contextWindow int64) bool {
	if contextWindow <= 0 {
		contextWindow = defaultContextWindow
	}
	used := estimateTokens(history) +
		estimateStr(systemPrompt) +
		estimateStr(nextMessage)
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

// compactHistory summarizes the oldest messages using the LLM and returns a
// compacted history: one summary message followed by the most recent messages,
// plus the raw summary text for the caller to store as priorSummary.
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
) ([]sdk.Message, string, error) {
	if keepRecentTokens <= 0 {
		keepRecentTokens = defaultKeepRecentTokens
	}

	if len(history) < 2 {
		return history, "", nil
	}

	// Always preserve the first message (original task) outside the summary.
	anchor := history[:1]
	rest := history[1:]

	if len(rest) == 0 {
		return history, "", nil
	}

	// Find the token-budget cut point within rest.
	// 0 = fits, j>0 = cut before j, -1 = no valid boundary (skip).
	cutIdx := findCutPoint(rest, keepRecentTokens)
	if cutIdx == 0 || cutIdx == -1 {
		// Everything fits, or no valid user boundary exists — skip compaction.
		return history, "", nil
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

	// Stream the summary from the model.
	var summary strings.Builder
	fa := fantasy.NewAgent(lm)
	_, err := fa.Stream(ctx, fantasy.AgentStreamCall{
		Prompt: src.String() + "\n\n---\n\n" + compactionSummaryPrompt,
		OnTextDelta: func(_, text string) error {
			summary.WriteString(text)
			return nil
		},
	})
	if err != nil {
		return history, "", fmt.Errorf("compaction: summarize: %w", err)
	}
	if summary.Len() == 0 {
		return history, "", fmt.Errorf("compaction: empty summary")
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
	return append(anchor, compacted...), summaryText, nil
}
