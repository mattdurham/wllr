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
	// CompactionTriggerProactive is set when the hard preflight limit
	// (window minus the output reserve) was crossed with the percentage
	// trigger disabled or unreachable.
	CompactionTriggerProactive = "proactive"
	// CompactionTriggerUsage is set when the next request's estimate crossed
	// the configured percentage threshold (`WLLR_COMPACT_THRESHOLD`).
	CompactionTriggerUsage = "usage_threshold"
	// CompactionTriggerReactive is set when the provider rejected the request
	// as context-too-long and the turn is compacted and retried.
	CompactionTriggerReactive = "reactive"
	// CompactionTriggerToolLoop is set when a long tool loop is summarized
	// before its next provider request.
	CompactionTriggerToolLoop = "tool_loop"
	// CompactionTriggerManual is set when the user runs /compact to fold the
	// current history on demand.
	CompactionTriggerManual = "manual"
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
// When Enabled is true and ThresholdPct > 0, the next request's estimate is
// compared to ThresholdPct of the context window. ThresholdPct is expressed as
// a fraction (0.80 = 80%); it is set from WLLR_COMPACT_THRESHOLD at pool
// creation.
type CompactConfig struct {
	// Enabled is true by default; set to false to disable the percentage trigger.
	Enabled bool
	// ThresholdPct is the fraction of the context window at which compaction fires.
	// Default is 0.80 (80%). Must be in (0, 1] for the trigger to be active.
	ThresholdPct float64
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
	// minKeepRecentTokens floors the recovery-band keep budget so a second
	// compaction pass still has a distinct (smaller) budget to retry with.
	minKeepRecentTokens int64 = 1_000
	// maxSummaryInputTokens caps the summarizer's conversation input. The cap
	// keeps the summary request well inside the model window even when the
	// conversation being folded is much larger.
	maxSummaryInputTokens int64 = 120_000
	// minSummaryInputTokens floors the summarizer input after overflow
	// re-planning. Below this, retrying the same summary is not useful.
	minSummaryInputTokens int64 = 8_000
	// compactionMaxPasses bounds summary attempts (initial + one headroom
	// pass) before deterministic truncation takes over.
	compactionMaxPasses = 2
)

// Compaction methods recorded on CompactionResult. "summary" is the normal LLM
// summarisation path; "truncate" is the deterministic no-model fallback used
// when summarisation fails or cannot create enough headroom.
const (
	CompactionMethodSummary  = "summary"
	CompactionMethodTruncate = "truncate"
)

// compactionBudget is the derived token policy for one compaction decision.
// All values are token counts for the same model window.
type compactionBudget struct {
	// Window is the resolved input window.
	Window int64
	// Trigger is the point at which the next request compacts. It is the
	// configured percentage of the window when enabled, otherwise the hard
	// window-minus-reserve limit.
	Trigger int64
	// Safety is the hard preflight ceiling (window minus the output reserve).
	Safety int64
	// KeepRecent is the recovery-band retention budget for the kept tail.
	KeepRecent int64
	// Recovery is the target headroom after compaction (80% of Trigger).
	Recovery int64
}

// newCompactionBudget derives the trigger, safety, retention, and recovery
// values for a window and configuration.
//
// Retention uses the same recovery-band rule as omp: keep at most
// 0.8*trigger - reserve, so the compacted context lands below the trigger's
// recovery band instead of immediately re-triggering. The historical
// window/10 budget is preserved when it is already smaller (the common case
// for 200k-token windows and below).
func newCompactionBudget(window int64, cfg CompactConfig) compactionBudget {
	if window <= 0 {
		window = defaultContextWindow
	}
	reserve := reserveTokens
	if quarter := window / 4; quarter > 0 && reserve > quarter {
		reserve = quarter
	}
	safety := window - reserve
	if safety <= 0 {
		safety = window / 2
	}
	trigger := safety
	if cfg.Enabled && cfg.ThresholdPct > 0 {
		if pctTrigger := int64(float64(window) * cfg.ThresholdPct); pctTrigger > 0 && pctTrigger < trigger {
			trigger = pctTrigger
		}
	}

	keep := window / 10
	if keep <= 0 {
		keep = defaultKeepRecentTokens
	}
	band := int64(float64(trigger)*0.8) - reserve
	if band < minKeepRecentTokens {
		band = trigger / 2
	}
	if keep > band {
		keep = band
	}
	if keep < minKeepRecentTokens {
		keep = minKeepRecentTokens
	}
	if quarter := window / 4; quarter > 0 && keep > quarter {
		keep = quarter
	}
	if keep < 1 {
		keep = 1
	}

	recovery := int64(float64(trigger) * 0.8)
	if recovery <= 0 {
		recovery = trigger
	}
	return compactionBudget{
		Window:     window,
		Trigger:    trigger,
		Safety:     safety,
		KeepRecent: keep,
		Recovery:   recovery,
	}
}

