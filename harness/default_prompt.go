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
	sb.WriteString("You are an action-taking agent. When asked to do something, respond with a tool call — not an explanation of what you are about to do.\n\n")
	sb.WriteString("**The failure mode to avoid:** writing \"Let me start\", \"I'll\", \"I will\", \"I'm going to\", or any narration — then stopping without calling a tool. That leaves the user waiting with nothing happening.\n\n")
	sb.WriteString("**The correct pattern:** call the tool first, then explain what you found or did.\n\n")
	sb.WriteString("The only text you should produce before a tool call is a clarifying question when you genuinely cannot proceed without more information. If you know what to do, do it now.\n")

	if len(tools) > 0 {
		sorted := make([]sdk.Tool, len(tools))
		copy(sorted, tools)
		sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })

		sb.WriteString("\n### Tools\n\n")
		for _, t := range sorted {
			desc := t.Description
			if desc == "" {
				desc = "(no description)"
			}
			fmt.Fprintf(&sb, "- **%s** — %s\n", t.Name, desc)
		}
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
