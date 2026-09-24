package mcp

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"fmt"
	"os"
	"path/filepath"

	yaml "gopkg.in/yaml.v3"
)

// Config holds the configuration for all MCP servers. It is the mcp-bridge
// extension's own config: the file's contents ARE the config.
type Config struct {
	MCPServers map[string]ServerConfig `json:"servers"`
}

// LoadConfig loads MCP server configuration from the mcp-bridge extension's
// own config file, <wllr home>/extensions/mcp-bridge/config.yaml. Extension
// configs live beside their WASM, never in the app config file, so there is
// no shared-config fallback.
//
// Servers are declared under the `servers` key as name -> {command, args, env}.
// The legacy `mcpServers` key (the shared-config-era name, which the startup
// migration preserves verbatim when moving an mcp-bridge group into this file)
// is accepted when `servers` is absent.
//
// A missing file is an empty config: no MCP servers configured is a fine
// default state.
func LoadConfig() (*Config, error) {
	path := configPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Config{MCPServers: make(map[string]ServerConfig)}, nil
		}
		return nil, fmt.Errorf("read config: %w", err)
	}

	var raw struct {
		Servers    map[string]ServerConfig `yaml:"servers"`
		MCPServers map[string]ServerConfig `yaml:"mcpServers"`
	}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	servers := raw.Servers
	if servers == nil {
		servers = raw.MCPServers
	}
	if servers == nil {
		servers = make(map[string]ServerConfig)
	}
	return &Config{MCPServers: servers}, nil
}

// configPath returns the mcp-bridge extension's own config file. It matches
// the host's per-extension layout so config_read and this loader always see
// the same file. WLLR_CONFIG intentionally does not apply: it relocates the
// app config file, not extension configs.
func configPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".wllr", "extensions", "mcp-bridge", "config.yaml")
	}
	return filepath.Join(home, ".wllr", "extensions", "mcp-bridge", "config.yaml")
}
