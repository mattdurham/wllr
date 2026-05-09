//go:build wasip1

// Package main is the context built-in extension for the wllr coding harness.
// On session_start it reads AGENTS.md (falling back to CLAUDE.md) from
// ~/.wllr/ and the current working directory, then injects the combined
// content as the agent system prompt via set_system_prompt.
//
// Lookup order (first match wins for each scope):
//
//	Global: ~/.wllr/AGENTS.md → ~/.wllr/CLAUDE.md
//	CWD:    ./AGENTS.md        → ./CLAUDE.md
//
// Both global and CWD content are combined (CWD appended after global).
package main

import (
	"os"
	"path/filepath"
	"strings"
)

func init() {
	OnSessionStart(onSessionStart)
}

func onSessionStart() {
	var parts []string

	// Global context: ~/.wllr/AGENTS.md or CLAUDE.md
	if content := readFirst(globalPaths()); content != "" {
		parts = append(parts, content)
	}

	// CWD context: ./AGENTS.md or CLAUDE.md
	if content := readFirst([]string{"AGENTS.md", "CLAUDE.md"}); content != "" {
		parts = append(parts, content)
	}

	if len(parts) == 0 {
		return
	}

	prompt := strings.Join(parts, "\n\n---\n\n")
	Logf(1, "context: loaded system prompt (%d bytes)", len(prompt))
	SetSystemPrompt(prompt)
}

func globalPaths() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return []string{
		filepath.Join(home, ".wllr", "AGENTS.md"),
		filepath.Join(home, ".wllr", "CLAUDE.md"),
	}
}

func readFirst(paths []string) string {
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err == nil && len(data) > 0 {
			Log(1, "context: loaded "+p)
			return strings.TrimSpace(string(data))
		}
	}
	return ""
}

func main() {}