// shouldCompactByBudget reports whether the next request's estimated size has
// reached the compaction trigger. The caller passes contextEstimate (history +
// system prompt + next message + tool definitions), not the previous turn's
// provider usage: provider usage can include in-turn tool traffic that wllr
// never replays, so it overstates the next request for this history model.
func shouldCompactByBudget(estimate int64, b compactionBudget) bool {
	return estimate > 0 && estimate >= b.Trigger
}

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

// estimateTokens estimates token count for a message slice using the chars/4
// heuristic (ASCII) plus one token per non-ASCII rune. The non-ASCII rule is
// deliberately conservative: UTF-8 bytes/4 undercounts CJK and other
// multibyte text by roughly 2x, which is the dangerous direction for a
// compaction trigger.
func estimateTokens(msgs []sdk.Message) int64 {
	var tokens int64
	for _, m := range msgs {
		tokens += estimateStr(m.Content)
	}
	return tokens
}

// estimateStr estimates token count for a single string. ASCII bytes count as
// 4 characters per token (floored); every non-ASCII rune counts as one token.
// The non-ASCII rule intentionally errs high so a compaction preflight fires
// before the provider rejects the request; the ASCII floor preserves the
// historical estimator's small undercount.
func estimateStr(s string) int64 {
	if s == "" {
		return 0
	}
	var ascii, wide int64
	for _, r := range s {
		if r > 0x7f {
			wide++
		} else {
			ascii++
		}
	}
	return ascii/4 + wide
}

// estimateToolTokens estimates the tokens occupied by the tool definitions
// sent with a provider request. Tool schemas can be a substantial part of the
// context, especially when many extensions are enabled.
func estimateToolTokens(tools []fantasy.AgentTool) int64 {
	var tokens int64
	for _, tool := range tools {
		raw, err := json.Marshal(tool.Info())
		if err != nil {
			continue
		}
		tokens += estimateStr(string(raw))
	}
	return tokens
}

// contextEstimate returns the chars/4 estimate of the next provider request's
// size: conversation history plus system prompt plus the new content plus the
// serialized tool definitions. It is the quantity the preflight budget check
// uses, dispatchCompactionNotice reports after a compaction, and executeTurn
// seeds the display baseline with before the first provider-reported usage
// exists.
func contextEstimate(history []sdk.Message, sysPrompt, content string, tools []fantasy.AgentTool) int64 {
	return estimateTokens(history) + estimateStr(sysPrompt) + estimateStr(content) + estimateToolTokens(tools)
}

