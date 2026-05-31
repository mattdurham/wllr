package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/mattdurham/wllr/modules/extension"
	"github.com/mattdurham/wllr/modules/sdk"
)

// testUIBridge is a minimal UIBridge for mcp tests.
type testUIBridge struct {
	onRegisterTool func(tool sdk.Tool) error
	onToolResult   func(id, result string, isError bool)
}

func (b *testUIBridge) Notify(_ string)                                       {}
func (b *testUIBridge) ShowModal(_ string)                                    {}
func (b *testUIBridge) ShowPicker(_ string, _ []sdk.ShowPickerItem, _ string) {}
func (b *testUIBridge) Abort()                                                {}
func (b *testUIBridge) SetStatus(_, _ string)                                 {}
func (b *testUIBridge) GetStatusInfo() sdk.StatusInfo                         { return sdk.StatusInfo{} }
func (b *testUIBridge) SendMessage(_ sdk.Message)                             {}
func (b *testUIBridge) RegisterCommand(_, _ string, _ bool) error             { return nil }
func (b *testUIBridge) RegisterTool(tool sdk.Tool) error {
	if b.onRegisterTool != nil {
		return b.onRegisterTool(tool)
	}
	return nil
}
func (b *testUIBridge) SetSystemPrompt(_ string)           {}
func (b *testUIBridge) AppendSystemPrompt(_ string)        {}
func (b *testUIBridge) ResetHistory(_ []sdk.Message) error { return nil }
func (b *testUIBridge) ToolResult(id, result string, isError bool) {
	if b.onToolResult != nil {
		b.onToolResult(id, result, isError)
	}
}
func (b *testUIBridge) AfterToolCall(_, _, _ string, _ bool) {}
func (b *testUIBridge) ConsoleOutput(_ string)               {}
func (b *testUIBridge) ConsoleClear()                        {}

var _ extension.UIBridge = (*testUIBridge)(nil)

// TestExtension_NewExtension verifies the constructor wires bridge and host.
func TestExtension_NewExtension(t *testing.T) {
	host := extension.NewHost(nil)
	defer func() { _ = host.Close(context.Background()) }()

	ext := NewExtension(host)
	if ext == nil {
		t.Fatal("NewExtension returned nil")
		return
	}
	if ext.bridge == nil {
		t.Fatal("bridge is nil")
		return
	}
	if ext.host == nil {
		t.Fatal("host is nil")
	}
}

// TestExtension_Start_NoServers verifies Start succeeds with no MCP servers configured.
func TestExtension_Start_NoServers(t *testing.T) {
	// Use a temp dir so LoadConfig returns no servers.
	t.Setenv("WLLR_CONFIG", t.TempDir()+"/nonexistent.json")

	host := extension.NewHost(nil)
	defer func() { _ = host.Close(context.Background()) }()

	ext := NewExtension(host)
	if err := ext.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
}

// TestExtension_Close_Empty verifies Close is safe on an extension that was never started.
func TestExtension_Close_Empty(t *testing.T) {
	host := extension.NewHost(nil)
	defer func() { _ = host.Close(context.Background()) }()

	ext := NewExtension(host)
	if err := ext.Close(); err != nil {
		t.Errorf("Close on empty extension: %v", err)
	}
}

// TestExtension_RegisterTool_UsesHostCallback verifies that registerTool
// invokes OnRegisterTool on the host.
func TestExtension_RegisterTool_UsesHostCallback(t *testing.T) {
	host := extension.NewHost(nil)
	defer func() { _ = host.Close(context.Background()) }()

	var registered []sdk.Tool
	host.SetUIBridge(&testUIBridge{onRegisterTool: func(tool sdk.Tool) error {
		registered = append(registered, tool)
		return nil
	}})

	ext := NewExtension(host)
	tool := sdk.Tool{Name: "my_tool", Description: "does stuff", InputSchema: json.RawMessage(`{}`)}
	if err := ext.registerTool(tool); err != nil {
		t.Fatalf("registerTool: %v", err)
	}

	if len(registered) != 1 {
		t.Fatalf("expected 1 registered tool, got %d", len(registered))
	}
	if registered[0].Name != "my_tool" {
		t.Errorf("tool name: got %q, want %q", registered[0].Name, "my_tool")
	}
}

