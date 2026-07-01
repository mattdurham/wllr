//go:build wasip1

// Package main is the history extension for wllr.
// It records each conversation turn to append-only JSONL files under
// ~/.wllr/sessions/<sanitized-cwd>/ and provides an interactive /history
// picker for browsing sessions and rolling back to any message.
package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func init() {
	RegisterCommand("history", "Browse previous conversations and resume from any point")

	OnSessionStart(handleSessionStart)

	OnBeforeAgentStart(func(prompt string) {
		recordMessage("user", prompt)
	})

	OnMessageEnd(func(role, content string) {
		if role == "assistant" {
			recordMessage("assistant", content)
		}
	})

	OnBeforeToolCall(func(payload json.RawMessage) {
		var p struct {
			ToolCallID string          `json:"tool_call_id"`
			ToolName   string          `json:"tool_name"`
			Input      json.RawMessage `json:"input"`
		}
		if json.Unmarshal(payload, &p) != nil || currentFile == "" {
			return
		}
		entryCount++
		appendJSONL(currentFile, toolCallEntry{
			Type:       "tool_call",
			ID:         fmt.Sprintf("t%d", entryCount),
			Timestamp:  time.Now().Format(time.RFC3339Nano),
			ToolCallID: p.ToolCallID,
			ToolName:   p.ToolName,
			Input:      p.Input,
		})
	})

	OnCommand("history", func(_ []string) {
		handleHistoryCommand()
	})
	// Step 2: a session was chosen → show the message picker so the user can
	// choose the point to resume from.
	OnCommand("history:session_selected", func(args []string) {
		if len(args) > 0 {
			handleSessionSelected(args[0])
		}
	})
	// Step 3: a message was chosen → replay context up to that point.
	OnCommand("history:message_selected", func(args []string) {
		if len(args) > 0 {
			handleMessageSelected(args[0])
		}
	})
}

// ─── Session state ────────────────────────────────────────────────────────────

var (
	currentFile        string
	entryCount         int
	pendingSessionPath string // set when session is chosen, used by message picker
)

// ─── JSONL entry types ────────────────────────────────────────────────────────

// ─── Event handlers ───────────────────────────────────────────────────────────

func handleSessionStart() {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "/tmp"
	}
	cwd, _ := os.Getwd()

	sessDir := filepath.Join(home, ".wllr", "sessions", sanitizePath(cwd))
	if err := os.MkdirAll(sessDir, 0o755); err != nil {
		Logf(2, "history: mkdir %s: %v", sessDir, err)
		return
	}

	// Use crypto/rand for the ID so filenames are unique even if time.Now()
	// returns a fixed value in the WASM runtime.
	id := randomID()
	ts := time.Now()
	fname := ts.Format("2006-01-02T15-04-05") + "_" + id + ".jsonl"
	currentFile = filepath.Join(sessDir, fname)
	entryCount = 0

	appendJSONL(currentFile, sessionHeader{
		Type:      "session",
		ID:        id,
		Timestamp: ts.Format(time.RFC3339Nano),
		CWD:       cwd,
	})
	Logf(1, "history: session started → %s", currentFile)
}

func randomID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		// Fallback: use time nanoseconds if rand fails.
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

func recordMessage(role, content string) {
	if currentFile == "" {
		return
	}
	entryCount++
	appendJSONL(currentFile, messageEntry{
		Type:      "message",
		ID:        fmt.Sprintf("%s%d", string(role[0]), entryCount),
		Timestamp: time.Now().Format(time.RFC3339Nano),
		Role:      role,
		Content:   content,
	})
}

// ─── /history → session picker ───────────────────────────────────────────────

func handleHistoryCommand() {
	sessions, err := listSessions()
	if err != nil || len(sessions) == 0 {
		Modal("No previous sessions found.\n\nStart a conversation to create your first session.")
		return
	}

	limit := 20
	if len(sessions) < limit {
		limit = len(sessions)
	}
	items := make([]PickerItem, 0, limit)
	for _, s := range sessions[:limit] {
		items = append(items, PickerItem{
			ID:       s.path,
			Label:    s.timestamp,
			Sublabel: s.preview,
		})
	}
	ShowPicker("Select a session  (↑↓ · enter · esc)", items, "history:session_selected")
}

