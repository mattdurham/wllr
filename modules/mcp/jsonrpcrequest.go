package mcp

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// JSONRPCRequest represents a JSON-RPC 2.0 request.
type JSONRPCRequest struct {
	Params  interface{} `json:"params,omitempty"`
	JSONRPC string      `json:"jsonrpc"`
	Method  string      `json:"method"`
	ID      int         `json:"id,omitempty"`
}
