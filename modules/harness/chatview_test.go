package harness

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestChatView_SetExternalContent_FollowsWhenAtBottom(t *testing.T) {
	c := NewChatView(80, 5)
	c.SetExternalContent(numberedLines(20))
	if !c.vp.AtBottom() {
		t.Fatal("initial content should scroll to bottom")
	}
	initialOffset := c.vp.YOffset()

	c.SetExternalContent(numberedLines(30))

	if !c.vp.AtBottom() {
		t.Fatalf(
			"content update should follow bottom when already at bottom; offset=%d lines=%d",
			c.vp.YOffset(),
			c.vp.TotalLineCount(),
		)
	}
	if c.vp.YOffset() <= initialOffset {
		t.Fatalf("expected offset to advance after more content, before=%d after=%d", initialOffset, c.vp.YOffset())
	}
}

func TestChatView_SetExternalContent_PreservesScrollback(t *testing.T) {
	c := NewChatView(80, 5)
	c.SetExternalContent(numberedLines(30))
	c.ScrollUp(6)
	scrolledOffset := c.vp.YOffset()
	if c.vp.AtBottom() {
		t.Fatal("test setup expected viewport to be scrolled above bottom")
	}

	c.SetExternalContent(numberedLines(40))

	if got := c.vp.YOffset(); got != scrolledOffset {
		t.Fatalf("content update should preserve scrollback offset, before=%d after=%d", scrolledOffset, got)
	}
	if c.vp.AtBottom() {
		t.Fatal("content update should not force viewport back to bottom while user is scrolled up")
	}
}

func TestChatView_SetSize_FollowsWhenAtBottom(t *testing.T) {
	c := NewChatView(80, 5)
	c.SetExternalContent(numberedLines(30))
	if !c.vp.AtBottom() {
		t.Fatal("test setup expected viewport at bottom")
	}

	c.SetSize(80, 3)

	if !c.vp.AtBottom() {
		t.Fatalf(
			"resize should keep a tail-following viewport at bottom; offset=%d lines=%d",
			c.vp.YOffset(),
			c.vp.TotalLineCount(),
		)
	}
}

func TestChatView_ToolActivityLines_ShowsLastThreeAndMatchesDoneByID(t *testing.T) {
	c := NewChatView(80, 5)
	c.AddToolCall("call-1", "main", "read_file", `{"path":"a.go"}`)
	c.AddToolCall("call-2", "main/worker", "exec", `{"command":"go test ./..."}`)
	c.AddToolCall("call-3", "main", "write_file", `{"path":"b.go"}`)
	c.AddToolCall("call-4", "main", "get_env", `{"name":"HOME"}`)

	c.UpdateToolCall("call-2", "main/worker", "exec", false, "ok")

	if !c.toolLog[1].Done {
		t.Fatal("UpdateToolCall should mark the matching ID done")
	}
	if c.toolLog[3].Done {
		t.Fatal("UpdateToolCall should not mark the last pending call when an ID matches earlier")
	}

	lines := c.ToolActivityLines(80, 3)
	if len(lines) != 3 {
		t.Fatalf("len(lines) = %d, want 3", len(lines))
	}
	joined := strings.Join(lines, "\n")
	if strings.Contains(joined, "read_file") {
		t.Fatalf("activity lines should show the last three calls, got:\n%s", joined)
	}
	for _, want := range []string{"done exec [main/worker]", "running write_file", "running get_env"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("activity lines missing %q:\n%s", want, joined)
		}
	}
}

func TestChatView_UpdateToolCall_CreatesEntryForMissingStart(t *testing.T) {
	c := NewChatView(80, 5)

	c.UpdateToolCall("call-sub", "main/worker", "exec", false, "ok")

	if len(c.toolLog) != 1 {
		t.Fatalf("len(toolLog) = %d, want 1", len(c.toolLog))
	}
	got := c.toolLog[0]
	if !got.Done || got.ID != "call-sub" || got.AgentID != "main/worker" || got.Name != "exec" {
		t.Fatalf("unexpected tool entry: %+v", got)
	}
	lines := c.ToolActivityLines(80, 3)
	if len(lines) != 1 || !strings.Contains(lines[0], "done exec [main/worker]") {
		t.Fatalf("unexpected activity lines: %#v", lines)
	}
}

