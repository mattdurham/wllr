package mcp

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// CallToolParams are the parameters for tools/call.
type CallToolParams struct {
	Arguments map[string]interface{} `json:"arguments,omitempty"`
	Name      string                 `json:"name"`
}
