package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestLoadConfig_Missing returns an empty config when no file exists.
func TestLoadConfig_Missing(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("WLLR_CONFIG", filepath.Join(dir, "nonexistent.json"))

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig with missing file: %v", err)
	}
	if len(cfg.MCPServers) != 0 {
		t.Errorf("expected 0 servers, got %d", len(cfg.MCPServers))
	}
}

// TestLoadConfig_EmptyFile returns an empty config for a valid JSON file
// that has no mcp-bridge key.
func TestLoadConfig_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{"other-key": {}}`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("WLLR_CONFIG", path)

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if len(cfg.MCPServers) != 0 {
		t.Errorf("expected 0 servers, got %d", len(cfg.MCPServers))
	}
}

// TestLoadConfig_WithServers parses a config containing an mcp-bridge section.
func TestLoadConfig_WithServers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	raw := `{
		"mcp-bridge": {
			"mcpServers": {
				"filesystem": {
					"command": "npx",
					"args": ["-y", "@modelcontextprotocol/server-filesystem", "/tmp"],
					"env": {"NODE_ENV": "production"}
				}
			}
		}
	}`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("WLLR_CONFIG", path)

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

// TestLoadConfig_InvalidJSON returns an error for malformed JSON.
func TestLoadConfig_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`not json`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("WLLR_CONFIG", path)

	_, err := LoadConfig()
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

// TestLoadConfig_InvalidMCPBridgeSection returns an error when mcp-bridge value is not valid.
func TestLoadConfig_InvalidMCPBridgeSection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	// mcp-bridge value is a string, not an object.
	if err := os.WriteFile(path, []byte(`{"mcp-bridge": "not an object"}`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("WLLR_CONFIG", path)

	_, err := LoadConfig()
	if err == nil {
		t.Fatal("expected error for invalid mcp-bridge value, got nil")
	}
}

// TestConfigPath_EnvOverride verifies WLLR_CONFIG overrides the default path.
func TestConfigPath_EnvOverride(t *testing.T) {
	const want = "/tmp/custom-wllr-config.json"
	t.Setenv("WLLR_CONFIG", want)
	got := configPath()
	if got != want {
		t.Errorf("configPath: got %q, want %q", got, want)
	}
}

// TestConfigPath_Default verifies the default path ends in the expected suffix.
func TestConfigPath_Default(t *testing.T) {
	t.Setenv("WLLR_CONFIG", "")
	got := configPath()
	if got == "" {
		t.Fatal("configPath returned empty string")
	}
	if filepath.Base(got) != "config.json" {
		t.Errorf("configPath base: got %q, want config.json", filepath.Base(got))
	}
}

// TestBridge_RegisterTools_Populated verifies RegisterTools returns tools from servers.
func TestBridge_RegisterTools_Populated(t *testing.T) {
	b := NewBridge()

	srv := &Server{
		name: "test",
		tools: []Tool{
			{Name: "list_files", Description: "list files", InputSchema: json.RawMessage(`{}`)},
			{Name: "read_file", Description: "read a file", InputSchema: json.RawMessage(`{}`)},
		},
		pending: make(map[int]chan *JSONRPCResponse),
	}
	b.servers["test"] = srv
	b.toolToSrv["list_files"] = "test"
	b.toolToSrv["read_file"] = "test"

	tools := b.RegisterTools()
	if len(tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(tools))
	}

	names := make(map[string]bool)
	for _, tool := range tools {
		names[tool.Name] = true
	}
	if !names["list_files"] || !names["read_file"] {
		t.Errorf("missing expected tools, got %v", names)
	}
}

// TestBridge_CallTool_ToolNotFound ensures an error is returned for unknown tools.
func TestBridge_CallTool_ToolNotFound(t *testing.T) {
	b := NewBridge()
	_, err := b.CallTool(context.Background(), "nonexistent", nil)
	if err == nil {
		t.Fatal("expected error for nonexistent tool, got nil")
	}
}

// TestBridge_CallTool_ServerNotFound ensures an error is returned when the
// server entry is missing from b.servers but present in b.toolToSrv.
func TestBridge_CallTool_ServerNotFound(t *testing.T) {
	b := NewBridge()
	b.toolToSrv["orphan_tool"] = "ghost-server"

	_, err := b.CallTool(context.Background(), "orphan_tool", nil)
	if err == nil {
		t.Fatal("expected error for orphaned tool, got nil")
	}
}

// TestBridge_Close_Empty verifies Close is a no-op on an empty bridge.
func TestBridge_Close_Empty(t *testing.T) {
	b := NewBridge()
	if err := b.Close(); err != nil {
		t.Errorf("Close empty bridge: %v", err)
	}
}

// TestBridge_Start_NoServers verifies Start returns nil when no servers are configured.
func TestBridge_Start_NoServers(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("WLLR_CONFIG", filepath.Join(dir, "nonexistent.json"))

	b := NewBridge()
	if err := b.Start(context.Background()); err != nil {
		t.Fatalf("Start with no servers: %v", err)
	}
}

// TestFormatToolResult_IsError verifies the isError path returns content text.
func TestFormatToolResult_IsError(t *testing.T) {
	result := formatToolResult(&CallToolResult{
		IsError: true,
		Content: []ContentItem{{Type: "text", Text: "tool failed"}},
	})
	if result != "tool failed" {
		t.Errorf("formatToolResult isError: got %q, want %q", result, "tool failed")
	}
}

// TestFormatToolResult_NonTextSkipped verifies non-text content items are ignored.
func TestFormatToolResult_NonTextSkipped(t *testing.T) {
	result := formatToolResult(&CallToolResult{
		Content: []ContentItem{
			{Type: "image", Data: "base64data"},
			{Type: "text", Text: "actual text"},
		},
	})
	if result != "actual text" {
		t.Errorf("formatToolResult: got %q, want %q", result, "actual text")
	}
}
