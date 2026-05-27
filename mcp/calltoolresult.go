package mcp

// CallToolResult is the result of tools/call.
type CallToolResult struct {
	Content []ContentItem `json:"content,omitempty"`
	IsError bool          `json:"isError,omitempty"`
}