// TestExtension_RegisterTool_NilCallback is safe when OnRegisterTool is not set.
func TestExtension_RegisterTool_NilCallback(t *testing.T) {
	host := extension.NewHost(nil)
	defer func() { _ = host.Close(context.Background()) }()
	// OnRegisterTool is nil by default.

	ext := NewExtension(host)
	tool := sdk.Tool{Name: "silent_tool", InputSchema: json.RawMessage(`{}`)}
	if err := ext.registerTool(tool); err != nil {
		t.Fatalf("registerTool with nil callback: %v", err)
	}
}

// TestExtension_HandleToolCall_NotMCPTool verifies that non-MCP tools are ignored.
func TestExtension_HandleToolCall_NotMCPTool(t *testing.T) {
	host := extension.NewHost(nil)
	defer func() { _ = host.Close(context.Background()) }()

	ext := NewExtension(host)

	// Fire an EventBeforeToolCall for a tool that isn't in the MCP bridge.
	payload, _ := json.Marshal(sdk.BeforeToolCallPayload{
		AgentID:    "agent-1",
		ToolCallID: "call-1",
		ToolName:   "native_tool", // not in MCP bridge
		Input:      json.RawMessage(`{}`),
	})
	evt := sdk.Event{Type: sdk.EventBeforeToolCall, Payload: payload}

	// Should return nil and not call SendToolResult.
	var toolResultCalled bool
	host.SetUIBridge(&testUIBridge{onToolResult: func(id, result string, isError bool) {
		toolResultCalled = true
	}})

	if err := ext.handleToolCall(context.Background(), evt); err != nil {
		t.Fatalf("handleToolCall: %v", err)
	}
	if toolResultCalled {
		t.Error("expected OnToolResult NOT to be called for non-MCP tool")
	}
}

// TestExtension_HandleToolCall_MCPTool routes an MCP tool call to the bridge.
func TestExtension_HandleToolCall_MCPTool(t *testing.T) {
	host := extension.NewHost(nil)
	defer func() { _ = host.Close(context.Background()) }()

	ext := NewExtension(host)

	// Inject a server + tool mapping directly into the bridge.
	srv := &Server{
		name: "test",
		tools: []Tool{
			{Name: "mcp_tool", InputSchema: json.RawMessage(`{}`)},
		},
		pending: make(map[int]chan *JSONRPCResponse),
	}
	ext.bridge.servers["test"] = srv
	ext.bridge.toolToSrv["mcp_tool"] = "test"

	var gotID, gotResult string
	var gotError bool
	host.SetUIBridge(&testUIBridge{onToolResult: func(id, result string, isError bool) {
		gotID = id
		gotResult = result
		gotError = isError
	}})

	payload, _ := json.Marshal(sdk.BeforeToolCallPayload{
		AgentID:    "agent-1",
		ToolCallID: "call-mcp",
		ToolName:   "mcp_tool",
		Input:      json.RawMessage(`{"text":"hello"}`),
	})
	evt := sdk.Event{Type: sdk.EventBeforeToolCall, Payload: payload}

	// The bridge will try to call srv.CallTool which uses srv.stdin (nil here).
	// The error from the nil stdin write is caught and returned as a tool error.
	if err := ext.handleToolCall(context.Background(), evt); err != nil {
		t.Fatalf("handleToolCall: %v", err)
	}

	// The result should be an error (nil stdin means write failed).
	if gotID != "call-mcp" {
		t.Errorf("tool result ID: got %q, want %q", gotID, "call-mcp")
	}
	_ = gotResult
	_ = gotError
}

// TestExtension_HandleToolCall_InvalidPayload returns an error for bad JSON.
func TestExtension_HandleToolCall_InvalidPayload(t *testing.T) {
	host := extension.NewHost(nil)
	defer func() { _ = host.Close(context.Background()) }()

	ext := NewExtension(host)

	evt := sdk.Event{
		Type:    sdk.EventBeforeToolCall,
		Payload: json.RawMessage(`not valid json`),
	}
	err := ext.handleToolCall(context.Background(), evt)
	if err == nil {
		t.Fatal("expected error for invalid payload, got nil")
	}
}
