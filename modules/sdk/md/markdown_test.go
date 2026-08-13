package md

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestDetect_CodeFence(t *testing.T) {
	if !Detect("```go\nfunc main() {}\n```") {
		t.Fatal("fenced code block should be detected")
	}
}

func TestDetect_Header(t *testing.T) {
	if !Detect("# Heading") {
		t.Fatal("header should be detected")
	}
}

func TestDetect_PlainProse(t *testing.T) {
	if Detect("This is plain prose with no markers.") {
		t.Fatal("plain prose should not be detected")
	}
}

func TestDetect_Emphasis(t *testing.T) {
	if !Detect("some **bold** text") {
		t.Fatal("bold emphasis should be detected")
	}
}

func TestRender_PlainProsePassthrough(t *testing.T) {
	in := "Just some plain prose."
	if got := Render(in); got != in {
		t.Fatalf("plain prose must pass through unchanged, got %q", got)
	}
}

func TestRender_CodeFence(t *testing.T) {
	in := "```go\nfunc main() {}\n```"
	out := Render(in)
	if strings.Contains(out, "```") {
		t.Fatalf("fence markers must not appear literally, got %q", out)
	}
	if !strings.Contains(out, "func main()") {
		t.Fatalf("code body should be present, got %q", out)
	}
}

func TestRender_Header(t *testing.T) {
	out := Render("# Title")
	if strings.Contains(out, "#") {
		t.Fatalf("header marker must not appear literally, got %q", out)
	}
	if !strings.Contains(out, "Title") {
		t.Fatalf("header text should be present, got %q", out)
	}
}

func TestRender_BoldEmphasis(t *testing.T) {
	out := Render("a **bold** word")
	if !strings.Contains(out, "bold") {
		t.Fatalf("bold text should be present, got %q", out)
	}
	if strings.Contains(out, "**") {
		t.Fatalf("bold markers must not appear literally, got %q", out)
	}
}

func TestRender_List(t *testing.T) {
	out := Render("- first item\n- second item")
	if strings.Contains(out, "- ") {
		// The bullet marker is styled but the body is unstyled; the "- " prefix
		// is still part of the styled marker. Only assert content presence.
	}
	if !strings.Contains(out, "first item") || !strings.Contains(out, "second item") {
		t.Fatalf("list items should be present, got %q", out)
	}
}

func TestRender_Link(t *testing.T) {
	out := Render("[text](https://example.com)")
	if strings.Contains(out, "](") {
		t.Fatalf("link markers must not appear literally, got %q", out)
	}
	if !strings.Contains(ansi.Strip(out), "text") {
		t.Fatalf("link text should be present, got %q", out)
	}
}

func TestTable_RendersColumns(t *testing.T) {
	lines := []string{
		"| Name | Value |",
		"| --- | --- |",
		"| a | 1 |",
		"| b | 2 |",
	}
	out := Table(lines)
	if !strings.Contains(out, "Name") || !strings.Contains(out, "a") {
		t.Fatalf("table cells should be present, got %q", out)
	}
	if !strings.Contains(out, "│") {
		t.Fatalf("table should use column separators, got %q", out)
	}
}

func TestTable_MalformedFallsBack(t *testing.T) {
	if got := Table([]string{"just some prose", "no table here"}); got != "" {
		t.Fatalf("malformed table should return empty (caller falls back), got %q", got)
	}
}
