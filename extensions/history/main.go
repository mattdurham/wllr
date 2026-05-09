//go:build wasip1

// Package main is the history extension for wllr.
// It records each conversation turn to append-only JSONL files under
// ~/.wllr/sessions/<sanitized-cwd>/ and provides /history and /resume commands.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unsafe"
)

// ─── WASM ABI ────────────────────────────────────────────────────────────────

//go:wasmimport env host_log
func hostLog(level, ptr, length uint32)

//go:wasmimport env host_call
func hostCall(reqPtr, reqLen, respPtrPtr, respLenPtr uint32) uint32

var pinned = map[uintptr][]byte{}

//go:wasmexport _alloc
func extensionAlloc(size int32) int32 {
	if size <= 0 {
		return 0
	}
	buf := make([]byte, size)
	ptr := uintptr(unsafe.Pointer(&buf[0]))
	pinned[ptr] = buf
	return int32(ptr)
}

//go:wasmexport _free
func extensionFree(ptr int32) {
	delete(pinned, uintptr(ptr))
}

//go:wasmexport _init
func extensionInit() int32 {
	hostCallJSON("subscribe", map[string]string{"event": "session_start"})
	hostCallJSON("subscribe", map[string]string{"event": "before_agent_start"})
	hostCallJSON("subscribe", map[string]string{"event": "message_end"})
	hostCallJSON("subscribe", map[string]string{"event": "on_command"})
	hostCallJSON("register_command", map[string]string{
		"name":        "history",
		"description": "Browse previous conversation sessions",
	})
	hostCallJSON("register_command", map[string]string{
		"name":        "resume",
		"description": "Resume a previous session: /resume <index>",
	})
	return 0
}

//go:wasmexport _on_event
func extensionOnEvent(ptr, length int32) int32 {
	data := unsafe.Slice((*byte)(unsafe.Pointer(uintptr(ptr))), length)
	var evt struct {
		Type    string          `json:"type"`
		Payload json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(data, &evt); err != nil {
		return 0
	}
	switch evt.Type {
	case "session_start":
		handleSessionStart()

	case "before_agent_start":
		var p struct {
			Prompt string `json:"prompt"`
		}
		if json.Unmarshal(evt.Payload, &p) == nil && p.Prompt != "" {
			recordMessage("user", p.Prompt)
		}

	case "message_end":
		var p struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}
		if json.Unmarshal(evt.Payload, &p) == nil && p.Role == "assistant" && p.Content != "" {
			recordMessage("assistant", p.Content)
		}

	case "on_command":
		var p struct {
			Name string   `json:"name"`
			Args []string `json:"args"`
		}
		if json.Unmarshal(evt.Payload, &p) == nil {
			switch p.Name {
			case "history":
				handleHistoryCommand()
			case "resume":
				handleResumeCommand(p.Args)
			}
		}
	}
	return 0
}

// ─── Session state ────────────────────────────────────────────────────────────

var (
	currentFile string
	entryCount  int
)

// ─── JSONL entry types ────────────────────────────────────────────────────────

type sessionHeader struct {
	Type      string `json:"type"`
	ID        string `json:"id"`
	Timestamp string `json:"timestamp"`
	CWD       string `json:"cwd"`
}

type messageEntry struct {
	Type      string `json:"type"`
	ID        string `json:"id"`
	Timestamp string `json:"timestamp"`
	Role      string `json:"role"`
	Content   string `json:"content"`
}

// ─── Event handlers ───────────────────────────────────────────────────────────

func handleSessionStart() {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "/tmp"
	}
	cwd, _ := os.Getwd()

	sessDir := filepath.Join(home, ".wllr", "sessions", sanitizePath(cwd))
	if err := os.MkdirAll(sessDir, 0o755); err != nil {
		logf(2, "history: mkdir %s: %v", sessDir, err)
		return
	}

	ts := time.Now()
	id := fmt.Sprintf("%d", ts.UnixNano())
	fname := ts.Format("2006-01-02T15-04-05") + "_" + id + ".jsonl"
	currentFile = filepath.Join(sessDir, fname)
	entryCount = 0

	appendJSONL(currentFile, sessionHeader{
		Type:      "session",
		ID:        id,
		Timestamp: ts.Format(time.RFC3339Nano),
		CWD:       cwd,
	})
	logf(1, "history: session started → %s", currentFile)
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

// ─── /history command ─────────────────────────────────────────────────────────

func handleHistoryCommand() {
	sessions, err := listSessions()
	if err != nil || len(sessions) == 0 {
		showModal("No previous sessions found.\n\nStart a conversation to create your first session.")
		return
	}

	var sb strings.Builder
	sb.WriteString("Recent sessions — use /resume <n> to load one:\n\n")
	limit := 20
	if len(sessions) < limit {
		limit = len(sessions)
	}
	for i, s := range sessions[:limit] {
		sb.WriteString(fmt.Sprintf("[%2d]  %s\n      %s\n\n", i+1, s.timestamp, s.preview))
	}
	showModal(strings.TrimRight(sb.String(), "\n"))
}

