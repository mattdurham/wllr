package main

// This file holds host-testable session I/O and normalization logic. It carries
// no build tag (unlike main.go/wllrsdk.go, which are wasip1-only) so it compiles
// on the host for unit tests — mirroring the tasks extension's claim.go pattern.

import (
	"encoding/json"
	"os"
	"strings"
)

func loadMessages(path string) ([]storedMsg, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	var out []storedMsg
	for _, line := range lines[1:] {
		var e messageEntry
		if json.Unmarshal([]byte(line), &e) != nil || e.Type != "message" || e.Role == "" {
			continue
		}
		if strings.TrimSpace(e.Content) == "" {
			continue // skip empty messages — API rejects them
		}
		// Enforce alternation: skip consecutive same-role messages.
		if len(out) > 0 && out[len(out)-1].role == e.Role {
			continue
		}
		out = append(out, storedMsg{role: e.Role, content: e.Content})
	}
	// API requires history to start with a user message.
	for len(out) > 0 && out[0].role != "user" {
		out = out[1:]
	}
	return out, nil
}

func sanitizePath(p string) string {
	if p == "" {
		return "--"
	}
	return strings.NewReplacer("/", "--", " ", "_").Replace(strings.TrimPrefix(p, "/"))
}
