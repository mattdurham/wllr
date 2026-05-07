package mcp

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/mattdurham/wllr/extension"
	"github.com/mattdurham/wllr/sdk"
)

// Extension wraps the MCP bridge to integrate with wllr's extension system.
type Extension struct {
	bridge *Bridge
	host   *extension.Host
}

// NewExtension creates a new MCP extension wrapper.
func NewExtension(host *extension.Host) *Extension {
	return &Extension{
		bridge: NewBridge(),
		host:   host,
	}
}

// Start initializes and starts the MCP bridge, then registers tools with the host.
func (e *Extension) Start(ctx context.Context) error {
	// Start the bridge (loads config and spawns servers)
	if err := e.bridge.Start(ctx); err != nil {
		return err
	}

	// Register all MCP tools with the host
	tools := e.bridge.RegisterTools()
	for _, tool := range tools {
		if err := e.registerTool(tool); err != nil {
			slog.Warn("mcp: failed to register tool", "tool", tool.Name, "error", err)
		}
	}

	// Subscribe to before_tool_call events to intercept MCP tool calls
	e.host.Bus.Subscribe(sdk.EventBeforeToolCall, e.handleToolCall)

	slog.Info("mcp: extension started", "tools", len(tools))
	return nil
}

// registerTool registers a tool with the host using OnRegisterTool callback.
func (e *Extension) registerTool(tool sdk.Tool) error {
	if e.host.OnRegisterTool != nil {
		return e.host.OnRegisterTool(tool)
	}
	return nil
}

// handleToolCall intercepts tool calls and routes MCP tools to the bridge.
func (e *Extension) handleToolCall(ctx context.Context, evt sdk.Event) error {
	var payload sdk.BeforeToolCallPayload
	if err := json.Unmarshal(evt.Payload, &payload); err != nil {
		return err
	}

	// Check if this tool belongs to an MCP server
	e.bridge.mu.RLock()
	_, isMCPTool := e.bridge.toolToSrv[payload.ToolName]
	e.bridge.mu.RUnlock()

	if !isMCPTool {
		// Not an MCP tool, let other handlers deal with it
		return nil
	}

	// Parse arguments
	var args map[string]interface{}
	if len(payload.Input) > 0 {
		if err := json.Unmarshal(payload.Input, &args); err != nil {
			e.host.SendToolResult(payload.ToolCallID, err.Error(), true)
			return nil
		}
	}

	// Call the tool via the bridge
	result, err := e.bridge.CallTool(ctx, payload.ToolName, args)
	isError := err != nil
	if err != nil {
		result = err.Error()
	}

	// Send the result back to the host
	e.host.SendToolResult(payload.ToolCallID, result, isError)
	return nil
}

// Close stops the MCP bridge and cleans up.
func (e *Extension) Close() error {
	return e.bridge.Close()
}
