package agent

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"encoding/json"
	"sort"

	"charm.land/fantasy"
	"github.com/mattdurham/wllr/modules/sdk"
)

// contextbreakdown.go decomposes what the next provider request will contain,
// alongside the authoritative provider-reported usage from the most recent
// completed turn. It backs the /context command.
//
// Two structural facts shape the buckets:
//
//   - wllr history carries only user/assistant text (see Submit: tool calls
//     and results are recorded to the canonical transcript, never to
//     a.history). Tool traffic from a completed turn is therefore not part of
//     the persistent context: each Submit builds a fresh fantasy agent, and
//     the recovery path for vanished detail is the recall tool, not context.
//
//   - Bucket sizes use the chars/4 heuristic shared with compaction
//     (estimateTokens). It intentionally overestimates and will not sum to
//     the provider-reported input exactly; the reported number is the
//     authoritative one, and the estimate is for attribution.

// ContextBreakdown is the context accounting for one agent.
type ContextBreakdown struct {
	// ContextWindow is the resolved input window for this agent's model.
	// Zero when unknown (model metadata unresolved).
	ContextWindow int64

	// LastRequest is the provider-reported usage from the most recently
	// completed turn: the largest per-step input (see contextUsageFromResult),
	// which includes that step's accumulated tool traffic and cache effects.
	LastRequest fantasy.Usage

	// SystemTokens estimates the system prompt (chars/4).
	SystemTokens int64

	// SystemComponents decomposes the system prompt into its labeled sources:
	// the prompt extension's report (built-in rules, tools list, prompt files,
	// AGENTS.md, cwd note) plus one entry per labeled extension append (the
	// skills list). Chars are raw counts; estimate tokens at chars/4. It
	// describes the base prompt only — an agent's SpawnOpts prompt override
	// (the subagent identity block) is not a component. Empty when no report
	// has arrived (older prompt extension, or nothing set yet).
	SystemComponents []sdk.SystemPromptComponent

	// ToolTokens estimates the serialized tool definitions sent with each
	// request (chars/4). ToolCount is the number of definitions.
	ToolTokens int64
	ToolCount  int

	// ToolsByTool attributes the tool-definition tokens to each tool,
	// heaviest first. Definitions are static per session, but they are
	// often the second-largest resident cost after history, and the
	// aggregate alone cannot show which tool's schema is the expensive one.
	// Per-tool values are each rounded down (chars/4 individually), so they
	// may trail ToolTokens by up to ToolCount-1 tokens of rounding.
	ToolsByTool []ToolSize
	// HistoryTokens estimates the LLM-facing history — user/assistant text
	// only (chars/4). UserTokens and AssistantTokens split it by role, and
	// TurnCount counts the user messages (one per recorded turn).
	HistoryTokens   int64
	UserTokens      int64
	AssistantTokens int64
	TurnCount       int

	// LargestMessages is the heaviest history messages, heaviest first
	// (capped at maxLargestMessages). A single oversized user message or
	// pasted document is the most common way a window fills, and the
	// by-role totals alone cannot reveal which specific prompt did it.
	LargestMessages []MessageSize

	// FilteredTokens estimates system/steering messages held in history but
	// filtered from LLM context by sdkToFantasyMessages. They cost memory,
	// never context.
	FilteredTokens int64

	// CanonicalChars and CanonicalEntries size the canonical transcript: the
	// verbatim record of everything this agent saw and did. It is NOT part of
	// context; it is what the recall tool can retrieve. CanonicalByKind
	// attributes it by entry kind (user message, assistant message, tool call,
	// tool result — see TranscriptKind constants), CanonicalMessagesByRole
	// further splits the message kind by role, and CanonicalByTool attributes
	// tool traffic (calls + results) to the producing tool, heaviest first.
	CanonicalChars          int64
	CanonicalEntries        int
	CanonicalByKind         map[string]int64
	CanonicalMessagesByRole map[string]int64
	CanonicalByTool         []ToolSize
}

// Sum returns the estimated total of all LLM-facing buckets (system + tools +
// history). Filtered and canonical sizes are excluded: they never reach the
// provider.
func (b ContextBreakdown) Sum() int64 {
	return b.SystemTokens + b.ToolTokens + b.HistoryTokens
}

