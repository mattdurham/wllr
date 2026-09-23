package main

import (
	"os"
	"path/filepath"
	"strings"
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

func TestTranscriptPreview_BasicLayout(t *testing.T) {
	path := writeJSONL(
		t,
		hdr,
		`{"type":"message","role":"user","content":"hello world"}`,
		`{"type":"tool_call","tool_name":"read_file"}`,
		`{"type":"message","role":"assistant","content":"line one\nline two"}`,
	)
	got := transcriptPreview(path)
	want := "you:\n  hello world\n\nasst:\n  line one\n  line two"
	if got != want {
		t.Errorf("transcriptPreview = %q, want %q", got, want)
	}
}

func TestTranscriptPreview_MissingFileEmpty(t *testing.T) {
	if got := transcriptPreview(filepath.Join(t.TempDir(), "nope.jsonl")); got != "" {
		t.Errorf("transcriptPreview(missing) = %q, want empty", got)
	}
}

func TestTranscriptPreview_LongLineWrapped(t *testing.T) {
	long := strings.Repeat("x", previewWrapWidth+10)
	path := writeJSONL(t, hdr, `{"type":"message","role":"user","content":"`+long+`"}`)
	got := transcriptPreview(path)
	lines := strings.Split(got, "\n")
	// marker + two wrapped continuation lines
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3 (marker + 2 wrapped): %q", len(lines), got)
	}
	if got := len([]rune(strings.TrimSpace(lines[1]))); got != previewWrapWidth {
		t.Errorf("wrapped line width = %d, want %d", got, previewWrapWidth)
	}
}

func TestTranscriptPreview_TotalCap(t *testing.T) {
	// Many alternating messages: the preview must stop at maxPreviewLines with
	// an elision marker instead of growing unbounded. Roles alternate because
	// loadMessages collapses consecutive same-role entries.
	var b strings.Builder
	b.WriteString(hdr + "\n")
	for i := 0; i < 40; i++ {
		b.WriteString(
			`{"type":"message","role":"user","content":"` + strings.Repeat("w", previewWrapWidth) + `"}` + "\n",
		)
		b.WriteString(
			`{"type":"message","role":"assistant","content":"` + strings.Repeat("a", previewWrapWidth) + `"}` + "\n",
		)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "big.jsonl")
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	got := transcriptPreview(path)
	lines := strings.Split(got, "\n")
	if len(lines) > maxPreviewLines+1 {
		t.Errorf("preview has %d lines, want at most %d + 1 marker", len(lines), maxPreviewLines)
	}
	if !strings.HasSuffix(got, "lines)") {
		t.Errorf("preview should end with an elision marker, got tail %q", got[max(0, len(got)-60):])
	}
}

func TestTranscriptPreview_PerMessageCap(t *testing.T) {
	// One message longer than maxPreviewMsgLines must not dominate: it is
	// truncated with a per-message marker and the next message still appears.
	// The repeats use a raw string so \n stays JSON-escaped on the wire.
	long := strings.Repeat(`a\n`, maxPreviewMsgLines+10)
	path := writeJSONL(
		t,
		hdr,
		`{"type":"message","role":"user","content":"`+long+`"}`,
		`{"type":"message","role":"assistant","content":"after"}`,
	)
	got := transcriptPreview(path)
	if !strings.Contains(got, "asst:") || !strings.Contains(got, "after") {
		t.Errorf("preview should include the later message beyond the cap, got %q", got)
	}
	if !strings.Contains(got, "…") {
		t.Errorf("preview should mark the truncated message, got %q", got)
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
