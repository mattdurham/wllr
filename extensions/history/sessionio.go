package main

// This file holds host-testable session I/O and normalization logic. It carries
// no build tag (unlike main.go/wllrsdk.go, which are wasip1-only) so it compiles
// on the host for unit tests — mirroring the tasks extension's claim.go pattern.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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

// Preview caps for the split picker's right pane. The preview travels inside
// the show_picker payload for every listed session at once, so the budget is
// deliberately small: per-message line caps stop one long answer from filling
// the whole preview, and the total line cap bounds the payload size.
const (
	previewWrapWidth   = 72  // columns per wrapped line
	maxPreviewMsgLines = 24  // wrapped lines kept per message
	maxPreviewLines    = 200 // wrapped lines kept for the whole transcript
)

// transcriptPreview renders a session file as a capped plain-text transcript
// for the split picker's preview pane: "you:"/"asst:" role markers, a blank
// line between messages, lines wrapped to previewWrapWidth. Keeps the first
// maxPreviewLines lines; anything further is elided with a "… (+N more lines)"
// marker so long conversations stay bounded. Malformed or missing files yield
// an empty string (the picker then falls back to the session's sublabel).
func transcriptPreview(path string) string {
	msgs, err := loadMessages(path)
	if err != nil || len(msgs) == 0 {
		return ""
	}
	var lines []string
	for _, m := range msgs {
		marker := "you:"
		if m.role == "assistant" {
			marker = "asst:"
		}
		lines = append(lines, marker)
		chunks := previewChunks(m.content)
		shown := chunks
		truncated := len(chunks) > maxPreviewMsgLines
		if truncated {
			shown = chunks[:maxPreviewMsgLines]
		}
		for _, c := range shown {
			lines = append(lines, "  "+c)
		}
		if truncated {
			lines = append(lines, "  …")
		}
		lines = append(lines, "")
	}
	if len(lines) > maxPreviewLines {
		lines = append(lines[:maxPreviewLines], fmt.Sprintf("… (+%d more lines)", len(lines)-maxPreviewLines))
	}
	return strings.TrimRight(strings.Join(lines, "\n"), "\n")
}

// previewChunks wraps a message's content into previewWrapWidth-wide chunks,
// one per output line.
func previewChunks(content string) []string {
	lines := strings.Split(content, "\n")
	out := make([]string, 0, len(lines))
	for _, raw := range lines {
		out = append(out, wrapPreviewLine(raw)...)
	}
	return out
}

// wrapPreviewLine hard-wraps one raw content line to previewWrapWidth,
// splitting long unbreakable tokens so nothing is lost from the preview.
func wrapPreviewLine(s string) []string {
	s = strings.TrimRight(s, "\r")
	if s == "" {
		return []string{""}
	}
	r := []rune(s)
	if len(r) <= previewWrapWidth {
		return []string{s}
	}
	var out []string
	for len(r) > previewWrapWidth {
		out = append(out, string(r[:previewWrapWidth]))
		r = r[previewWrapWidth:]
	}
	if len(r) > 0 {
		out = append(out, string(r))
	}
	return out
}

func sanitizePath(p string) string {
	if p == "" {
		return "--"
	}
	return strings.NewReplacer("/", "--", " ", "_").Replace(strings.TrimPrefix(p, "/"))
}

// historyListDir returns the session directory the /history picker should list:
// the current project's per-cwd directory by default, all directories (empty
// string) when the user explicitly asked for "all". currentFile is the session
// file being written; when it is unknown (session start failed) the caller's
// host cwd is sanitized into the equivalent directory. Empty result means the
// listing falls back to unscoped (all folders under base).
func historyListDir(currentFile, hostCwd, base, firstArg string) string {
	if firstArg == "all" {
		return ""
	}
	if currentFile != "" {
		return filepath.Dir(currentFile)
	}
	if hostCwd != "" {
		return filepath.Join(base, sanitizePath(hostCwd))
	}
	return ""
}
