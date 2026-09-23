package agent

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"charm.land/fantasy"
)

// toolLoopCompactor replaces only the provider-facing messages in a long
// Fantasy tool loop. Fantasy reconstructs the original prompt for each step,
// so cutStep records which completed steps are already included in summary.
type toolLoopCompactor struct {
	lm           fantasy.LanguageModel
	onCompaction func(CompactionResult)
	summary      string
	window       int64
	threshold    int64
	cutStep      int
}

func newToolLoopCompactor(
	lm fantasy.LanguageModel,
	window int64,
	cfg CompactConfig,
	onCompaction func(CompactionResult),
) *toolLoopCompactor {
	pct := 0.85 // hard safety margin even when the usage trigger is disabled
	if cfg.Enabled && cfg.ThresholdPct > 0 && cfg.ThresholdPct < pct {
		pct = cfg.ThresholdPct
	}
	threshold := int64(float64(window) * pct)
	if reserve := window - reserveTokens; reserve > 0 && reserve < threshold {
		threshold = reserve
	}
	return &toolLoopCompactor{lm: lm, window: window, threshold: threshold, onCompaction: onCompaction}
}

func (c *toolLoopCompactor) prepare(
	ctx context.Context,
	opts fantasy.PrepareStepFunctionOptions,
) (context.Context, fantasy.PrepareStepResult, error) {
	if len(opts.Steps) == 0 || c.window <= 0 {
		return ctx, fantasy.PrepareStepResult{}, nil
	}

	messages := opts.Messages
	if c.summary != "" {
		messages = []fantasy.Message{fantasy.NewUserMessage("[Compacted conversation and tool results]\n" + c.summary)}
		for _, step := range opts.Steps[c.cutStep:] {
			messages = append(messages, step.Messages...)
		}
	}

	last := opts.Steps[len(opts.Steps)-1]
	// Provider input usage is the reliable baseline. The most recent step's
	// tool results have not yet been counted in that input, so estimate their
	// additional cost conservatively at two bytes per token.
	predicted := last.Usage.InputTokens + last.Usage.OutputTokens + estimateFantasyMessageBytes(last.Messages)/2
	if last.Usage.InputTokens == 0 {
		// Some compatible endpoints omit usage entirely. Fall back to the
		// current provider-facing message size in that case.
		if estimated := estimateFantasyMessageBytes(messages) / 2; estimated > predicted {
			predicted = estimated
		}
	}
	if predicted < c.threshold {
		if c.summary != "" {
			return ctx, fantasy.PrepareStepResult{Messages: messages}, nil
		}
		return ctx, fantasy.PrepareStepResult{}, nil
	}

	start := time.Now()
	summary, usage, err := summarizeToolLoop(ctx, c.lm, messages, c.window)
	if err != nil {
		return ctx, fantasy.PrepareStepResult{}, fmt.Errorf("tool-loop context compaction failed: %w", err)
	}
	c.summary = summary
	c.cutStep = len(opts.Steps)
	if c.onCompaction != nil {
		c.onCompaction(CompactionResult{
			Summary: summary, Messages: len(messages), Usage: usage,
			Latency: time.Since(start), Trigger: CompactionTriggerToolLoop,
		})
	}
	return ctx, fantasy.PrepareStepResult{Messages: []fantasy.Message{
		fantasy.NewUserMessage("[Compacted conversation and tool results]\n" + summary),
	}}, nil
}

func estimateFantasyMessageBytes(messages []fantasy.Message) int64 {
	var size int64
	for _, message := range messages {
		encoded, err := json.Marshal(message)
		if err == nil {
			size += int64(len(encoded))
		}
	}
	return size
}

// summarizeToolLoop bounds the summary request independently of the request
// that just approached the context limit. Keep the initial task and the newest
// messages; earlier tool details are represented by the rolling summary.
func summarizeToolLoop(
	ctx context.Context,
	lm fantasy.LanguageModel,
	messages []fantasy.Message,
	window int64,
) (string, fantasy.Usage, error) {
	maxChars := window * 2
	if maxChars > 80_000 {
		maxChars = 80_000
	}
	if maxChars < 2_000 {
		maxChars = 2_000
	}
	var src strings.Builder
	if len(messages) > 0 {
		appendToolLoopMessage(&src, messages[0], 2_000)
	}
	start := len(messages) - 40
	if start < 1 {
		start = 1
	}
	if start > 1 {
		fmt.Fprintf(&src, "\n[%d earlier messages omitted]\n", start-1)
	}
	for _, message := range messages[start:] {
		remaining := maxChars - int64(src.Len())
		if remaining <= 0 {
			break
		}
		limit := int64(2_000)
		if remaining < limit {
			limit = remaining
		}
		appendToolLoopMessage(&src, message, int(limit))
	}

	var summary strings.Builder
	res, err := fantasy.NewAgent(lm).Stream(ctx, fantasy.AgentStreamCall{
		Prompt: src.String() + "\n\n---\n\n" + compactionSummaryPrompt,
		OnTextDelta: func(_, text string) error {
			summary.WriteString(text)
			return nil
		},
	})
	if err != nil {
		return "", fantasy.Usage{}, err
	}
	if summary.Len() == 0 {
		return "", fantasy.Usage{}, fmt.Errorf("empty summary")
	}
	var usage fantasy.Usage
	if res != nil {
		usage = res.TotalUsage
	}
	return summary.String(), usage, nil
}

func appendToolLoopMessage(dst *strings.Builder, message fantasy.Message, limit int) {
	encoded, err := json.Marshal(message)
	if err != nil {
		return
	}
	if len(encoded) > limit {
		half := limit / 2
		encoded = []byte(string(encoded[:half]) + "...[truncated]..." + string(encoded[len(encoded)-half:]))
	}
	dst.Write(encoded)
	dst.WriteString("\n")
}
