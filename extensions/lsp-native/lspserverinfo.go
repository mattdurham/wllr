package main

// LSPServerInfo describes a configured native LSP server.
type LSPServerInfo struct {
	ID           string   `json:"id"`
	Command      string   `json:"command"`
	Args         []string `json:"args,omitempty"`
	Role         string   `json:"role"`
	FilePatterns []string `json:"file_patterns,omitempty"`
	Languages    []string `json:"languages,omitempty"`
}
