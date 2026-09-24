package mcp

import (
	"os"
	"path/filepath"
	"testing"
)

// withMCPHome redirects HOME to a fresh temp dir and returns the
// mcp-bridge extension config directory under it. LoadConfig resolves its
// file from os.UserHomeDir(), so this is the only seam tests need.
func withMCPHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	return filepath.Join(home, ".wllr", "extensions", "mcp-bridge")
}

// writeMCPConfig writes the extension's config.yaml under the redirected home.
func writeMCPConfig(t *testing.T, content string) {
	t.Helper()
	dir := withMCPHome(t)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestLoadConfig_Missing returns an empty config when no file exists.
func TestLoadConfig_Missing(t *testing.T) {
	withMCPHome(t) // no config file written

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig with missing file: %v", err)
	}
	if cfg.MCPServers == nil {
		t.Fatal("MCPServers should not be nil after LoadConfig")
	}
	if len(cfg.MCPServers) != 0 {
		t.Errorf("expected 0 servers, got %d", len(cfg.MCPServers))
	}
}

// TestLoadConfig_EmptyFile returns an empty config for an empty document.
func TestLoadConfig_EmptyFile(t *testing.T) {
	writeMCPConfig(t, "")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if len(cfg.MCPServers) != 0 {
		t.Errorf("expected 0 servers, got %d", len(cfg.MCPServers))
	}
}

// TestLoadConfig_UnrelatedKeys returns an empty config when the file holds
// only unknown keys: extension config files are not shared pools.
func TestLoadConfig_UnrelatedKeys(t *testing.T) {
	writeMCPConfig(t, "other-key: {}\n")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if len(cfg.MCPServers) != 0 {
		t.Errorf("expected 0 servers, got %d", len(cfg.MCPServers))
	}
}

// TestLoadConfig_WithServers parses a config with a servers section.
func TestLoadConfig_WithServers(t *testing.T) {
	writeMCPConfig(t, `servers:
  filesystem:
    command: npx
    args: ["-y", "@modelcontextprotocol/server-filesystem", "/tmp"]
    env:
      NODE_ENV: production
`)

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if len(cfg.MCPServers) != 1 {
		t.Fatalf("expected 1 server, got %d", len(cfg.MCPServers))
	}

	srv, ok := cfg.MCPServers["filesystem"]
	if !ok {
		t.Fatal("expected 'filesystem' server")
	}
	if srv.Command != "npx" {
		t.Errorf("command: got %q, want %q", srv.Command, "npx")
	}
	if len(srv.Args) != 3 {
		t.Errorf("args len: got %d, want 3", len(srv.Args))
	}
	if srv.Env["NODE_ENV"] != "production" {
		t.Errorf("env NODE_ENV: got %q, want %q", srv.Env["NODE_ENV"], "production")
	}
}

// TestLoadConfig_LegacyMCPServers accepts the shared-config-era mcpServers
// key, which the startup migration preserves when it moves an mcp-bridge
// group into the extension's own file.
func TestLoadConfig_LegacyMCPServers(t *testing.T) {
	writeMCPConfig(t, `mcpServers:
  filesystem:
    command: npx
    args: ["-y", "@modelcontextprotocol/server-filesystem", "/tmp"]
`)

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if len(cfg.MCPServers) != 1 {
		t.Fatalf("expected 1 server, got %d", len(cfg.MCPServers))
	}
	if cfg.MCPServers["filesystem"].Command != "npx" {
		t.Errorf("command: got %q, want %q", cfg.MCPServers["filesystem"].Command, "npx")
	}
}

// TestLoadConfig_ServersKeyWins gives the canonical servers key precedence
// when both it and the legacy alias are present.
func TestLoadConfig_ServersKeyWins(t *testing.T) {
	writeMCPConfig(t, `servers:
  current:
    command: /bin/current
mcpServers:
  legacy:
    command: /bin/legacy
`)

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if len(cfg.MCPServers) != 1 {
		t.Fatalf("expected 1 server, got %d", len(cfg.MCPServers))
	}
	if _, ok := cfg.MCPServers["current"]; !ok {
		t.Fatalf("expected 'current' server, got %v", cfg.MCPServers)
	}
}

// TestLoadConfig_NullServers keeps the map non-nil when servers is present
// but empty or null.
func TestLoadConfig_NullServers(t *testing.T) {
	writeMCPConfig(t, "servers:\n")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.MCPServers == nil {
		t.Error("MCPServers should not be nil after LoadConfig")
	}
}

// TestLoadConfig_InvalidYAML returns an error for malformed YAML.
func TestLoadConfig_InvalidYAML(t *testing.T) {
	writeMCPConfig(t, "not: [valid: yaml")

	if _, err := LoadConfig(); err == nil {
		t.Fatal("expected error for invalid YAML, got nil")
	}
}

// TestLoadConfig_ServersNotAMap returns an error when servers is a scalar.
func TestLoadConfig_ServersNotAMap(t *testing.T) {
	writeMCPConfig(t, "servers: not a map\n")

	_, err := LoadConfig()
	if err == nil {
		t.Fatal("expected error for non-map servers value, got nil")
	}
}

// TestConfigPath_Default verifies the path is the mcp-bridge extension's own
// config file under the wllr home.
func TestConfigPath_Default(t *testing.T) {
	home := withMCPHome(t)
	got := configPath()
	if got != filepath.Join(home, "config.yaml") {
		t.Errorf("configPath: got %q, want %q", got, filepath.Join(home, "config.yaml"))
	}
}
