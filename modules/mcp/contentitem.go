package mcp

// ContentItem represents a piece of content returned by a tool.
type ContentItem struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
	Data string `json:"data,omitempty"`
}
