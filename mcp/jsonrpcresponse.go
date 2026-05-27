package mcp

import "encoding/json"

// JSONRPCResponse represents a JSON-RPC 2.0 response.
type JSONRPCResponse struct {
	Error   *JSONRPCError   `json:"error,omitempty"`
	JSONRPC string          `json:"jsonrpc"`
	Result  json.RawMessage `json:"result,omitempty"`
	ID      int             `json:"id,omitempty"`
}