// findCutPoint returns the index into history such that history[idx:] fits
// within keepTokens and starts at a RoleUser message. ok is false when there
// is no useful cut: everything already fits, or the overflow point has no
// user boundary after it (compaction must not produce a kept slice that starts
// with an assistant message). idx is then 0.
func findCutPoint(history []sdk.Message, keepTokens int64) (idx int, ok bool) {
	if keepTokens <= 0 {
		keepTokens = defaultKeepRecentTokens
	}
	var acc int64
	for i := len(history) - 1; i >= 0; i-- {
		acc += estimateStr(history[i].Content)
		if acc > keepTokens {
			// Budget exhausted at i. Snap forward to the nearest user message,
			// starting at i itself (the bust point may be a user message).
			for j := i; j < len(history); j++ {
				if history[j].Role == sdk.RoleUser {
					return j, true
				}
			}
			// No user boundary after the overflow point: nothing foldable.
			return 0, false
		}
	}
	// Everything fits in budget.
	return 0, false
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

// compactionRequest is the input to one summarization pass.
type compactionRequest struct {
	PriorSummary     string
	Trigger          string
	History          []sdk.Message
	KeepRecentTokens int64
	Window           int64
	// Force folds history even when the kept tail already fits the budget.
	// Automatic triggers that already decided to compact and manual /compact
	// set it; it is what lets a caller fold the oldest turn of a short history.
	Force bool
}

// compactHistory summarizes the oldest messages using the LLM and returns the
// run outcome: compacted history, raw summary text for the caller to store as
// priorSummary, the number of messages folded into the summary, and the token
// usage of the summarization call itself.
//
// trigger identifies the trigger kind (see CompactionTrigger*); it is carried
// in the return value so callers can log it without re-deriving it.
//
// keepRecentTokens controls how many recent tokens (estimateStr heuristic) are
// kept verbatim. Pass 0 to use defaultKeepRecentTokens (20,000).
//
// priorSummary, when non-empty, is prepended to the compaction prompt so the
// model can build an incremental summary instead of starting from scratch.
// The folded span is serialized within summaryInputBudget(window); oversized
// input is head+tail truncated, and a context-too-long rejection halves the
// input and retries instead of failing outright.
//
// If summarisation fails, returns the original history unchanged with an empty
// summary string. runCompaction layers the deterministic fallback above this.
//
// The first message is always kept verbatim (user's original task anchor).
func compactHistory(
	ctx context.Context,
	lm fantasy.LanguageModel,
	req compactionRequest,
) (CompactionResult, error) {
	history := req.History
	trigger := req.Trigger
	keepRecentTokens := req.KeepRecentTokens
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

	cutIdx, ok := findCutPoint(rest, keepRecentTokens)
	if !ok && req.Force {
		// The history fits the retention budget, but the caller already decided
		// compaction is required (manual /compact, trigger crossed, retry).
		// Fold everything before the newest user turn; a newest user turn at
		// rest[0] means there is nothing older to fold.
		for j := len(rest) - 1; j > 0; j-- {
			if rest[j].Role == sdk.RoleUser {
				cutIdx, ok = j, true
				break
			}
		}
	}
	if !ok || cutIdx <= 0 {
		return noOpCompaction(history, trigger), nil
	}

	toSummarize := rest[:cutIdx]
	toKeep := rest[cutIdx:]

	budget := summaryInputBudget(req.Window)
	sourceTruncated := estimateTokens(toSummarize)+estimateStr(req.PriorSummary) > budget

	var (
		summaryText string
		usage       fantasy.Usage
		latency     time.Duration
		lastErr     error
	)
	// A provider context-limit rejection on the summary request means the
	// folded span (plus the prior summary) does not fit the summarizer's own
	// window. Halve the input and re-plan rather than failing the compaction.
	for range 2 {
		src := buildSummarySource(toSummarize, req.PriorSummary, budget)
		text, u, d, err := runSummary(ctx, lm, src)
		if err != nil {
			lastErr = err
			if isContextTooLong(err) && budget > minSummaryInputTokens {
				budget /= 2
				if budget < minSummaryInputTokens {
					budget = minSummaryInputTokens
				}
				continue
			}
			return noOpCompaction(history, trigger), fmt.Errorf("compaction: summarize: %w", err)
		}
		summaryText, usage, latency, lastErr = text, u, d, nil
		break
	}
	if lastErr != nil {
		return noOpCompaction(history, trigger), fmt.Errorf("compaction: summarize: %w", lastErr)
	}

	// Build summary message content: header + LLM output + optional file list.
	var msgContent strings.Builder
	fmt.Fprintf(&msgContent, "[Previous conversation summary — %d messages compacted]\n\n", len(toSummarize))
	msgContent.WriteString(summaryText)
	if sourceTruncated {
		msgContent.WriteString("\n\n> Note: older folded messages were truncated to fit the summarization budget. " +
			"The full text remains retrievable with the recall tool.")
	}

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
		Latency:  latency,
		Trigger:  trigger,
		Method:   CompactionMethodSummary,
	}, nil
}

