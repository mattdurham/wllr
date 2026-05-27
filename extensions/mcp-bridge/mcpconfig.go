package main

// mcpConfig is the top-level config structure.
type mcpConfig struct {
	Servers map[string]mcpServerConfig `json:"servers"`
}