// ─── Session selected → show message picker (choose resume point) ────────────

// handleSessionSelected shows the conversation's messages in a picker so the
// user can choose the point to resume from. Selecting a message replays context
// up to and including it (see handleMessageSelected). Selecting the last message
// resumes the full conversation.
func handleSessionSelected(path string) {
	pendingSessionPath = path
	msgs, err := loadMessages(path)
	if err != nil || len(msgs) == 0 {
		Modal("Could not load session messages.")
		pendingSessionPath = ""
		return
	}

	items := make([]PickerItem, 0, len(msgs))
	for i, m := range msgs {
		label := "you"
		if m.role == "assistant" {
			label = "asst"
		}
		preview := strings.ReplaceAll(m.content, "\n", " ")
		if r := []rune(preview); len(r) > 70 {
			preview = string(r[:70]) + "…"
		}
		items = append(items, PickerItem{
			ID:       fmt.Sprintf("%d", i),
			Label:    fmt.Sprintf("%2d [%s]", i+1, label),
			Sublabel: preview,
		})
	}
	ShowPicker("Resume from which point?  (loads context up to the selected message)", items, "history:message_selected")
}

// ─── Message selected → replay context up to that point ────────────────────

// handleMessageSelected loads the pending session's messages up to and
// including the selected index, then replays that slice into the agent's
// context via AgentResetHistory. The next turn continues from exactly that
// point. Selecting the last message resumes the whole conversation.
func handleMessageSelected(idxStr string) {
	if pendingSessionPath == "" {
		return
	}
	path := pendingSessionPath
	pendingSessionPath = ""

	var idx int
	fmt.Sscanf(idxStr, "%d", &idx)

	msgs, err := loadMessages(path)
	if err != nil || len(msgs) == 0 {
		Modal("Could not load session messages.")
		return
	}
	if idx < 0 || idx >= len(msgs) {
		return
	}

	selected := msgs[:idx+1]
	wire := make([]Message, len(selected))
	for i, m := range selected {
		wire[i] = Message{Role: m.role, Content: m.content}
	}
	AgentResetHistory(wire)
	Notify(fmt.Sprintf("Resumed — replayed %d of %d messages into context.", len(selected), len(msgs)))
}

// ─── Session file I/O ─────────────────────────────────────────────────────────

func listSessions() ([]sessionInfo, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	base := filepath.Join(home, ".wllr", "sessions")

	entries, err := os.ReadDir(base)
	if err != nil {
		return nil, err
	}

	var all []sessionInfo
	for _, dir := range entries {
		if !dir.IsDir() {
			continue
		}
		files, err := os.ReadDir(filepath.Join(base, dir.Name()))
		if err != nil {
			continue
		}
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".jsonl") {
				continue
			}
			path := filepath.Join(base, dir.Name(), f.Name())
			if path == currentFile {
				continue
			}
			si, err := peekSession(path)
			if err == nil {
				all = append(all, si)
			}
		}
	}

	sort.Slice(all, func(i, j int) bool { return all[i].timestamp > all[j].timestamp })
	return all, nil
}

func peekSession(path string) (sessionInfo, error) {
	// Use file modification time for the display timestamp — the header
	// timestamp comes from time.Now() inside WASM which may be incorrect.
	info, err := os.Stat(path)
	if err != nil {
		return sessionInfo{}, err
	}
	ts := info.ModTime().Format("2006-01-02 15:04")

	data, err := os.ReadFile(path)
	if err != nil {
		return sessionInfo{}, err
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) == 0 {
		return sessionInfo{}, fmt.Errorf("empty")
	}

	preview := "(empty)"
	for _, line := range lines[1:] {
		var m messageEntry
		if json.Unmarshal([]byte(line), &m) == nil && m.Type == "message" && m.Role == "user" && m.Content != "" {
			r := []rune(m.Content)
			if len(r) > 70 {
				preview = string(r[:70]) + "…"
			} else {
				preview = m.Content
			}
			break
		}
	}
	return sessionInfo{path: path, timestamp: ts, preview: preview}, nil
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

func appendJSONL(path string, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		Logf(2, "history: open %s: %v", path, err)
		return
	}
	defer f.Close()
	f.Write(data)
	f.Write([]byte("\n"))
}

func main() {}
