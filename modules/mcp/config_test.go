package mcp

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLoadConfig_NilMCPServers verifies that a config with an mcp-bridge section
// that omits mcpServers gets a non-nil map.
func TestLoadConfig_NilMCPServers(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.json")
	if err := os.WriteFile(p, []byte(`{"mcp-bridge":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WLLR_CONFIG", p)

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.MCPServers == nil {
		t.Error("MCPServers should not be nil after LoadConfig")
	}
}
