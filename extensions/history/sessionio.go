package main

// This file holds host-testable session I/O and normalization logic. It carries
// no build tag (unlike main.go/wllrsdk.go, which are wasip1-only) so it compiles
// on the host for unit tests — mirroring the tasks extension's claim.go pattern.

import (
	"encoding/json"
	"os"
	"strings"
	"time"
)

// formatTimestamp renders an RFC3339Nano timestamp (as produced by the host
// for list_sessions / host_info) as a human "2006-01-02 15:04" picker label,
// falling back to the raw string if it does not parse.
func formatTimestamp(raw string) string {
	t, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return raw
	}
	return t.Format("2006-01-02 15:04")
}

// collapseWhitespace renders message content for picker previews: newlines and
// tabs become spaces, runs of spaces are squeezed to one, and surrounding
// whitespace is trimmed. Stored content is never rewritten — this is display
// normalization only.
func collapseWhitespace(s string) string {
	s = strings.NewReplacer("\n", " ", "\t", " ", "\r", " ").Replace(s)
	var b strings.Builder
	prevSpace := false
	for _, r := range s {
		if r == ' ' {
			if !prevSpace {
				b.WriteRune(r)
			}
			prevSpace = true
			continue
		}
		prevSpace = false
		b.WriteRune(r)
	}
	return strings.TrimSpace(b.String())
}

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
		content := strings.TrimSpace(e.Content)
		if content == "" {
			continue // skip empty messages — API rejects them
		}
		// Enforce alternation: skip consecutive same-role messages. Checked
		// after trimming so a skipped blank entry can't break the pairing.
		if len(out) > 0 && out[len(out)-1].role == e.Role {
			continue
		}
		out = append(out, storedMsg{role: e.Role, content: content})
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
