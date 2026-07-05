package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mattdurham/wllr/modules/sdk"
)

// buildDefaultActionPrompt constructs the "guide to action" system prompt section
// from the currently registered tools and slash commands. Called after session_start
// so the lists are complete (all extension _init and session_start registrations done).
func buildDefaultActionPrompt(tools []sdk.Tool, commands []Command) string {
	var sb strings.Builder

	sb.WriteString("## Action Rules\n\n")
	sb.WriteString(
		"You are an action-taking agent. Before each tool call write one short sentence explaining your decision or what you found — then immediately call the tool. Never write reasoning as comments inside shell commands; write it as text the user can read.\n\n",
	)
	sb.WriteString(
		"**The failure mode to avoid:** writing \"Let me start\", \"Now I'll\", \"Next I'll\", or \"I'll write...\" — then stopping. That announces an action without taking it. If you plan to do something, do it in the same response.\n\n",
	)
	sb.WriteString(
		"**The correct pattern:** one sentence of reasoning → tool call → one sentence summarizing the result → next tool call. Keep going until the task is fully done or you need input.\n\n",
	)
	sb.WriteString(
		"**Never end a turn by describing your next action.** Either call the tool now, or tell the user the task is complete.\n",
	)

	// Tool schemas are already sent to the model via the API — listing them
	// again with full descriptions in the system prompt doubles the token cost.
	// Just name them so the model knows what's available.
	if len(tools) > 0 {
		sorted := make([]sdk.Tool, len(tools))
		copy(sorted, tools)
		sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })

		names := make([]string, len(sorted))
		toolNames := make(map[string]bool, len(sorted))
		for i, t := range sorted {
			names[i] = t.Name
			toolNames[t.Name] = true
		}
		fmt.Fprintf(&sb, "\nAvailable tools: %s\n", strings.Join(names, ", "))
		if toolNames["lsp_diagnostics"] || toolNames["lsp_lint"] || toolNames["lsp_definition"] ||
			toolNames["lsp_references"] {
			sb.WriteString("\n### Code Intelligence\n\n")
			sb.WriteString(
				"- Prefer LSP tools for diagnostics, linting, code navigation, finding references, and refactor reconnaissance before broad manual searching.\n",
			)
			sb.WriteString(
				"- Use `lsp_diagnostics` or `lsp_lint` after editing supported source files to catch compiler/type/language issues before you finish.\n",
			)
			sb.WriteString(
				"- Use `lsp_symbols`, `lsp_definition`, and `lsp_references` to understand code before changing it.\n",
			)
			sb.WriteString(
				"- Use `lsp_refactor_preview` before renames or refactors, then apply reviewed edits with normal file-editing tools.\n",
			)
			sb.WriteString(
				"- Use `lsp_capabilities` when you need to know which languages, tools, and output contracts are available.\n",
			)
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
