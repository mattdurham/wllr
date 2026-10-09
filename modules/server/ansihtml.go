package server

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// ansihtml.go converts ANSI-styled terminal text to HTML.
//
// It exists because the primary transcript — the "chat" scene area — is built
// by the bundled agents extension, which renders markdown to ANSI via
// modules/sdk/md and streams that styled text into scene-graph nodes. The
// terminal renderer feeds those bytes straight to lipgloss; a browser needs
// spans. Converting the styled text (rather than re-rendering the markdown)
// means the browser shows exactly what the terminal shows: the extension owns
// the markdown decision, and this file only translates its colour/attribute
// vocabulary into CSS.
//
// The converter is total: an unparseable or truncated escape is dropped rather
// than emitted, and all text is HTML-escaped, so its output is safe to insert
// as markup. It is the one place a message body becomes HTML, so escaping here
// is what keeps model output from injecting markup.

import (
	"fmt"
	"html"
	"strconv"
	"strings"
)

// ansiStyle is the accumulated SGR state while scanning a string. An empty
// string colour means "the terminal default", which maps to no CSS declaration
// so the surrounding theme shows through.
type ansiStyle struct {
	fg, bg    string
	bold      bool
	italic    bool
	underline bool
	faint     bool
}

// isZero reports whether the style carries no formatting, so a run can be
// emitted as plain escaped text with no wrapper span.
func (s ansiStyle) isZero() bool {
	return s == ansiStyle{}
}

// css renders the style as a declaration list for a span's style attribute.
func (s ansiStyle) css() string {
	var b strings.Builder
	if s.fg != "" {
		fmt.Fprintf(&b, "color:%s;", s.fg)
	}
	if s.bg != "" {
		fmt.Fprintf(&b, "background-color:%s;", s.bg)
	}
	if s.bold {
		b.WriteString("font-weight:700;")
	}
	if s.italic {
		b.WriteString("font-style:italic;")
	}
	if s.underline {
		b.WriteString("text-decoration:underline;")
	}
	if s.faint {
		b.WriteString("opacity:.6;")
	}
	return b.String()
}

// ansi16 is the standard + bright 16-colour palette, matching the usual xterm
// defaults (the same colours lipgloss falls back to for the ANSI names).
var ansi16 = [16]string{
	"#000000", "#cd0000", "#00cd00", "#cdcd00",
	"#0000ee", "#cd00cd", "#00cdcd", "#e5e5e5",
	"#7f7f7f", "#ff0000", "#00ff00", "#ffff00",
	"#5c5cff", "#ff00ff", "#00ffff", "#ffffff",
}

// ansi256 resolves an xterm 256-colour index: the 16 base colours, a 6x6x6
// colour cube, then a 24-step greyscale ramp.
func ansi256(n int) string {
	if n < 0 {
		return ""
	}
	if n < 16 {
		return ansi16[n]
	}
	if n < 232 {
		n -= 16
		r, g, b := n/36, (n/6)%6, n%6
		scale := func(v int) int {
			if v == 0 {
				return 0
			}
			return 55 + v*40
		}
		return fmt.Sprintf("#%02x%02x%02x", scale(r), scale(g), scale(b))
	}
	if n < 256 {
		v := 8 + (n-232)*10
		return fmt.Sprintf("#%02x%02x%02x", v, v, v)
	}
	return ""
}

// isCSIFinal reports whether b terminates a CSI sequence (final byte range
// 0x40–0x7E). Parameter and intermediate bytes fall below it.
func isCSIFinal(b byte) bool {
	return b >= 0x40 && b <= 0x7E
}