// ─── /resume command ──────────────────────────────────────────────────────────

func handleResumeCommand(args []string) {
	sessions, err := listSessions()
	if err != nil || len(sessions) == 0 {
		showModal("No previous sessions found.")
		return
	}

	idx := 1
	if len(args) > 0 {
		fmt.Sscanf(args[0], "%d", &idx)
	}
	if idx < 1 || idx > len(sessions) {
		showModal(fmt.Sprintf("Invalid index %d. Run /history to see available sessions (1–%d).", idx, len(sessions)))
		return
	}

	s := sessions[idx-1]
	msgs, err := loadMessages(s.path)
	if err != nil {
		showModal("Error loading session: " + err.Error())
		return
	}
	if len(msgs) == 0 {
		showModal("That session has no messages.")
		return
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## Resumed session (%s)\n\n", s.timestamp))
	sb.WriteString("The following is the conversation history from the resumed session:\n\n")
	for _, m := range msgs {
		label := "User"
		if m.role == "assistant" {
			label = "Assistant"
		}
		content := m.content
		if r := []rune(content); len(r) > 600 {
			content = string(r[:600]) + "…"
		}
		sb.WriteString(fmt.Sprintf("**%s:** %s\n\n", label, content))
	}
	sb.WriteString("---\n\nContinue this conversation from where it left off.")

	hostCallJSON("append_system_prompt", map[string]string{"text": sb.String()})
	hostCallJSON("notify", map[string]string{
		"text": fmt.Sprintf("Resumed session from %s (%d messages)", s.timestamp, len(msgs)),
	})
}

// ─── Session file I/O ─────────────────────────────────────────────────────────

type sessionInfo struct {
	path      string
	timestamp string
	preview   string
}

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
	data, err := os.ReadFile(path)
	if err != nil {
		return sessionInfo{}, err
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) == 0 {
		return sessionInfo{}, fmt.Errorf("empty")
	}

	ts := "unknown"
	var hdr sessionHeader
	if json.Unmarshal([]byte(lines[0]), &hdr) == nil && hdr.Timestamp != "" {
		if t, err := time.Parse(time.RFC3339Nano, hdr.Timestamp); err == nil {
			ts = t.Format("2006-01-02 15:04")
		}
	}

	preview := "(empty)"
	for _, line := range lines[1:] {
		var m messageEntry
		if json.Unmarshal([]byte(line), &m) == nil && m.Role == "user" && m.Content != "" {
			r := []rune(m.Content)
			if len(r) > 72 {
				preview = string(r[:72]) + "…"
			} else {
				preview = m.Content
			}
			break
		}
	}
	return sessionInfo{path: path, timestamp: ts, preview: preview}, nil
}

type msg struct{ role, content string }

func loadMessages(path string) ([]msg, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	var out []msg
	for _, line := range lines[1:] {
		var e messageEntry
		if json.Unmarshal([]byte(line), &e) == nil && e.Type == "message" {
			out = append(out, msg{role: e.Role, content: e.Content})
		}
	}
	return out, nil
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

func appendJSONL(path string, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		logf(2, "history: open %s: %v", path, err)
		return
	}
	defer f.Close()
	f.Write(data)
	f.Write([]byte("\n"))
}

func sanitizePath(p string) string {
	if p == "" {
		return "--"
	}
	return strings.NewReplacer("/", "--", " ", "_").Replace(strings.TrimPrefix(p, "/"))
}

func showModal(text string) {
	hostCallJSON("modal", map[string]string{"text": text})
}

func logf(level int, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	b := []byte(msg)
	if len(b) == 0 {
		return
	}
	hostLog(uint32(level), uint32(uintptr(unsafe.Pointer(&b[0]))), uint32(len(b)))
}

func hostCallJSON(method string, params any) {
	type request struct {
		Method string `json:"method"`
		Params any    `json:"params,omitempty"`
	}
	reqBytes, err := json.Marshal(request{Method: method, Params: params})
	if err != nil {
		return
	}
	buf := make([]byte, len(reqBytes))
	copy(buf, reqBytes)
	ptr := uintptr(unsafe.Pointer(&buf[0]))
	var respPtr, respLen uint32
	hostCall(
		uint32(ptr), uint32(len(buf)),
		uint32(uintptr(unsafe.Pointer(&respPtr))),
		uint32(uintptr(unsafe.Pointer(&respLen))),
	)
	if respPtr != 0 {
		delete(pinned, uintptr(respPtr))
	}
}

func main() {}
