package mcp

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// InitializeParams are the parameters for the initialize request.
type InitializeParams struct {
	ProtocolVersion string         `json:"protocolVersion"`
	Capabilities    map[string]any `json:"capabilities"`
	ClientInfo      ClientInfo     `json:"clientInfo"`
}
