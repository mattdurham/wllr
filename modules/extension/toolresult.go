package extension

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// toolResult holds the result of a tool execution sent back by an extension.
type toolResult struct {
	Result  string
	IsError bool
}

// ToolResult is the exported alias of toolResult for external dispatchers
// (modules/tools watchdog interface) that call ExecuteTool without importing
// wllr-internal names.
type ToolResult = toolResult
