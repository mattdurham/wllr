package mcp

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"fmt"
)

// JSONRPCRequest represents a JSON-RPC 2.0 request.

// JSONRPCResponse represents a JSON-RPC 2.0 response.

// JSONRPCError represents a JSON-RPC 2.0 error object.

func (e *JSONRPCError) Error() string {
	if len(e.Data) > 0 {
		return fmt.Sprintf("jsonrpc error %d: %s (data: %s)", e.Code, e.Message, string(e.Data))
	}
	return fmt.Sprintf("jsonrpc error %d: %s", e.Code, e.Message)
}

// InitializeParams are the parameters for the initialize request.

// ClientInfo describes the client application.

// InitializeResult is the result of the initialize request.

// ServerInfo describes the MCP server.

// ListToolsResult is the result of tools/list.

// Tool describes a tool provided by an MCP server.

// CallToolParams are the parameters for tools/call.

// CallToolResult is the result of tools/call.

// ContentItem represents a piece of content returned by a tool.

// "text", "image", "resource"

// base64 for images
// Add other fields as needed
