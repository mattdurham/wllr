package harness

import (
	"fmt"
	"strings"
	"testing"
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
		t.Fatalf("content update should follow bottom when already at bottom; offset=%d lines=%d", c.vp.YOffset(), c.vp.TotalLineCount())
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

func numberedLines(n int) string {
	lines := make([]string, n)
	for i := range lines {
		lines[i] = fmt.Sprintf("line %02d", i+1)
	}
	return strings.Join(lines, "\n")
}
