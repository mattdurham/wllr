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

	sb.WriteString("## Capabilities\n\n")
	sb.WriteString("Act immediately on user requests using your tools. Read files, run commands, edit code — don't describe what you plan to do, just do it.\n\n")
	sb.WriteString("You must always be doing one of two things: working (using a tool) or asking a clarifying question. Never pause silently. If you are unsure what to do next, ask. If you know what to do, do it.\n")

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
