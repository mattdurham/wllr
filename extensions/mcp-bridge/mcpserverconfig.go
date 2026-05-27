package main

// mcpServerConfig defines a single MCP server in the extension config.
type mcpServerConfig struct {
	Command string            `json:"command"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
}
