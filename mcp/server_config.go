package mcp

// ServerConfig defines the configuration for a single MCP server.
type ServerConfig struct {
	Env     map[string]string `json:"env"`     // Additional environment variables
	Command string            `json:"command"` // Command to execute
	Args    []string          `json:"args"`    // Command arguments
}
