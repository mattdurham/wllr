package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mattdurham/wllr/modules/agent"
)

// contextmodal.go renders the /context command's breakdown modal. The modal
// shows two views of the same context window: the last request's usage —
// provider-reported (authoritative) or, on a fresh session before any provider
// report, the chars/4 baseline seed, labeled as such — and an estimate of the
// next request's composition (attribution). The two rarely match exactly — the
// chars/4 heuristic overestimates, and within-turn tool traffic plus prompt
// caching only exist in the provider's count — so the delta is called out
// rather than hidden.

// renderContextBreakdown formats a ContextBreakdown for the modal.
func renderContextBreakdown(b agent.ContextBreakdown) string {
	var sb strings.Builder
	sb.WriteString("Context window: ")
	if b.ContextWindow > 0 {
		fmt.Fprintf(&sb, "%s tokens\n", comma(b.ContextWindow))
	} else {
		sb.WriteString("unknown\n")
	}

	sb.WriteString("\nLast request")
	if b.LastRequestIsEstimate {
		sb.WriteString(" (chars/4 baseline — no provider report yet)")
	} else {
		sb.WriteString(" (provider-reported)")
	}
	if b.LastRequest.InputTokens == 0 {
		sb.WriteString("\n  No completed turn yet.\n")
	} else {
		fmt.Fprintf(&sb, "\n  input %s", comma(b.LastRequest.InputTokens))
		if b.ContextWindow > 0 {
			fmt.Fprintf(&sb, "  (%.0f%% of window)", 100*float64(b.LastRequest.InputTokens)/float64(b.ContextWindow))
		}
		if b.LastRequest.CacheReadTokens > 0 || b.LastRequest.CacheCreationTokens > 0 {
			fmt.Fprintf(&sb, "\n  cache read %s · cache write %s",
				comma(b.LastRequest.CacheReadTokens), comma(b.LastRequest.CacheCreationTokens))
		}
		fmt.Fprintf(&sb, "\n  output %s\n", comma(b.LastRequest.OutputTokens))
	}

	fmt.Fprintf(&sb, "\nNext request estimate (chars/4 heuristic)\n")
	fmt.Fprintf(&sb, "  system prompt     %s\n", comma(b.SystemTokens))
	renderSystemComponents(&sb, b)
	fmt.Fprintf(&sb, "  tools (%d)         %s\n", b.ToolCount, comma(b.ToolTokens))
	renderToolsByExtension(&sb, b)
	fmt.Fprintf(&sb, "  history           %s", comma(b.HistoryTokens))
	if b.TurnCount > 0 {
		fmt.Fprintf(&sb, "  (%d turns: user %s, assistant %s)",
			b.TurnCount, comma(b.UserTokens), comma(b.AssistantTokens))
	}
	sb.WriteString("\n")
	for i, ms := range b.LargestMessages {
		if i == 0 {
			sb.WriteString("  largest prompts:\n")
		}
		fmt.Fprintf(&sb, "    %-9s      %s\n", ms.Role, comma(ms.Tokens))
	}
	if b.FilteredTokens > 0 {
		fmt.Fprintf(&sb, "  filtered (control, not sent) %s\n", comma(b.FilteredTokens))
	}
	fmt.Fprintf(&sb, "  estimated total   %s\n", comma(b.Sum()))

	if b.CanonicalEntries > 0 {
		fmt.Fprintf(&sb, "\nNot in context\n")
		fmt.Fprintf(&sb, "  canonical transcript: %s chars across %d entries — verbatim detail\n",
			comma(b.CanonicalChars), b.CanonicalEntries)
		sb.WriteString("  folded out of context is recoverable via the recall tool.\n")
		renderTranscriptBreakdown(&sb, b)
	}

	if b.LastRequest.InputTokens > 0 {
		if gap := b.LastRequest.InputTokens - b.Sum(); gap > b.LastRequest.InputTokens/10 {
			fmt.Fprintf(&sb, "\nGap of ~%s tokens vs reported input: within-turn tool traffic\n", comma(gap))
			sb.WriteString("(transient — not kept between turns) plus prompt-caching effects\n")
			sb.WriteString("and heuristic error. Use /tools to see the current turn's calls.\n")
		}
	}
	return strings.TrimRight(sb.String(), "\n")
}

// renderSystemComponents lists the system prompt's labeled sources under the
// system total. Each component's tokens are its chars/4; rows are display
// order (the ledger's arrival order), not sorted — prompt pieces read
// top-to-bottom in the actual prompt. Nothing renders when the ledger is empty
// (no report yet, or nothing set), so the section degrades to the plain total.
func renderSystemComponents(sb *strings.Builder, b agent.ContextBreakdown) {
	if len(b.SystemComponents) == 0 {
		return
	}
	sb.WriteString("    by source:\n")
	for _, comp := range b.SystemComponents {
		source := comp.Source
		if source == "" {
			source = "(unlabeled)"
		}
		fmt.Fprintf(sb, "      %-30s %s\n", source, comma(int64(comp.Chars)/4))
	}
}

