package mcp

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// CallToolResult is the result of tools/call.
type CallToolResult struct {
	Content []ContentItem `json:"content,omitempty"`
	IsError bool          `json:"isError,omitempty"`
}
