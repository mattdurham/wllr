package mcp

import "encoding/json"

// JSONRPCError represents a JSON-RPC 2.0 error object.
type JSONRPCError struct {
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
	Code    int             `json:"code"`
}
