package mcp

// JSONRPCRequest represents a JSON-RPC 2.0 request.
type JSONRPCRequest struct {
	Params  interface{} `json:"params,omitempty"`
	JSONRPC string      `json:"jsonrpc"`
	Method  string      `json:"method"`
	ID      int         `json:"id,omitempty"`
}