// runSummary streams one summarization request and returns its text, usage,
// and latency. An empty streamed summary is an error.
func runSummary(
	ctx context.Context,
	lm fantasy.LanguageModel,
	source string,
) (string, fantasy.Usage, time.Duration, error) {
	var summary strings.Builder
	fa := fantasy.NewAgent(lm)
	start := time.Now()
	res, err := fa.Stream(ctx, fantasy.AgentStreamCall{
		Prompt: source + "\n\n---\n\n" + compactionSummaryPrompt,
		OnTextDelta: func(_, text string) error {
			summary.WriteString(text)
			return nil
		},
	})
	latency := time.Since(start)
	if err != nil {
		return "", fantasy.Usage{}, latency, err
	}
	if summary.Len() == 0 {
		return "", fantasy.Usage{}, latency, fmt.Errorf("empty summary")
	}
	var usage fantasy.Usage
	if res != nil {
		usage = res.TotalUsage
	}
	return summary.String(), usage, latency, nil
}

// summaryInputBudget bounds the conversation text sent to the summarizer.
// The folded span can be much larger than the model window; budgeting it
// explicitly keeps the summary request viable instead of relying on a fixed
// per-message truncation that discards long messages wholesale.
func summaryInputBudget(window int64) int64 {
	if window <= 0 {
		window = defaultContextWindow
	}
	budget := window / 4
	if budget > maxSummaryInputTokens {
		budget = maxSummaryInputTokens
	}
	if budget < minSummaryInputTokens {
		budget = minSummaryInputTokens
	}
	return budget
}

// buildSummarySource serializes the messages being folded within budget
// tokens. Full text is preserved whenever it fits; otherwise each message is
// head+tail truncated, and when even the per-message floor cannot fit every
// message, the oldest ones are omitted with an explicit marker.
func buildSummarySource(msgs []sdk.Message, prior string, budget int64) string {
	if budget <= 0 {
		budget = minSummaryInputTokens
	}
	var b strings.Builder

	// Prepend prior summary context when available (iterative compaction).
	if prior != "" {
		priorBudget := budget / 4
		if priorBudget < 256 {
			priorBudget = 256
		}
		b.WriteString("[Prior summary from previous compaction]\n")
		b.WriteString(truncateContent(prior, priorBudget))
		b.WriteString("\n---\nUpdate the above summary to include the new messages below.\n\n")
	}

	remaining := budget - estimateStr(b.String())
	if remaining < 1 {
		remaining = 1
	}
	if estimateTokens(msgs) <= remaining {
		for _, m := range msgs {
			fmt.Fprintf(&b, "%s: %s\n\n", m.Role, m.Content)
		}
		return b.String()
	}

	perMessage := remaining / int64(len(msgs))
	if perMessage < 64 {
		keepCount := int(remaining / 64)
		if keepCount < 1 {
			keepCount = 1
		}
		if keepCount > len(msgs) {
			keepCount = len(msgs)
		}
		if omitted := len(msgs) - keepCount; omitted > 0 {
			fmt.Fprintf(&b, "[%d earlier messages omitted]\n", omitted)
			msgs = msgs[omitted:]
		}
		perMessage = remaining / int64(len(msgs))
		if perMessage < 1 {
			perMessage = 1
		}
	}
	for _, m := range msgs {
		fmt.Fprintf(&b, "%s: %s\n\n", m.Role, truncateContent(m.Content, perMessage))
	}
	return b.String()
}

// truncateContent returns s shortened to at most maxTokens estimated tokens,
// preserving a head and a tail around an explicit truncation marker.
func truncateContent(s string, maxTokens int64) string {
	if maxTokens <= 0 {
		return ""
	}
	if estimateStr(s) <= maxTokens {
		return s
	}
	runes := []rune(s)
	headBudget := maxTokens * 3 / 5
	tailBudget := maxTokens - headBudget
	head := runesUpToTokens(runes, headBudget)
	tail := runesFromTokens(runes, tailBudget)
	return string(head) + "\n…[truncated]…\n" + string(tail)
}

// runesUpToTokens returns the longest prefix whose estimated token count does
// not exceed budget.
func runesUpToTokens(runes []rune, budget int64) []rune {
	if budget <= 0 {
		return nil
	}
	var ascii, wide int64
	for i, r := range runes {
		if r > 0x7f {
			wide++
		} else {
			ascii++
		}
		if ascii/4+wide > budget {
			return runes[:i]
		}
	}
	return runes
}

