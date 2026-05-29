package mcp

// CallToolParams are the parameters for tools/call.
type CallToolParams struct {
	Arguments map[string]interface{} `json:"arguments,omitempty"`
	Name      string                 `json:"name"`
}
