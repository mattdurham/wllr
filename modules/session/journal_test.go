package session

import (
	"encoding/json"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/mattdurham/wllr/modules/sdk"
)

func TestJournalWritesEntries(t *testing.T) {
	dir := t.TempDir()
	j, err := OpenJournal(dir + "/test.jsonl")
	if err != nil {
		t.Fatalf("OpenJournal: %v", err)
	}

	if err := j.WriteEntry(map[string]string{"type": "session_start", "id": "x"}); err != nil {
		t.Fatalf("WriteEntry session_start: %v", err)
	}
	if err := j.WriteEntry(map[string]string{"type": "message", "role": "user", "content": "hi"}); err != nil {
		t.Fatalf("WriteEntry message: %v", err)
	}
	if err := j.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	data, err := os.ReadFile(dir + "/test.jsonl")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %q", len(lines), string(data))
	}

	var entry map[string]string
	if err := json.Unmarshal([]byte(lines[0]), &entry); err != nil {
		t.Fatalf("Unmarshal line 0: %v", err)
	}
	if entry["type"] != "session_start" {
		t.Errorf("entry[type] = %q, want %q", entry["type"], "session_start")
	}
}

func TestJournalConcurrentWrites(t *testing.T) {
	dir := t.TempDir()
	j, err := OpenJournal(dir + "/concurrent.jsonl")
	if err != nil {
		t.Fatalf("OpenJournal: %v", err)
	}
	defer func() { _ = j.Close() }()

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_ = j.WriteEntry(map[string]any{"type": "msg", "n": n})
		}(i)
	}
	wg.Wait()

	data, err := os.ReadFile(dir + "/concurrent.jsonl")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 10 {
		t.Errorf("expected 10 lines, got %d", len(lines))
	}
	for i, line := range lines {
		if !json.Valid([]byte(line)) {
			t.Errorf("line %d is not valid JSON: %q", i, line)
		}
	}
}

func TestJournalIDFormat(t *testing.T) {
	id := newSessionID()
	// format: YYYYMMDD-HHMMSS-XXXX
	parts := strings.Split(id, "-")
	if len(parts) != 3 {
		t.Fatalf("newSessionID() = %q, expected 3 dash-separated parts", id)
	}
	if len(parts[0]) != 8 {
		t.Errorf("date part %q should be 8 chars (YYYYMMDD)", parts[0])
	}
	for _, c := range parts[0] {
		if c < '0' || c > '9' {
			t.Errorf("date part %q contains non-digit %q", parts[0], c)
		}
	}
	if len(parts[1]) != 6 {
		t.Errorf("time part %q should be 6 chars (HHMMSS)", parts[1])
	}
	for _, c := range parts[1] {
		if c < '0' || c > '9' {
			t.Errorf("time part %q contains non-digit %q", parts[1], c)
		}
	}
	if len(parts[2]) != 4 {
		t.Errorf("random part %q should be 4 chars", parts[2])
	}
	for _, c := range parts[2] {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			t.Errorf("random part %q contains non-hex char %q", parts[2], c)
		}
	}
}

func TestLoadRecentSession(t *testing.T) {
	dir := t.TempDir()
	content := `{"type":"session_start","id":"x","ts":"2024-01-01T00:00:00Z"}` + "\n" +
		`{"type":"message","role":"user","content":"hello"}` + "\n" +
		`{"type":"message","role":"assistant","content":"world"}` + "\n"
	path := dir + "/20240101-000000-abcd.jsonl"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	msgs, err := LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[0].Role != sdk.RoleUser {
		t.Errorf("msgs[0].Role = %q, want %q", msgs[0].Role, sdk.RoleUser)
	}
	if msgs[0].Content != "hello" {
		t.Errorf("msgs[0].Content = %q, want %q", msgs[0].Content, "hello")
	}
	if msgs[1].Role != sdk.RoleAssistant {
		t.Errorf("msgs[1].Role = %q, want %q", msgs[1].Role, sdk.RoleAssistant)
	}
	if msgs[1].Content != "world" {
		t.Errorf("msgs[1].Content = %q, want %q", msgs[1].Content, "world")
	}
}