// ANSItoHTML converts s to HTML, translating SGR colour/attribute sequences to
// spans and dropping every other control sequence. Text is escaped, so the
// result is safe to insert into the DOM as markup.
func ANSItoHTML(s string) string {
	if s == "" {
		return ""
	}
	// Fast path: no escapes at all (user prompts, plain prose) skips parsing.
	if !strings.ContainsRune(s, 0x1b) {
		return html.EscapeString(s)
	}

	var out strings.Builder
	var run strings.Builder
	style := ansiStyle{}

	flush := func() {
		if run.Len() == 0 {
			return
		}
		text := html.EscapeString(run.String())
		run.Reset()
		if style.isZero() {
			out.WriteString(text)
			return
		}
		fmt.Fprintf(&out, `<span style="%s">%s</span>`, style.css(), text)
	}

	for i := 0; i < len(s); {
		c := s[i]
		if c != 0x1b {
			// Drop NUL, which would corrupt the markup; keep newlines, tabs, and
			// the other C0 controls, which the surrounding white-space:pre and the
			// HTML parser handle.
			if c == 0x00 {
				i++
				continue
			}
			run.WriteByte(c)
			i++
			continue
		}
		// An escape sequence ends the current styled run.
		if i+1 >= len(s) {
			break // trailing lone ESC: nothing to render
		}
		flush()
		switch s[i+1] {
		case '[':
			j := i + 2
			for j < len(s) && !isCSIFinal(s[j]) {
				j++
			}
			if j >= len(s) {
				i = len(s)
				break
			}
			if s[j] == 'm' {
				applySGR(&style, s[i+2:j])
			}
			i = j + 1
		case ']':
			// OSC (e.g. a hyperlink or window title): skip to BEL or ST.
			j := i + 2
			for j < len(s) {
				if s[j] == 0x07 {
					j++
					break
				}
				if s[j] == 0x1b && j+1 < len(s) && s[j+1] == '\\' {
					j += 2
					break
				}
				j++
			}
			i = j
		default:
			// Two-byte escape (charset select, etc.): drop it.
			i += 2
		}
	}
	flush()
	return out.String()
}

// applySGR folds a CSI parameter string (the bytes between "ESC [" and "m")
// into style. Unknown parameters are ignored, so a newer attribute degrades to
// "unstyled" rather than corrupting the run.
func applySGR(style *ansiStyle, params string) {
	nums := parseSGRParams(params)
	for i := 0; i < len(nums); i++ {
		switch n := nums[i]; {
		case n == 0:
			*style = ansiStyle{}
		case n == 1:
			style.bold = true
		case n == 2:
			style.faint = true
		case n == 3:
			style.italic = true
		case n == 4:
			style.underline = true
		case n == 22:
			style.bold, style.faint = false, false
		case n == 23:
			style.italic = false
		case n == 24:
			style.underline = false
		case n == 39:
			style.fg = ""
		case n == 49:
			style.bg = ""
		case n >= 30 && n <= 37:
			style.fg = ansi16[n-30]
		case n >= 90 && n <= 97:
			style.fg = ansi16[n-90+8]
		case n >= 40 && n <= 47:
			style.bg = ansi16[n-40]
		case n >= 100 && n <= 107:
			style.bg = ansi16[n-100+8]
		case n == 38 || n == 48:
			// Extended colour: 38;5;N (256) or 38;2;R;G;B (truecolor). Consume
			// the following parameters so they are not misread as their own SGR
			// codes.
			color, used := parseExtendedColor(nums[i+1:])
			if color != "" {
				if n == 38 {
					style.fg = color
				} else {
					style.bg = color
				}
			}
			i += used
		}
	}
}

// parseSGRParams splits a parameter string into integers. CSI allows
// sub-parameters separated by ':' (e.g. underline styles) and an empty string
// means 0 (reset).
func parseSGRParams(params string) []int {
	if params == "" {
		return []int{0}
	}
	fields := strings.FieldsFunc(params, func(r rune) bool { return r == ';' || r == ':' })
	nums := make([]int, 0, len(fields))
	for _, f := range fields {
		if f == "" {
			nums = append(nums, 0)
			continue
		}
		n, err := strconv.Atoi(f)
		if err != nil {
			nums = append(nums, 0)
			continue
		}
		nums = append(nums, n)
	}
	return nums
}

// parseExtendedColor resolves the tail of a 38/48 extended colour into a CSS
// colour. It returns the colour (empty when unrecognized) and how many
// parameters it consumed, so the caller can skip them.
func parseExtendedColor(rest []int) (string, int) {
	if len(rest) == 0 {
		return "", 0
	}
	switch rest[0] {
	case 5: // 256-colour index
		if len(rest) < 2 {
			return "", 1
		}
		return ansi256(rest[1]), 2
	case 2: // truecolor r;g;b
		if len(rest) < 4 {
			return "", len(rest)
		}
		clamp := func(v int) int {
			if v < 0 {
				return 0
			}
			if v > 255 {
				return 255
			}
			return v
		}
		return fmt.Sprintf("#%02x%02x%02x", clamp(rest[1]), clamp(rest[2]), clamp(rest[3])), 4
	}
	return "", 1
}
