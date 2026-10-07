package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mattdurham/wllr/modules/extension"
)

// newReadFileHost wires the native tools (including read_file) onto a real host
// so tests exercise the exact handler the agent calls.
func newReadFileHost(t *testing.T) *extension.Host {
	t.Helper()
	h := extension.NewHost(nil)
	t.Cleanup(func() { _ = h.Close(context.Background()) })
	registerNativeTools(h)
	return h
}

// callReadFile invokes the read_file tool through the host and returns the
// textual result and its error flag.
func callReadFile(t *testing.T, h *extension.Host, input string) (string, bool) {
	t.Helper()
	res, err := h.ExecuteTool(context.Background(), "main", "tc-read", "read_file", json.RawMessage(input))
	if err != nil {
		t.Fatalf("ExecuteTool(read_file): %v", err)
	}
	return res.Result, res.IsError
}

func TestReadFile_ReturnsWholeFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("line1\nline2\nline3"), 0o600); err != nil {
		t.Fatal(err)
	}

	h := newReadFileHost(t)
	got, isErr := callReadFile(t, h, `{"path":`+jsonString(path)+`}`)
	if isErr {
		t.Fatalf("unexpected error: %q", got)
	}
	if got != "line1\nline2\nline3" {
		t.Fatalf("content = %q", got)
	}
}

func TestReadFile_LineRange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("l1\nl2\nl3\nl4\nl5"), 0o600); err != nil {
		t.Fatal(err)
	}

	h := newReadFileHost(t)
	got, isErr := callReadFile(t, h, `{"path":`+jsonString(path)+`,"start_line":2,"end_line":4}`)
	if isErr {
		t.Fatalf("unexpected error: %q", got)
	}
	if got != "l2\nl3\nl4" {
		t.Fatalf("range content = %q, want l2\\nl3\\nl4", got)
	}
}

func TestReadFile_LineRangeOpenEnd(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("l1\nl2\nl3"), 0o600); err != nil {
		t.Fatal(err)
	}

	h := newReadFileHost(t)
	// end_line omitted (0) means "to end of file".
	got, isErr := callReadFile(t, h, `{"path":`+jsonString(path)+`,"start_line":2}`)
	if isErr {
		t.Fatalf("unexpected error: %q", got)
	}
	if got != "l2\nl3" {
		t.Fatalf("open-ended range = %q, want l2\\nl3", got)
	}
}

func TestReadFile_LineRangeBeyondEOF(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("l1\nl2"), 0o600); err != nil {
		t.Fatal(err)
	}

	h := newReadFileHost(t)
	got, isErr := callReadFile(t, h, `{"path":`+jsonString(path)+`,"start_line":99}`)
	if isErr {
		t.Fatalf("range-past-EOF should not be an error: %q", got)
	}
	if !strings.Contains(got, "beyond end of file") {
		t.Fatalf("expected beyond-EOF notice, got %q", got)
	}
}

func TestReadFile_ByteCapTruncatesWithNotice(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "big.txt")
	// 100 lines of "0123456789" = 1100 bytes.
	body := strings.Repeat("0123456789\n", 100)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	h := newReadFileHost(t)
	got, isErr := callReadFile(t, h, `{"path":`+jsonString(path)+`,"max_bytes":100}`)
	if isErr {
		t.Fatalf("truncation should not be an error: %q", got)
	}
	if !strings.Contains(got, "truncated at") {
		t.Fatalf("expected truncation notice, got %q", got)
	}
	// The notice reports the true total, not the truncated length.
	if !strings.Contains(got, "1100 total") {
		t.Fatalf("expected total byte count in notice, got %q", got)
	}
	// The returned body must not exceed the cap (plus the notice suffix).
	bodyPart := strings.SplitN(got, "\n[read_file: truncated", 2)[0]
	if len(bodyPart) > 100 {
		t.Fatalf("truncated body = %d bytes, want <= 100", len(bodyPart))
	}
}

func TestReadFile_SmallFileNotTruncated(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "small.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}

	h := newReadFileHost(t)
	got, _ := callReadFile(t, h, `{"path":`+jsonString(path)+`}`)
	if strings.Contains(got, "truncated") {
		t.Fatalf("small file must not be truncated: %q", got)
	}
	if got != "hello" {
		t.Fatalf("content = %q", got)
	}
}

func TestReadFile_MissingSuggestsSameDirTypo(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "model.go"), []byte("package x"), 0o600); err != nil {
		t.Fatal(err)
	}

	h := newReadFileHost(t)
	missing := filepath.Join(dir, "modle.go") // transposed letters
	got, isErr := callReadFile(t, h, `{"path":`+jsonString(missing)+`}`)
	if !isErr {
		t.Fatalf("missing file must be an error, got %q", got)
	}
	if !strings.Contains(got, "did you mean") {
		t.Fatalf("expected did-you-mean suggestions, got %q", got)
	}
	if !strings.Contains(got, filepath.Join(dir, "model.go")) {
		t.Fatalf("expected model.go in suggestions, got %q", got)
	}
}

func TestReadFile_MissingSuggestsRelocatedWorktree(t *testing.T) {
	root := t.TempDir()
	// The file exists only under a sibling "-worktrees/<name>/" checkout, not in
	// the main repo path the caller asked for — the exact pattern that produced a
	// two-day path-guessing loop in the failure analysis.
	want := filepath.Join(root, "repo-worktrees", "review", "internal", "actors", "planner", "job.go")
	if err := os.MkdirAll(filepath.Dir(want), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(want, []byte("package planner"), 0o600); err != nil {
		t.Fatal(err)
	}

	h := newReadFileHost(t)
	missing := filepath.Join(root, "repo", "internal", "actors", "planner", "job.go")
	got, isErr := callReadFile(t, h, `{"path":`+jsonString(missing)+`}`)
	if !isErr {
		t.Fatalf("missing file must be an error, got %q", got)
	}
	if !strings.Contains(got, want) {
		t.Fatalf("expected relocated path %q in suggestions, got %q", want, got)
	}
}

func TestReadFile_MissingNoSuggestionIsPlainError(t *testing.T) {
	dir := t.TempDir()
	h := newReadFileHost(t)
	missing := filepath.Join(dir, "nothing-like-this-anywhere.txt")
	got, isErr := callReadFile(t, h, `{"path":`+jsonString(missing)+`}`)
	if !isErr {
		t.Fatalf("missing file must be an error, got %q", got)
	}
	if strings.Contains(got, "did you mean") {
		t.Fatalf("no candidates should yield a plain error, got %q", got)
	}
}

func TestReadFile_PathRequired(t *testing.T) {
	h := newReadFileHost(t)
	got, isErr := callReadFile(t, h, `{}`)
	if !isErr {
		t.Fatalf("missing path must be an error, got %q", got)
	}
	if got != "path is required" {
		t.Fatalf("got %q, want path is required", got)
	}
}

func TestBoundedEditDistance(t *testing.T) {
	cases := []struct {
		a, b string
		max  int
		want int
	}{
		{"model.go", "model.go", 2, 0},
		{"modle.go", "model.go", 2, 2},
		{"a", "b", 2, 1},
		{"completely-different", "xyz", 2, 2}, // capped
		{"", "abc", 5, 3},
	}
	for _, tc := range cases {
		if got := boundedEditDistance(tc.a, tc.b, tc.max); got != tc.want {
			t.Errorf("boundedEditDistance(%q,%q,%d) = %d, want %d", tc.a, tc.b, tc.max, got, tc.want)
		}
	}
}

// jsonString renders s as a JSON string literal for tool inputs.
func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
