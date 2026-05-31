package session

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/mattdurham/wllr/modules/sdk"
)

// Journal is a thread-safe append-only JSONL session log.
// Each call to WriteEntry appends one JSON object followed by a newline.
type Journal struct {
	f  *os.File
	w  *bufio.Writer
	mu sync.Mutex
}

// OpenJournal opens or creates the JSONL file at path for append-only writing.
// The file is created if it does not exist. Directories must already exist.
func OpenJournal(path string) (*Journal, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open journal %q: %w", path, err)
	}
	return &Journal{f: f, w: bufio.NewWriter(f)}, nil
}

// WriteEntry marshals v as a single JSON object and appends it as a line.
// WriteEntry is goroutine-safe; concurrent callers are serialized by an internal mutex.
func (j *Journal) WriteEntry(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("journal marshal: %w", err)
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if _, err := j.w.Write(b); err != nil {
		return fmt.Errorf("journal write: %w", err)
	}
	if err := j.w.WriteByte('\n'); err != nil {
		return fmt.Errorf("journal write newline: %w", err)
	}
	if err := j.w.Flush(); err != nil {
		return fmt.Errorf("journal flush: %w", err)
	}
	return nil
}

// Close flushes any buffered data and closes the underlying file.
func (j *Journal) Close() error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if err := j.w.Flush(); err != nil {
		_ = j.f.Close()
		return fmt.Errorf("journal flush on close: %w", err)
	}
	if err := j.f.Close(); err != nil {
		return fmt.Errorf("journal close file: %w", err)
	}
	return nil
}

// NewSessionID returns a session ID in the format YYYYMMDD-HHMMSS-XXXX where
// XXXX is 4 random lowercase hex characters derived from crypto/rand.
func NewSessionID() string {
	return newSessionID()
}

// newSessionID is the internal implementation used by tests.
func newSessionID() string {
	ts := time.Now().Format("20060102-150405")
	var b [2]byte
	_, _ = rand.Read(b[:])
	return ts + "-" + hex.EncodeToString(b[:])
}

// journalEntry is used for parsing JSONL lines in LoadSession.
type journalEntry struct {
	Type    string `json:"type"`
	Role    string `json:"role"`
	Content string `json:"content"`
}

// LoadSession reads a JSONL session file at path and returns the user and
// assistant messages in order. Lines of type "session_start" and "session_end"
// are ignored. Lines that cannot be parsed are silently skipped.
func LoadSession(path string) ([]sdk.Message, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("load session %q: %w", path, err)
	}
	var msgs []sdk.Message
	lines := splitLines(data)
	for _, line := range lines {
		if len(line) == 0 {
			continue
		}
		var entry journalEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			continue
		}
		if entry.Type != "message" {
			continue
		}
		var role sdk.Role
		switch entry.Role {
		case string(sdk.RoleUser):
			role = sdk.RoleUser
		case string(sdk.RoleAssistant):
			role = sdk.RoleAssistant
		default:
			continue
		}
		msgs = append(msgs, sdk.Message{Role: role, Content: entry.Content})
	}
	return msgs, nil
}

// splitLines splits b into non-empty lines without allocating a string.
func splitLines(b []byte) [][]byte {
	var lines [][]byte
	start := 0
	for i, c := range b {
		if c == '\n' {
			if i > start {
				lines = append(lines, b[start:i])
			}
			start = i + 1
		}
	}
	if start < len(b) {
		lines = append(lines, b[start:])
	}
	return lines
}
