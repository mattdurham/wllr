package server

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"strings"
	"testing"
)

// TestANSItoHTMLEscapesPlainText proves the converter escapes markup, so a
// message containing HTML cannot inject tags. This is the property that makes
// the output safe to insert with innerHTML.
func TestANSItoHTMLEscapesPlainText(t *testing.T) {
	got := ANSItoHTML(`<script>alert("x")</script> & <b>`)
	want := `&lt;script&gt;alert(&#34;x&#34;)&lt;/script&gt; &amp; &lt;b&gt;`
	if got != want {
		t.Errorf("escape:\n got %q\nwant %q", got, want)
	}
}

// TestANSItoHTMLEscapesInsideStyledRun proves text inside a styled run is
// escaped too: the escaping must not be skipped on the styled path.
func TestANSItoHTMLEscapesInsideStyledRun(t *testing.T) {
	got := ANSItoHTML("\x1b[1m<a>\x1b[0m")
	if !strings.Contains(got, "&lt;a&gt;") {
		t.Fatalf("styled run not escaped: %q", got)
	}
	if strings.Contains(got, "<a>") {
		t.Fatalf("raw markup leaked: %q", got)
	}
}

// TestANSItoHTMLAttributes covers the SGR attributes the markdown renderer
// emits (bold for emphasis and headers, italic, underline for links).
func TestANSItoHTMLAttributes(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string // substring expected in output
	}{
		{"bold", "\x1b[1mhi\x1b[0m", "font-weight:700;"},
		{"italic", "\x1b[3mhi\x1b[0m", "font-style:italic;"},
		{"underline", "\x1b[4mhi\x1b[0m", "text-decoration:underline;"},
		{"faint", "\x1b[2mhi\x1b[0m", "opacity:.6;"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ANSItoHTML(tc.in)
			if !strings.Contains(got, tc.want) {
				t.Errorf("ANSItoHTML(%q) = %q, want %q", tc.in, got, tc.want)
			}
			if !strings.Contains(got, "hi") {
				t.Errorf("ANSItoHTML(%q) dropped text: %q", tc.in, got)
			}
		})
	}
}

// TestANSItoHTMLColors covers the colour forms the palette produces: 16-colour
// foreground/background, 256-colour, and truecolor.
func TestANSItoHTMLColors(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"fg16", "\x1b[31mX\x1b[39m", "color:#cd0000;"},
		{"fgBright", "\x1b[94mX\x1b[39m", "color:#5c5cff;"},
		{"bg16", "\x1b[44mX\x1b[49m", "background-color:#0000ee;"},
		{"fg256", "\x1b[38;5;196mX\x1b[39m", "color:#ff0000;"},
		{"bg256", "\x1b[48;5;232mX\x1b[49m", "background-color:#080808;"},
		{"truecolor", "\x1b[38;2;1;2;3mX\x1b[39m", "color:#010203;"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ANSItoHTML(tc.in)
			if !strings.Contains(got, tc.want) {
				t.Errorf("ANSItoHTML(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestANSItoHTMLResetClosesSpan proves a reset closes the styled run rather than
// letting the colour bleed onto following text.
func TestANSItoHTMLResetClosesSpan(t *testing.T) {
	got := ANSItoHTML("\x1b[31mred\x1b[0mplain")
	if !strings.Contains(got, "color:#cd0000;") {
		t.Errorf("red run missing colour: %q", got)
	}
	if !strings.HasSuffix(got, "plain") {
		t.Errorf("post-reset text should be unstyled and trailing: %q", got)
	}
	if strings.Count(got, "<span") != 1 {
		t.Errorf("expected exactly one span, got %q", got)
	}
}

// TestANSItoHTMLNonSGRAndOSC proves non-SGR control sequences are dropped
// rather than emitted as garbage, and a truncated escape does not panic.
func TestANSItoHTMLNonSGRAndOSC(t *testing.T) {
	// OSC 8 hyperlink (a real thing an extension could emit), then text.
	in := "\x1b]8;;https://example.com\x07link\x1b]8;;\x07 \x1b[2Jcleared"
	got := ANSItoHTML(in)
	if strings.Contains(got, "\x1b") {
		t.Errorf("control bytes leaked: %q", got)
	}
	if !strings.Contains(got, "link") || !strings.Contains(got, "cleared") {
		t.Errorf("dropped visible text: %q", got)
	}
	// A trailing partial escape must not panic or emit bytes.
	if got := ANSItoHTML("abc\x1b["); strings.Contains(got, "\x1b") {
		t.Errorf("truncated escape leaked: %q", got)
	}
}

// TestANSItoHTMLMultiParam covers a combined sequence (bold + colour) and the
// empty-parameter reset form ("ESC[m") lipgloss sometimes emits.
func TestANSItoHTMLMultiParam(t *testing.T) {
	got := ANSItoHTML("\x1b[1;31mX\x1b[m")
	if !strings.Contains(got, "font-weight:700;") || !strings.Contains(got, "color:#cd0000;") {
		t.Errorf("combined SGR not applied: %q", got)
	}
	if strings.Count(got, "<span") != 1 {
		t.Errorf("expected one span, got %q", got)
	}
}

// TestANSItoHTMLNewlinesPreserved proves newlines survive so the surrounding
// white-space:pre styling reproduces the terminal's line structure.
func TestANSItoHTMLNewlinesPreserved(t *testing.T) {
	got := ANSItoHTML("a\nb")
	if got != "a\nb" {
		t.Errorf("newline handling = %q, want %q", got, "a\nb")
	}
}
