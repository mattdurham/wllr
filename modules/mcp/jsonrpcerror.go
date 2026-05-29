package mcp

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import "encoding/json"

// JSONRPCError represents a JSON-RPC 2.0 error object.
type JSONRPCError struct {
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
	Code    int             `json:"code"`
}
