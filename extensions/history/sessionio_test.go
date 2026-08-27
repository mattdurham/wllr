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
