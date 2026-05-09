//go:build wasip1

// Package main is the history extension for wllr.
// It records each conversation turn to append-only JSONL files under
// ~/.wllr/sessions/<sanitized-cwd>/ and provides an interactive /history
// picker for browsing sessions and rolling back to any message.
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
		"description": "Browse and restore previous conversation sessions",
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
			case "history:session_selected":
				if len(p.Args) > 0 {
					handleSessionSelected(p.Args[0])
				}
			case "history:message_selected":
				if len(p.Args) > 0 {
					handleMessageSelected(p.Args[0])
				}
			}
		}
	}
	return 0
}

// ─── Session state ────────────────────────────────────────────────────────────

var (
	currentFile        string
	entryCount         int
	pendingSessionPath string // set when session is chosen, used by message picker
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

// ─── /history → session picker ───────────────────────────────────────────────

func handleHistoryCommand() {
	sessions, err := listSessions()
	if err != nil || len(sessions) == 0 {
		hostCallJSON("modal", map[string]string{
			"text": "No previous sessions found.\n\nStart a conversation to create your first session.",
		})
		return
	}

	limit := 20
	if len(sessions) < limit {
		limit = len(sessions)
	}
	items := make([]pickerItem, 0, limit)
	for _, s := range sessions[:limit] {
		items = append(items, pickerItem{
			ID:       s.path,
			Label:    s.timestamp,
			Sublabel: s.preview,
		})
	}
	showPicker("Select a session  (↑↓ · enter · esc)", items, "history:session_selected")
}

// ─── Session selected → message picker ───────────────────────────────────────

func handleSessionSelected(path string) {
	pendingSessionPath = path
	msgs, err := loadMessages(path)
	if err != nil || len(msgs) == 0 {
		hostCallJSON("modal", map[string]string{"text": "Could not load session messages."})
		return
	}

	items := make([]pickerItem, 0, len(msgs))
	for i, m := range msgs {
		label := "user"
		if m.role == "assistant" {
			label = "asst"
		}
		preview := m.content
		if r := []rune(preview); len(r) > 70 {
			preview = string(r[:70]) + "…"
		}
		items = append(items, pickerItem{
			ID:       fmt.Sprintf("%d", i),
			Label:    fmt.Sprintf("[%s]", label),
			Sublabel: preview,
		})
	}
	showPicker("Roll back to this point (loads all messages up to here)", items, "history:message_selected")
}

// ─── Message selected → reset agent history ──────────────────────────────────

func handleMessageSelected(idxStr string) {
	if pendingSessionPath == "" {
		return
	}
	var idx int
	fmt.Sscanf(idxStr, "%d", &idx)

	msgs, err := loadMessages(pendingSessionPath)
	if err != nil || len(msgs) == 0 {
		hostCallJSON("modal", map[string]string{"text": "Could not load session messages."})
		return
	}
	if idx < 0 || idx >= len(msgs) {
		return
	}

	selected := msgs[:idx+1]
	type wireMsg struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	wire := make([]wireMsg, len(selected))
	for i, m := range selected {
		wire[i] = wireMsg{Role: m.role, Content: m.content}
	}
	hostCallJSON("agent_reset_history", map[string]any{"messages": wire})
	pendingSessionPath = ""
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
		if t, err2 := time.Parse(time.RFC3339Nano, hdr.Timestamp); err2 == nil {
			ts = t.Format("2006-01-02 15:04")
		}
	}

	preview := "(empty)"
	for _, line := range lines[1:] {
		var m messageEntry
		if json.Unmarshal([]byte(line), &m) == nil && m.Role == "user" && m.Content != "" {
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

type storedMsg struct{ role, content string }

func loadMessages(path string) ([]storedMsg, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	var out []storedMsg
	for _, line := range lines[1:] {
		var e messageEntry
		if json.Unmarshal([]byte(line), &e) == nil && e.Type == "message" {
			out = append(out, storedMsg{role: e.Role, content: e.Content})
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

func logf(level int, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	b := []byte(msg)
	if len(b) == 0 {
		return
	}
	hostLog(uint32(level), uint32(uintptr(unsafe.Pointer(&b[0]))), uint32(len(b)))
}

// ─── Picker wire type ─────────────────────────────────────────────────────────

type pickerItem struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Sublabel string `json:"sublabel,omitempty"`
}

func showPicker(title string, items []pickerItem, callback string) {
	hostCallJSON("show_picker", map[string]any{
		"title":    title,
		"items":    items,
		"callback": callback,
	})
}

// ─── host_call helper ─────────────────────────────────────────────────────────

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
