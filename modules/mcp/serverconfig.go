package mcp

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// ServerConfig defines the configuration for a single MCP server.
type ServerConfig struct {
	Env     map[string]string `json:"env"`
	Command string            `json:"command"`
	Args    []string          `json:"args"`
}
