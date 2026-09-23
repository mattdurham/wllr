package main

import (
	"os"
	"path/filepath"
	"testing"
)

// writeJSONL writes lines to a temp .jsonl file and returns its path.
func writeJSONL(t *testing.T, lines ...string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	content := ""
	for _, l := range lines {
		content += l + "\n"
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}

const hdr = `{"type":"session","id":"abc","timestamp":"2026-06-30T00:00:00Z","cwd":"/x"}`

func TestLoadMessages_BasicOrder(t *testing.T) {
	path := writeJSONL(
		t,
		hdr,
		`{"type":"message","role":"user","content":"hello"}`,
		`{"type":"message","role":"assistant","content":"hi there"}`,
		`{"type":"message","role":"user","content":"more"}`,
	)
	msgs, err := loadMessages(path)
	if err != nil {
		t.Fatalf("loadMessages: %v", err)
	}
	if len(msgs) != 3 {
		t.Fatalf("got %d messages, want 3", len(msgs))
	}
	if msgs[0].role != "user" || msgs[0].content != "hello" {
		t.Errorf("msg0 = %+v", msgs[0])
	}
	if msgs[1].role != "assistant" {
		t.Errorf("msg1 role = %q, want assistant", msgs[1].role)
	}
}

func TestLoadMessages_SkipsToolCallsAndEmpty(t *testing.T) {
	path := writeJSONL(
		t,
		hdr,
		`{"type":"message","role":"user","content":"q"}`,
		`{"type":"tool_call","tool_name":"read_file"}`,
		`{"type":"message","role":"assistant","content":"   "}`,
		`{"type":"message","role":"assistant","content":"answer"}`,
	)
	msgs, err := loadMessages(path)
	if err != nil {
		t.Fatalf("loadMessages: %v", err)
	}
	// tool_call skipped; empty assistant skipped; so user + answer remain.
	if len(msgs) != 2 {
		t.Fatalf("got %d, want 2: %+v", len(msgs), msgs)
	}
	if msgs[1].content != "answer" {
		t.Errorf("msg1 content = %q", msgs[1].content)
	}
}

func TestLoadMessages_CollapsesConsecutiveSameRole(t *testing.T) {
	path := writeJSONL(
		t,
		hdr,
		`{"type":"message","role":"user","content":"first"}`,
		`{"type":"message","role":"user","content":"second"}`,
		`{"type":"message","role":"assistant","content":"reply"}`,
	)
	msgs, err := loadMessages(path)
	if err != nil {
		t.Fatalf("loadMessages: %v", err)
	}
	// Consecutive user messages collapse to the first; API needs alternation.
	if len(msgs) != 2 || msgs[0].content != "first" || msgs[1].role != "assistant" {
		t.Fatalf("got %+v, want [first(user), reply(asst)]", msgs)
	}
}

func TestLoadMessages_DropsLeadingAssistant(t *testing.T) {
	path := writeJSONL(
		t,
		hdr,
		`{"type":"message","role":"assistant","content":"leading"}`,
		`{"type":"message","role":"user","content":"q"}`,
	)
	msgs, err := loadMessages(path)
	if err != nil {
		t.Fatalf("loadMessages: %v", err)
	}
	// History must start with a user message.
	if len(msgs) != 1 || msgs[0].role != "user" {
		t.Fatalf("got %+v, want a single leading user message", msgs)
	}
}

func TestLoadMessages_TrimmedContent(t *testing.T) {
	path := writeJSONL(
		t,
		hdr,
		`{"type":"message","role":"user","content":"  hello  "}`,
		`{"type":"message","role":"assistant","content": "  hi there\n  "}`,
	)
	msgs, err := loadMessages(path)
	if err != nil {
		t.Fatalf("loadMessages: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("got %d, want 2: %+v", len(msgs), msgs)
	}
	if msgs[0].content != "hello" {
		t.Errorf("msg0 content = %q, want %q", msgs[0].content, "hello")
	}
	if msgs[1].content != "hi there" {
		t.Errorf("msg1 content = %q, want %q", msgs[1].content, "hi there")
	}
}

func TestLoadMessages_WhitespaceOnlyCannotBreakAlternation(t *testing.T) {
	path := writeJSONL(
		t,
		hdr,
		`{"type":"message","role":"user","content":"q"}`,
		`{"type":"message","role":"assistant","content":"\n\n\n"}`,
		`{"type":"message","role":"assistant","content":"answer"}`,
	)
	msgs, err := loadMessages(path)
	if err != nil {
		t.Fatalf("loadMessages: %v", err)
	}
	// The blank assistant must be dropped *and* must not block the real
	// assistant reply: [user q, assistant answer] expected.
	if len(msgs) != 2 {
		t.Fatalf("got %+v, want [q(user), answer(asst)]", msgs)
	}
	if msgs[1].role != "assistant" || msgs[1].content != "answer" {
		t.Errorf("msg1 = %+v, want assistant/answer", msgs[1])
	}
}

func TestCollapseWhitespace(t *testing.T) {
	cases := map[string]string{
		"  hello  ":                "hello",
		"\n\n\n":                   "",
		"a\nb\nc":                  "a b c",
		"a\t\t\tb":                 "a b",
		"\nLet me re-orient\n\n\n": "Let me re-orient",
		"one two   three":          "one two three",
		"":                         "",
	}
	for in, want := range cases {
		if got := collapseWhitespace(in); got != want {
			t.Errorf("collapseWhitespace(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLoadMessages_MissingFile(t *testing.T) {
	if _, err := loadMessages(filepath.Join(t.TempDir(), "nope.jsonl")); err == nil {
		t.Error("expected error for missing file")
	}
}

func TestSanitizePath(t *testing.T) {
	cases := map[string]string{
		"":           "--",
		"/home/x y":  "home--x_y",
		"/a/b/c":     "a--b--c",
		"relative/p": "relative--p",
	}
	for in, want := range cases {
		if got := sanitizePath(in); got != want {
			t.Errorf("sanitizePath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFormatTimestamp(t *testing.T) {
	// RFC3339Nano host timestamps render as human "2006-01-02 15:04" picker
	// labels; unparseable input falls back to the raw string.
	cases := []struct {
		raw  string
		want string
	}{
		{"2026-09-22T10-00-00_abcd", "2026-09-22T10-00-00_abcd"}, // not RFC3339 → raw
		{"2026-09-22T10:30:00Z", "2026-09-22 10:30"},
		{"2026-09-22T10:30:00.123456789+02:00", "2026-09-22 10:30"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := formatTimestamp(tc.raw); got != tc.want {
			t.Errorf("formatTimestamp(%q) = %q, want %q", tc.raw, got, tc.want)
		}
	}
}

func TestHistoryListDir(t *testing.T) {
	base := filepath.Join("home", ".wllr", "sessions")
	currentFile := filepath.Join(base, "Users--matt--proj", "2026-09-22T10-00-00_abcd.jsonl")
	wantDir := filepath.Join(base, "Users--matt--proj")

	cases := []struct {
		name        string
		currentFile string
		hostCwd     string
		firstArg    string
		want        string
	}{
		// Default: scope to the session file's directory.
		{"scoped to current session dir", currentFile, "/other", "", wantDir},
		// /history all: unscoped listing (empty dir).
		{"all widens listing", currentFile, "/other", "all", ""},
		// No session file yet: derive the dir from the host cwd.
		{"falls back to host cwd", "", "/Users/matt/source/proj", "", filepath.Join(base, "Users--matt--source--proj")},
		// Nothing known at all: unscoped rather than empty listing.
		{"no ground truth falls back to all", "", "", "", ""},
	}
	for _, tc := range cases {
		if got := historyListDir(tc.currentFile, tc.hostCwd, base, tc.firstArg); got != tc.want {
			t.Errorf("%s: historyListDir(%q, %q, %q, %q) = %q, want %q",
				tc.name, tc.currentFile, tc.hostCwd, base, tc.firstArg, got, tc.want)
		}
	}
}
