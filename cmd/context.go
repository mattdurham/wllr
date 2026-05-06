package main

import (
	"os"
	"path/filepath"
	"strings"
)

// loadSystemPrompt reads AGENTS.md (falling back to CLAUDE.md) from
// ~/.wllr/ and the current working directory, combining both.
// Called directly in --exec mode; the TUI uses the context WASM extension.
func loadSystemPrompt() string {
	home, _ := os.UserHomeDir()

	var parts []string

	// Global context: ~/.wllr/AGENTS.md → ~/.wllr/CLAUDE.md
	for _, p := range []string{
		filepath.Join(home, ".wllr", "AGENTS.md"),
		filepath.Join(home, ".wllr", "CLAUDE.md"),
	} {
		if data, err := os.ReadFile(p); err == nil && len(data) > 0 {
			parts = append(parts, strings.TrimSpace(string(data)))
			break
		}
	}

	// CWD context: AGENTS.md → CLAUDE.md
	for _, p := range []string{"AGENTS.md", "CLAUDE.md"} {
		if data, err := os.ReadFile(p); err == nil && len(data) > 0 {
			parts = append(parts, strings.TrimSpace(string(data)))
			break
		}
	}

	return strings.Join(parts, "\n\n---\n\n")
}

