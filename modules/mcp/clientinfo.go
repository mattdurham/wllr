package mcp

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// ClientInfo describes the client application.
type ClientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}
