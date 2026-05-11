package agent

import (
	"context"
	"fmt"
	"strings"

	"charm.land/fantasy"
	"github.com/mattdurham/wllr/sdk"
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
	// "claude-sonnet-4-6-20250514").
	for id, w := range modelContextWindows {
		if strings.Contains(lower, id) || strings.Contains(id, lower) {
			return w
		}
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

// compactionSummaryPrompt asks the model to produce a structured summary
// of the conversation history provided.  Matches pi's format so skills that
// reference this format work correctly.
const compactionSummaryPrompt = `Summarize the conversation history above into a structured context summary. Use this EXACT format:

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
// compacted history: one summary message followed by the most recent messages.
// If summarisation fails, returns the original history unchanged.
//
// The first message is always kept verbatim — it contains the user's original
// task and must not be lost to summarization.
func compactHistory(ctx context.Context, lm fantasy.LanguageModel, history []sdk.Message) ([]sdk.Message, error) {
	if len(history) <= keepMessages {
		return history, nil
	}

	// Always preserve the first message (original task) outside the summary.
	anchor := history[:1]
	rest := history[1:]

	if len(rest) <= keepMessages {
		return history, nil
	}

	toSummarize := rest[:len(rest)-keepMessages]
	toKeep := rest[len(rest)-keepMessages:]

	// Build a compact representation of the messages to summarize.
	var src strings.Builder
	for _, m := range toSummarize {
		src.WriteString(string(m.Role))
		src.WriteString(": ")
		content := m.Content
		if len([]rune(content)) > 2000 {
			content = string([]rune(content)[:2000]) + "…[truncated]"
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
		return history, fmt.Errorf("compaction: summarize: %w", err)
	}
	if summary.Len() == 0 {
		return history, fmt.Errorf("compaction: empty summary")
	}

	// Replace the old messages with a single summary entry, keeping the
	// original first message (the user's task) at the front.
	summaryMsg := sdk.Message{
		Role:    sdk.RoleUser,
		Content: "[Previous conversation summary — " + fmt.Sprintf("%d messages compacted", len(toSummarize)) + "]\n\n" + summary.String(),
	}
	compacted := append([]sdk.Message{summaryMsg}, toKeep...)
	return append(anchor, compacted...), nil
}
