package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// ServerConfig defines the configuration for a single MCP server.
type ServerConfig struct {
	Command string            `json:"command"` // Command to execute
	Args    []string          `json:"args"`    // Command arguments
	Env     map[string]string `json:"env"`     // Additional environment variables
}

// Config holds the configuration for all MCP servers.
type Config struct {
	MCPServers map[string]ServerConfig `json:"mcpServers"`
}

// LoadConfig loads MCP server configuration from the wllr config file.
// It looks for the "mcp-bridge" key in the shared config.
func LoadConfig() (*Config, error) {
	path := configPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// No config file means no MCP servers configured
			return &Config{MCPServers: make(map[string]ServerConfig)}, nil
		}
		return nil, fmt.Errorf("read config: %w", err)
	}

	// Parse the config file as a flat map
	var all map[string]json.RawMessage
	if err := json.Unmarshal(data, &all); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	// Look for mcp-bridge key
	mcpData, ok := all["mcp-bridge"]
	if !ok {
		return &Config{MCPServers: make(map[string]ServerConfig)}, nil
	}

	var cfg Config
	if err := json.Unmarshal(mcpData, &cfg); err != nil {
		return nil, fmt.Errorf("parse mcp-bridge config: %w", err)
	}

	if cfg.MCPServers == nil {
		cfg.MCPServers = make(map[string]ServerConfig)
	}

	return &cfg, nil
}

// configPath returns the path to the shared wllr config file.
func configPath() string {
	if p := os.Getenv("WLLR_CONFIG"); p != "" {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".wllr/config.json"
	}
	return filepath.Join(home, ".config", "wllr", "config.json")
}
