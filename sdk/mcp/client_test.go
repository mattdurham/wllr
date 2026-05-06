package mcp_test

import (
	"encoding/json"
	"testing"

	"github.com/mattdurham/wllr/sdk/mcp"
)

// TestClientHandshake verifies the MCP client can spawn a server and complete handshake.
// This test requires Node.js and npx to be available.
func TestClientHandshake(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}


	// Spawn the MCP filesystem server (requires Node.js + npx).
	client, err := mcp.NewClient("npx", "-y", "@modelcontextprotocol/server-filesystem", "/tmp")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer client.Close()

	// Perform initialization handshake.
	serverInfo, err := client.Initialize(mcp.ClientInfo{
		Name:    "wllr-test",
		Version: "0.1.0",
	})
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	if serverInfo.Name == "" {
		t.Error("expected non-empty server name")
	}
	t.Logf("Connected to MCP server: %s v%s", serverInfo.Name, serverInfo.Version)

	// List available tools.
	tools, err := client.ListTools()
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	if len(tools) == 0 {
		t.Error("expected at least one tool from filesystem server")
	}

	for _, tool := range tools {
		t.Logf("Tool: %s - %s", tool.Name, tool.Description)
		if tool.InputSchema != nil {
			var schema map[string]any
			if err := json.Unmarshal(tool.InputSchema, &schema); err != nil {
				t.Errorf("tool %q has invalid input schema: %v", tool.Name, err)
			}
		}
	}
}

// TestClientToolCall verifies a tool call to an MCP server.
func TestClientToolCall(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}


	client, err := mcp.NewClient("npx", "-y", "@modelcontextprotocol/server-filesystem", "/tmp")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer client.Close()

	if _, err := client.Initialize(mcp.ClientInfo{Name: "wllr-test", Version: "0.1.0"}); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	// Call a tool (list_directory on /tmp).
	result, err := client.CallTool("list_directory", map[string]any{"path": "/tmp"})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}

	if result.IsError {
		t.Errorf("tool call returned error: %+v", result.Content)
	}

	if len(result.Content) == 0 {
		t.Error("expected content in tool result")
	}

	for _, item := range result.Content {
		if item.Type == "text" {
			t.Logf("Result: %s", item.Text)
		}
	}
}