// toolGroup accumulates one extension's tool rows and subtotal for
// renderToolsByExtension.
type toolGroup struct {
	name  string
	total int64
	tools []agent.ToolSize
}

// renderToolsByExtension groups the per-tool token list under its owning
// extension, with a subtotal per extension, heaviest extension first and
// heaviest tool first within it. Tools with no owner were registered outside
// an extension context (harness-native tools, the recall tool) and group under
// "harness". Ownerless snapshots (no row carries an owner) keep the old flat
// listing.
func renderToolsByExtension(sb *strings.Builder, b agent.ContextBreakdown) {
	if len(b.ToolsByTool) == 0 {
		return
	}
	owned := false
	for _, ts := range b.ToolsByTool {
		if ts.Extension != "" {
			owned = true
			break
		}
	}
	if !owned {
		for _, ts := range b.ToolsByTool {
			fmt.Fprintf(sb, "    %-17s %s\n", ts.Name, comma(ts.Tokens))
		}
		return
	}
	byExt := map[string]*toolGroup{}
	var order []string
	for _, ts := range b.ToolsByTool {
		name := ts.Extension
		if name == "" {
			name = "harness"
		}
		g, ok := byExt[name]
		if !ok {
			g = &toolGroup{name: name}
			byExt[name] = g
			order = append(order, name)
		}
		g.total += ts.Tokens
		g.tools = append(g.tools, ts)
	}
	sort.SliceStable(order, func(i, j int) bool {
		if byExt[order[i]].total != byExt[order[j]].total {
			return byExt[order[i]].total > byExt[order[j]].total
		}
		return order[i] < order[j]
	})
	for _, name := range order {
		g := byExt[name]
		fmt.Fprintf(sb, "    %-17s %s\n", name, comma(g.total))
		for _, ts := range g.tools {
			fmt.Fprintf(sb, "      %-17s %s\n", ts.Name, comma(ts.Tokens))
		}
	}
}

// renderTranscriptBreakdown appends the canonical transcript's by-type and
// by-tool attribution under the "Not in context" heading. All inputs are
// chars (the transcript is never sent to the provider, so tokens would be a
// fiction); the maps may be nil on snapshots taken before this attribution
// existed, in which case only the totals above are shown.
func renderTranscriptBreakdown(sb *strings.Builder, b agent.ContextBreakdown) {
	if b.CanonicalByKind == nil {
		return
	}
	sb.WriteString("  by type:\n")
	kindLabels := map[string]string{
		agent.TranscriptKindMessage:    "messages",
		agent.TranscriptKindToolCall:   "tool calls",
		agent.TranscriptKindToolResult: "tool results",
	}
	for _, kind := range []string{
		agent.TranscriptKindMessage,
		agent.TranscriptKindToolCall,
		agent.TranscriptKindToolResult,
	} {
		if chars, ok := b.CanonicalByKind[kind]; ok && chars > 0 {
			fmt.Fprintf(sb, "    %-17s %s\n", kindLabels[kind], comma(chars))
			if kind == agent.TranscriptKindMessage {
				// The messages bucket is where prompts and replies live, so it
				// gets the same by-role split the resident history shows.
				if u := b.CanonicalMessagesByRole["user"]; u > 0 {
					fmt.Fprintf(sb, "      user        %s\n", comma(u))
				}
				if a := b.CanonicalMessagesByRole["assistant"]; a > 0 {
					fmt.Fprintf(sb, "      assistant   %s\n", comma(a))
				}
			}
		}
	}
	if len(b.CanonicalByTool) > 0 {
		sb.WriteString("  tool traffic by tool:\n")
		const show = 10
		for i, ts := range b.CanonicalByTool {
			if i == show {
				fmt.Fprintf(sb, "    … and %d more tools\n", len(b.CanonicalByTool)-show)
				break
			}
			fmt.Fprintf(sb, "    %-17s %s\n", ts.Name, comma(ts.Chars))
		}
	}
}

// comma formats an int64 with thousands separators.
func comma(n int64) string {
	s := fmt.Sprintf("%d", n)
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	var parts []string
	for len(s) > 3 {
		parts = append([]string{s[len(s)-3:]}, parts...)
		s = s[:len(s)-3]
	}
	parts = append([]string{s}, parts...)
	out := strings.Join(parts, ",")
	if neg {
		out = "-" + out
	}
	return out
}
