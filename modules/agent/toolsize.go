package agent

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// toolsize.go holds the attribution value type returned inside
// ContextBreakdown (ToolsByTool, CanonicalByTool). It lives apart from
// contextbreakdown.go because that file's revive max-public-structs budget is
// spent on ContextBreakdown itself.

// ToolSize is one tool's attributed size: tool-definition tokens for the
// resident context (ToolsByTool), transcript chars for the canonical,
// never-resident side (CanonicalByTool). Extension names the extension that
// registered the tool; empty for harness-native tools the renderer labels
// "harness".
type ToolSize struct {
	Name      string
	Extension string
	Tokens    int64 // tool-definition tokens (ToolsByTool)
	Chars     int64 // transcript chars (CanonicalByTool)
}
