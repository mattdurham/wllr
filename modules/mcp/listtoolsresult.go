package mcp

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// ListToolsResult is the result of tools/list.
type ListToolsResult struct {
	Tools []Tool `json:"tools"`
}
