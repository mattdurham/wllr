package mcp

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// TestProtocolTypes verifies JSON marshaling of protocol types.
func TestProtocolTypes(t *testing.T) {
	// Test JSONRPCRequest
	req := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "initialize",
		Params: InitializeParams{
			ProtocolVersion: "2024-11-05",
			Capabilities:    map[string]any{},
			ClientInfo: ClientInfo{
				Name:    "wllr",
				Version: "0.1.0",
			},
		},
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	var decoded JSONRPCRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}

	if decoded.Method != "initialize" {
		t.Errorf("expected method initialize, got %s", decoded.Method)
	}
}

// TestConfigLoading tests config parsing.
func TestConfigLoading(t *testing.T) {
	// Test with empty config
	cfg := &Config{
		MCPServers: map[string]ServerConfig{
			"test": {
				Command: "echo",
				Args:    []string{"hello"},
				Env:     map[string]string{"FOO": "bar"},
			},
		},
	}

	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}

	var decoded Config
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}

	if len(decoded.MCPServers) != 1 {
		t.Errorf("expected 1 server, got %d", len(decoded.MCPServers))
	}

	srv, ok := decoded.MCPServers["test"]
	if !ok {
		t.Fatal("missing test server")
	}

	if srv.Command != "echo" {
		t.Errorf("expected command echo, got %s", srv.Command)
	}
}

// TestBridge tests basic bridge operations without real MCP servers.
func TestBridge(t *testing.T) {
	bridge := NewBridge()

	// Test empty bridge
	tools := bridge.RegisterTools()
	if len(tools) != 0 {
		t.Errorf("expected 0 tools, got %d", len(tools))
	}

	// Test CallTool on non-existent tool
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := bridge.CallTool(ctx, "nonexistent", nil)
	if err == nil {
		t.Error("expected error calling nonexistent tool")
	}
}

// TestToolResultFormatting tests the formatToolResult function.
func TestToolResultFormatting(t *testing.T) {
	tests := []struct {
		name     string
		result   CallToolResult
		expected string
	}{
		{
			name:     "empty",
			result:   CallToolResult{Content: []ContentItem{}},
			expected: "",
		},
		{
			name: "single text",
			result: CallToolResult{
				Content: []ContentItem{
					{Type: "text", Text: "hello world"},
				},
			},
			expected: "hello world",
		},
		{
			name: "multiple text items",
			result: CallToolResult{
				Content: []ContentItem{
					{Type: "text", Text: "hello "},
					{Type: "text", Text: "world"},
				},
			},
			expected: "hello world",
		},
		{
			name: "mixed content types",
			result: CallToolResult{
				Content: []ContentItem{
					{Type: "text", Text: "text content"},
					{Type: "image", Data: "base64data"},
				},
			},
			expected: "text content",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatToolResult(&tt.result)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}