// runesFromTokens returns the longest suffix whose estimated token count does
// not exceed budget.
func runesFromTokens(runes []rune, budget int64) []rune {
	if budget <= 0 {
		return nil
	}
	var ascii, wide int64
	for i := len(runes) - 1; i >= 0; i-- {
		if runes[i] > 0x7f {
			wide++
		} else {
			ascii++
		}
		if ascii/4+wide > budget {
			return runes[i+1:]
		}
	}
	return runes
}

// compactionRun is the full input to the adaptive compaction pipeline.
type compactionRun struct {
	PriorSummary  string
	SystemPrompt  string
	NextMessage   string
	Trigger       string
	History       []sdk.Message
	Tools         []fantasy.AgentTool
	Config        CompactConfig
	Window        int64
	Force         bool
	AllowTruncate bool
}

// runCompaction executes the adaptive pipeline: budgeted summarization, a
// progress guard that folds the retained tail again when the first pass did
// not create real headroom, and deterministic truncation when summarization
// fails or cannot get under the hard window limit.
//
// It never returns a history larger than the input. A summary result is
// returned when it fits the hard safety limit; otherwise the caller gets a
// truncation result (when allowed) or the summarization error.
func runCompaction(
	ctx context.Context,
	lm fantasy.LanguageModel,
	req compactionRun,
) (CompactionResult, error) {
	if req.Window <= 0 {
		req.Window = defaultContextWindow
	}
	budget := newCompactionBudget(req.Window, req.Config)
	before := contextEstimate(req.History, req.SystemPrompt, req.NextMessage, req.Tools)
	fixedTokens := estimateStr(req.SystemPrompt) + estimateStr(req.NextMessage) + estimateToolTokens(req.Tools)

	history := req.History
	prior := req.PriorSummary
	keep := budget.KeepRecent
	force := req.Force

	var (
		last          CompactionResult
		lastErr       error
		totalMessages int
		totalUsage    fantasy.Usage
		totalLatency  time.Duration
	)
	for pass := range compactionMaxPasses {
		res, err := compactHistory(ctx, lm, compactionRequest{
			History:          history,
			PriorSummary:     prior,
			KeepRecentTokens: keep,
			Window:           req.Window,
			Trigger:          req.Trigger,
			Force:            force,
		})
		if err != nil {
			lastErr = err
			break
		}
		if res.Summary == "" {
			// A headroom pass that finds nothing extra foldable must not erase
			// the successful first pass's result.
			if last.Summary == "" {
				last = res
			}
			break
		}
		// Each pass may summarize a span that includes the previous pass's
		// summary message; report the cumulative fold count and model cost so
		// the progress guard does not make a multi-pass run look cheaper than
		// it was.
		totalMessages += res.Messages
		totalUsage = addUsage(totalUsage, res.Usage)
		totalLatency += res.Latency
		res.Messages = totalMessages
		res.Usage = totalUsage
		res.Latency = totalLatency
		res.EstimatedTokens = contextEstimate(res.History, req.SystemPrompt, req.NextMessage, req.Tools)
		res.TokensBefore = before
		res.TokensAfter = res.EstimatedTokens
		last = res

		// Progress guard: a summary that still sits above the recovery band
		// gets one more pass with half the retention budget. Without this, the
		// next turn would immediately compact again on the same context.
		if res.EstimatedTokens <= budget.Recovery ||
			keep <= minKeepRecentTokens ||
			fixedTokens >= budget.Recovery ||
			pass == compactionMaxPasses-1 {
			break
		}
		history = res.History
		prior = res.Summary
		keep /= 2
		if keep < minKeepRecentTokens {
			keep = minKeepRecentTokens
		}
		force = true
	}

	if last.Summary != "" {
		reduced := last.EstimatedTokens < before
		// Manual compaction may commit a summary that grows a tiny context
		// (the user asked for it). Automatic compaction must actually reduce
		// the estimate; a non-shrinking result is not committed.
		if last.EstimatedTokens <= budget.Safety && (!req.AllowTruncate || reduced) {
			return last, nil
		}
	}

	if !req.AllowTruncate {
		if lastErr != nil {
			return noOpCompaction(req.History, req.Trigger), lastErr
		}
		return last, nil
	}

	base := req.History
	if last.Summary != "" {
		// Preserve the produced summary while shedding the retained tail.
		base = last.History
	}
	if trunc, ok := truncateHistory(base, budget, req.SystemPrompt, req.NextMessage, req.Tools, req.Trigger); ok {
		trunc.TokensBefore = before
		return trunc, nil
	}
	if lastErr != nil {
		return noOpCompaction(req.History, req.Trigger), lastErr
	}
	// Nothing reduced the request (a non-shrinking summary and no truncation
	// headroom): leave history untouched rather than committing a larger
	// context.
	return noOpCompaction(req.History, req.Trigger), nil
}

