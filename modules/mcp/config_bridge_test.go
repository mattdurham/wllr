package mcp

import (
	"context"
	"encoding/json"
	"testing"
)

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

// TestBridge_Start_NoServers verifies Start returns nil when no servers are
// configured.
func TestBridge_Start_NoServers(t *testing.T) {
	withMCPHome(t) // no config file written

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
