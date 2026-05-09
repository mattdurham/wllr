package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mattdurham/wllr/sdk"
)

// buildDefaultActionPrompt constructs the "guide to action" system prompt section
// from the currently registered tools and slash commands. Called after session_start
// so the lists are complete (all extension _init and session_start registrations done).
func buildDefaultActionPrompt(tools []sdk.Tool, commands []Command) string {
	var sb strings.Builder

	sb.WriteString("## Action Rules\n\n")
	sb.WriteString("You are an action-taking agent. Before each tool call write one short sentence explaining your decision or what you found — then immediately call the tool. Never write reasoning as comments inside shell commands; write it as text the user can read.\n\n")
	sb.WriteString("**The failure mode to avoid:** writing \"Let me start\" or \"I'll do X\" — then stopping without calling a tool. Text must always be followed by a tool call or a direct answer.\n\n")
	sb.WriteString("**The correct pattern:** one sentence of reasoning → tool call → one sentence summarising the result → next tool call. Keep each sentence tight; do not write paragraphs between tool calls.\n")

	// Tool schemas are already sent to the model via the API — listing them
	// again with full descriptions in the system prompt doubles the token cost.
	// Just name them so the model knows what's available.
	if len(tools) > 0 {
		sorted := make([]sdk.Tool, len(tools))
		copy(sorted, tools)
		sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })

		names := make([]string, len(sorted))
		for i, t := range sorted {
			names[i] = t.Name
		}
		fmt.Fprintf(&sb, "\nAvailable tools: %s\n", strings.Join(names, ", "))
	}

	if len(commands) > 0 {
		sb.WriteString("\n### Slash commands\n\n")
		for _, c := range commands {
			desc := c.Desc
			if desc == "" {
				desc = "(no description)"
			}
			fmt.Fprintf(&sb, "- **/%s** — %s\n", c.Name, desc)
		}
	}

	return sb.String()
}