// addUsage sums two provider usage records field by field.
func addUsage(a, b fantasy.Usage) fantasy.Usage {
	a.InputTokens += b.InputTokens
	a.OutputTokens += b.OutputTokens
	a.TotalTokens += b.TotalTokens
	a.ReasoningTokens += b.ReasoningTokens
	a.CacheCreationTokens += b.CacheCreationTokens
	a.CacheReadTokens += b.CacheReadTokens
	return a
}

// truncateHistory is the model-free fallback: keep the newest messages and
// the original-task anchor that fit the hard window limit, and replace
// everything else with an explicit truncation notice. It reports false when
// the result would not be smaller than the input.
func truncateHistory(
	history []sdk.Message,
	b compactionBudget,
	sysPrompt, nextMessage string,
	tools []fantasy.AgentTool,
	trigger string,
) (CompactionResult, bool) {
	before := contextEstimate(history, sysPrompt, nextMessage, tools)
	if before <= b.Safety {
		return CompactionResult{}, false
	}
	fixed := estimateStr(sysPrompt) + estimateStr(nextMessage) + estimateToolTokens(tools)
	// Reserve room for the truncation notice itself; otherwise a kept tail
	// that exactly fills the budget pushes the result over the safety limit.
	const truncationNoticeTokens int64 = 128
	avail := b.Safety - fixed - truncationNoticeTokens
	if avail < 1 {
		avail = 1
	}

	// The original-task anchor is kept only when it is cheap relative to the
	// remaining budget.
	anchorCount := 0
	var anchor []sdk.Message
	if len(history) > 1 {
		anchorTokens := estimateStr(history[0].Content)
		if anchorTokens <= avail/4 {
			anchor = []sdk.Message{history[0]}
			anchorCount = 1
			avail -= anchorTokens
			if avail < 1 {
				avail = 1
			}
		}
	}

	var kept []sdk.Message
	var used int64
	for i := len(history) - 1; i >= 0; i-- {
		m := history[i]
		t := estimateStr(m.Content)
		if len(kept) == 0 {
			if t > avail {
				m.Content = truncateContent(m.Content, avail)
				t = estimateStr(m.Content)
			}
			kept = append(kept, m)
			used += t
			continue
		}
		if used+t > avail {
			break
		}
		kept = append([]sdk.Message{m}, kept...)
		used += t
	}

	removed := len(history) - len(kept) - anchorCount
	if removed < 0 {
		removed = 0
	}
	notice := fmt.Sprintf(
		"[Context truncated: %d older message(s) removed without a summary because the context could not be reduced by summarization. The full detail remains retrievable with recall.]",
		removed,
	)
	newHistory := make([]sdk.Message, 0, len(anchor)+1+len(kept))
	newHistory = append(newHistory, anchor...)
	newHistory = append(newHistory, sdk.Message{Role: sdk.RoleUser, Content: notice})
	newHistory = append(newHistory, kept...)

	after := contextEstimate(newHistory, sysPrompt, nextMessage, tools)
	if after >= before {
		return CompactionResult{}, false
	}
	return CompactionResult{
		History:         newHistory,
		Summary:         notice,
		Trigger:         trigger,
		Messages:        removed,
		Method:          CompactionMethodTruncate,
		EstimatedTokens: after,
		TokensBefore:    before,
		TokensAfter:     after,
	}, true
}

// noOpCompaction builds a CompactionResult for runs that did not compact
// (history fits, no valid boundary, or summarization failure). Summary is
// empty, which callers treat as "compaction did not happen".
func noOpCompaction(history []sdk.Message, trigger string) CompactionResult {
	return CompactionResult{History: history, Trigger: trigger}
}
