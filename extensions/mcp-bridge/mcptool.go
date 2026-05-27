package main

import "encoding/json"

// mcpTool represents a tool discovered from an MCP server.
type mcpTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}
