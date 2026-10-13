package server

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"strings"
	"testing"

	"github.com/mattdurham/wllr/modules/sdk"
)

// TestMessageHTMLUserVerbatimEscaped proves a user message is escaped, not
// markdown-rendered: prompts are shown exactly as typed (matching the terminal
// extension), and injected markup cannot execute.
func TestMessageHTMLUserVerbatimEscaped(t *testing.T) {
	got := MessageHTML(sdk.RoleUser, "**not bold** <img src=x onerror=alert(1)>")
	if !strings.Contains(got, "**not bold**") {
		t.Errorf("user content was markdown-rendered: %q", got)
	}
	if strings.Contains(got, "<img") {
		t.Errorf("user markup was not escaped: %q", got)
	}
}

// TestMessageHTMLAssistantRendersBold proves assistant markdown goes through the
// same renderer the terminal uses, so **bold** becomes bold styling.
func TestMessageHTMLAssistantRendersBold(t *testing.T) {
	got := MessageHTML(sdk.RoleAssistant, "this is **bold** text")
	if !strings.Contains(got, "font-weight:700;") {
		t.Errorf("bold not rendered: %q", got)
	}
	if !strings.Contains(got, "bold") {
		t.Errorf("text lost: %q", got)
	}
}

// TestMessageHTMLAssistantHeaderStyled proves a markdown header renders with the
// header style (bold + the accent colour the md package uses).
func TestMessageHTMLAssistantHeaderStyled(t *testing.T) {
	got := MessageHTML(sdk.RoleAssistant, "## Title")
	if !strings.Contains(got, "font-weight:700;") {
		t.Errorf("header not bold: %q", got)
	}
	if !strings.Contains(got, "#89cff0") {
		t.Errorf("header colour missing: %q", got)
	}
}

// TestMessageHTMLAssistantCodeBlockStyled proves a fenced code block renders with
// its background style, which is the marker that the ANSI passed through the
// converter rather than being shown raw.
func TestMessageHTMLAssistantCodeBlockStyled(t *testing.T) {
	got := MessageHTML(sdk.RoleAssistant, "before\n```\ncode line\n```\nafter")
	if !strings.Contains(got, "background-color:") {
		t.Errorf("code block background missing: %q", got)
	}
	if !strings.Contains(got, "code line") {
		t.Errorf("code text lost: %q", got)
	}
	if strings.Contains(got, "\x1b") {
		t.Errorf("raw escapes leaked into HTML: %q", got)
	}
}

// TestMessageHTMLAssistantPlainPassthrough proves plain prose (no markdown
// markers) is returned as escaped text, so the cheap path stays cheap.
func TestMessageHTMLAssistantPlainPassthrough(t *testing.T) {
	got := MessageHTML(sdk.RoleAssistant, "just some words")
	if got != "just some words" {
		t.Errorf("plain prose = %q, want unchanged", got)
	}
}

// TestMessageHTMLEmpty proves an empty body renders empty rather than a stray
// span.
func TestMessageHTMLEmpty(t *testing.T) {
	if got := MessageHTML(sdk.RoleAssistant, ""); got != "" {
		t.Errorf("empty = %q, want empty", got)
	}
}

// TestMessageHTMLNeverEmitsRawEscapes is the cross-cutting safety property: no
// matter the input, the output carries no control bytes, so it is always safe
// markup.
func TestMessageHTMLNeverEmitsRawEscapes(t *testing.T) {
	inputs := []string{
		"plain",
		"# head\n\n- a\n- b",
		"```go\nfunc main() {}\n```",
		"[link](https://example.com) and `code`",
		"> quote\n\n| a | b |\n| - | - |\n| 1 | 2 |",
		"text with <script> and \x1b[31m literal escape",
	}
	for _, in := range inputs {
		got := MessageHTML(sdk.RoleAssistant, in)
		if strings.Contains(got, "\x1b") {
			t.Errorf("input %q produced raw escapes: %q", in, got)
		}
	}
}
