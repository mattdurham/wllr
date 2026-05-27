package mcp

// ServerConfig defines the configuration for a single MCP server.
type ServerConfig struct {
	Env     map[string]string `json:"env"`
	Command string            `json:"command"`
	Args    []string          `json:"args"`
}
