package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/mattdurham/wllr/modules/sdk"
)

// renderDefaultStatusLine fits a cloned bundled status tree to the viewport.
// Keeping fitting here makes terminal resize immediate without a WASM event.
func renderDefaultStatusLine(root *sdk.UINode, width int) string {
	if width <= 0 {
		return ""
	}
	fields := make([]sdk.UINode, 0, len(root.Children))
	for _, node := range root.Children {
		if strings.HasPrefix(node.ID, "sl-sep") || node.ID == "sl-provider" {
			continue
		}
		node.Text = strings.TrimSpace(node.Text)
		fields = append(fields, node)
	}
	render := func() string {
		var parts []string
		for _, node := range fields {
			if node.Text != "" {
				parts = append(parts, renderNode(node, width))
			}
		}
		return strings.Join(parts, "  ")
	}
	for i := range fields {
		if fields[i].ID != "sl-cwd" || ansi.StringWidth(render()) <= width {
			continue
		}
		path := strings.TrimRight(fields[i].Text, "/")
		if slash := strings.LastIndex(path, "/"); slash >= 0 {
			prefix := "…/"
			if strings.HasPrefix(path, "~/") {
				prefix = "~/…/"
			}
			fields[i].Text = prefix + path[slash+1:]
		}
	}
	// Keep the model, current activity, and context until secondary fields go.
	for _, id := range []string{"sl-cwd", "sl-tps", "sl-compact", "sl-agent"} {
		if ansi.StringWidth(render()) <= width {
			break
		}
		for i := range fields {
			if fields[i].ID == id {
				fields[i].Text = ""
			}
		}
	}
	// Spend the remaining space on the model so the context suffix survives.
	for i := range fields {
		if fields[i].ID != "sl-model" {
			continue
		}
		overflow := ansi.StringWidth(render()) - width
		if overflow > 0 {
			budget := max(1, ansi.StringWidth(fields[i].Text)-overflow)
			fields[i].Text = ansi.Truncate(fields[i].Text, budget, "…")
		}
	}
	return ansi.Truncate(render(), width, "…")
}