func TestChatView_ToolActivityLinesTruncatesCommand(t *testing.T) {
	c := NewChatView(80, 5)
	command := `go test ./... && go test -race ./pkg/indexo && go vet ./...`
	c.AddToolCall("call-1", "main", "exec", `{"command":"`+command+`"}`)

	lines := c.ToolActivityLines(30, 12)
	if len(lines) != 1 || !strings.HasPrefix(lines[0], "running exec  command:") || !strings.HasSuffix(lines[0], "…") {
		t.Fatalf("expected one truncated command row, got %#v", lines)
	}
	if ansi.StringWidth(lines[0]) > 30 {
		t.Fatalf("tool activity exceeds content width: %q", lines[0])
	}
	if !strings.Contains(c.ToolLogModal(), command) {
		t.Fatal("full command missing from /tools")
	}
	if wide := c.ToolActivityLines(120, 3); len(wide) != 1 || !strings.Contains(wide[0], command) {
		t.Fatalf("widening did not restore command: %#v", wide)
	}
}

func TestChatView_ViewOmitsViewportPaddingRows(t *testing.T) {
	c := NewChatView(80, 5)
	c.SetExternalContent("hello")

	if got := c.View(); got != "hello" {
		t.Fatalf("View() = %q, want only transcript content", got)
	}
}

func TestChatView_ToolActivityLinesFitAndPreserveRecentCalls(t *testing.T) {
	c := NewChatView(80, 5)
	c.AddToolCall("1", "main", "exec", `{"command":"echo \"hello\"\n`+strings.Repeat("🎉", 40)+`"}`)
	c.AddToolCall("2", "main/worker", "read_file", `{"path":"/very/long/parent/directory/important.go"}`)
	c.AddToolCall("3", "main", "search", `{"query":"日本語のテキストを検索します"}`)
	for _, width := range []int{1, 20, 60, 120} {
		lines := c.ToolActivityLines(width, 3)
		if len(lines) != 3 {
			t.Fatalf("width %d: got %d rows, want three calls", width, len(lines))
		}
		for _, line := range lines {
			if strings.ContainsAny(line, "\r\n") || ansi.StringWidth(line) > width {
				t.Fatalf("width %d: invalid row %q", width, line)
			}
		}
		if width == 60 && !strings.Contains(lines[1], "important.go") {
			t.Fatalf("path lost filename: %q", lines[1])
		}
	}
	if lines := c.ToolActivityLines(0, 3); len(lines) != 0 {
		t.Fatalf("zero width returned rows: %#v", lines)
	}
	if got := c.toolLog[0].Preview; !strings.HasPrefix(got, "command: echo \"hello\"\n") {
		t.Fatalf("JSON escapes decoded incorrectly: %q", got)
	}
}

// truncateRunes must measure display width for the same reason: a rune-count
// truncation leaves wide characters overflowing the pane that renders them.
func TestTruncateRunes_UsesDisplayWidth(t *testing.T) {
	emoji := strings.Repeat("🎉", 40)
	if got := ansi.StringWidth(truncateRunes(emoji, 20)); got > 20 {
		t.Fatalf("truncateRunes produced %d columns, want <= 20", got)
	}
	if got := ansi.StringWidth(truncateRunes("日本語のテキスト", 8)); got > 8 {
		t.Fatalf("truncateRunes CJK produced %d columns, want <= 8", got)
	}
	if got := truncateRunes("short", 20); got != "short" {
		t.Fatalf("truncateRunes should pass through text that fits, got %q", got)
	}
	if got := truncateRunes("", 20); got != "" {
		t.Fatalf("truncateRunes empty = %q, want empty", got)
	}
}

func numberedLines(n int) string {
	lines := make([]string, n)
	for i := range lines {
		lines[i] = fmt.Sprintf("line %02d", i+1)
	}
	return strings.Join(lines, "\n")
}