// ContextBreakdown snapshots the agent's next-request composition. All reads
// take the same locks the corresponding Submit paths use, so it is safe to
// call while a turn is running (the snapshot may then be slightly stale, but
// never torn).
func (a *Agent) ContextBreakdown() ContextBreakdown {
	a.systemPromptMu.RLock()
	sysBase := a.systemPrompt
	a.systemPromptMu.RUnlock()
	sys := sysBase
	if sysBase != "" && a.opts.SystemPrompt != "" {
		sys = sysBase + "\n\n" + a.opts.SystemPrompt
	} else if sys == "" {
		sys = a.opts.SystemPrompt
	}

	a.toolsFnMu.RLock()
	toolsFn := a.toolsFn
	a.toolsFnMu.RUnlock()
	// Same resolution Submit uses: dynamic toolsFn wins, opts.Tools otherwise.
	tools := a.opts.Tools
	if toolsFn != nil {
		tools = toolsFn()
	}

	a.historyMu.Lock()
	history := make([]sdk.Message, len(a.history))
	copy(history, a.history)
	a.historyMu.Unlock()

	a.lastUsageMu.RLock()
	usage := a.lastUsage
	a.lastUsageMu.RUnlock()

	b := ContextBreakdown{
		ContextWindow: a.ContextWindow(),
		LastRequest:   usage,
		SystemTokens:  estimateStr(sys),
		ToolTokens:    estimateToolTokens(tools),
		ToolCount:     len(tools),
		// The component ledger describes the base prompt; the agent-level
		// SpawnOpts override (subagent identity block) is not part of it.
		SystemComponents: a.pool.PromptComponents(),
	}
	// Tool→extension attribution comes from the host's registration records;
	// tools with no owner were registered outside an extension context
	// (harness-native tools, the recall tool).
	owners := a.pool.ToolOwners()
	// Per-tool definition tokens: same JSON marshal the aggregate uses,
	// individually rounded. Insertion order is irrelevant; the renderer sorts.
	for _, tool := range tools {
		raw, err := json.Marshal(tool.Info())
		if err != nil {
			continue
		}
		b.ToolsByTool = append(b.ToolsByTool, ToolSize{
			Name:      tool.Info().Name,
			Extension: owners[tool.Info().Name],
			Tokens:    int64(len(raw)) / 4,
		})
	}
	sort.SliceStable(b.ToolsByTool, func(i, j int) bool {
		return b.ToolsByTool[i].Tokens > b.ToolsByTool[j].Tokens
	})

	for _, m := range history {
		switch {
		case m.Type == sdk.MessageTypeSystem || m.Type == sdk.MessageTypeSteering:
			b.FilteredTokens += estimateStr(m.Content)
		case m.Role == sdk.RoleUser:
			b.UserTokens += estimateStr(m.Content)
			b.TurnCount++
		case m.Role == sdk.RoleAssistant:
			b.AssistantTokens += estimateStr(m.Content)
		}
	}
	b.HistoryTokens = b.UserTokens + b.AssistantTokens

	// Surface the individual prompts that dominate the window. Every
	// user/assistant message competes for the same cap, so a by-role total
	// alone can hide a single pasted document dwarfing everything else.
	// System/steering types are excluded by type, not role: a steering
	// message carries RoleUser but never reaches the provider.
	for _, m := range history {
		if m.Type == sdk.MessageTypeSystem || m.Type == sdk.MessageTypeSteering {
			continue
		}
		if m.Role != sdk.RoleUser && m.Role != sdk.RoleAssistant {
			continue
		}
		b.LargestMessages = append(b.LargestMessages, MessageSize{
			Role:   string(m.Role),
			Tokens: estimateStr(m.Content),
			Chars:  len(m.Content),
		})
	}
	sort.SliceStable(b.LargestMessages, func(i, j int) bool {
		return b.LargestMessages[i].Tokens > b.LargestMessages[j].Tokens
	})
	if len(b.LargestMessages) > maxLargestMessages {
		b.LargestMessages = b.LargestMessages[:maxLargestMessages]
	}

	// Read the canonical transcript without creating one: an agent that has
	// recorded nothing has no canonical overhead to report. canonicalMu guards
	// lazy creation; the Transcript's own lock guards its entries.
	a.canonicalMu.Lock()
	t := a.canonical
	a.canonicalMu.Unlock()
	if t != nil {
		b.CanonicalChars, b.CanonicalEntries = t.SizeChars()
		b.CanonicalByKind = make(map[string]int64)
		b.CanonicalMessagesByRole = make(map[string]int64)
		toolChars := make(map[string]int64)
		for _, e := range t.Snapshot() {
			b.CanonicalByKind[e.Kind] += int64(len(e.Content))
			if e.Kind == TranscriptKindMessage {
				role := e.Role
				if role == "" {
					role = "unknown"
				}
				b.CanonicalMessagesByRole[role] += int64(len(e.Content))
			} else {
				toolChars[e.ToolName] += int64(len(e.Content))
			}
		}
		for name, chars := range toolChars {
			b.CanonicalByTool = append(b.CanonicalByTool, ToolSize{
				Name: name, Chars: chars,
			})
		}
		sort.SliceStable(b.CanonicalByTool, func(i, j int) bool {
			return b.CanonicalByTool[i].Chars > b.CanonicalByTool[j].Chars
		})
	}
	return b
}
