// Package md provides a lightweight markdown-to-ANSI renderer for terminal
// output. It is intentionally small and dependency-light: it converts the
// subset of CommonMark that appears in agent chat responses (fenced code
// blocks, headers, bold/italic, lists, links, and simple tables) into styled
// terminal text. Plain prose with no markdown markers is passed through
// unchanged, keeping the cheap path cheap.
//
// The output embeds ANSI escape sequences via lipgloss, which the bubbles
// viewport preserves. It is not a full CommonMark implementation; complex
// nesting degrades gracefully to unstyled text rather than erroring.
package md

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// Styles used by the renderer. Defined as package-level values so the cheap
// path (no markdown) allocates nothing.
var (
	codeStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#7aa6da")).
			Background(lipgloss.Color("#1e1e1e")).
			Padding(0, 1)
	codeBlockStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#cccccc")).
			Background(lipgloss.Color("#0f0f0f")).
			Padding(0, 1)
	headerStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#89CFF0"))
	emphasisBold = lipgloss.NewStyle().Bold(true)
	emphasisItal = lipgloss.NewStyle().Italic(true)
	linkStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#89CFF0")).Underline(true)
	bulletStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#89CFF0"))
	tableHeader  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#89CFF0"))
)

// Detect reports whether s contains markdown formatting markers that warrant
// rendering. It is the cheap-path gate: plain prose skips the renderer.
func Detect(s string) bool {
	if s == "" {
		return false
	}
	if strings.Contains(s, "\n```") || strings.Contains(s, "```") {
		return true
	}
	if strings.Contains(s, "\n#") || strings.Contains(s, "## ") || strings.Contains(s, "# ") {
		return true
	}
	if strings.Contains(s, "\n* ") || strings.Contains(s, "\n- ") || strings.Contains(s, "\n1. ") {
		return true
	}
	if strings.Contains(s, "\n| ") || strings.Contains(s, "**") || strings.Contains(s, "__") {
		return true
	}
	if strings.Contains(s, "](") {
		return true
	}
	return strings.Contains(s, "\n> ")
}

// Render converts markdown s into ANSI-styled terminal text. Plain prose is
// returned unchanged.
func Render(s string) string {
	if s == "" {
		return ""
	}
	if !Detect(s) {
		return s
	}
	lines := strings.Split(s, "\n")
	var out []string
	inCode := false
	for _, line := range lines {
		trimmed := strings.TrimRight(line, " \t")
		switch {
		case strings.HasPrefix(trimmed, "```"):
			inCode = !inCode
			// The fence line itself renders as a divider-ish blank line.
			if !inCode {
				out = append(out, "")
			}
			continue
		case inCode:
			out = append(out, codeBlockStyle.Render(trimmed))
		default:
			out = append(out, renderInline(trimmed))
		}
	}
	return strings.Join(out, "\n")
}

// renderInline applies inline markdown styling to a single line: headers,
// bold, italic, links, code spans, and list bullets.
func renderInline(line string) string {
	line = strings.TrimRight(line, " \t")

	// Header: #..###### followed by a space.
	if _, rest, ok := header(line); ok {
		return headerStyle.Render(rest)
	}
	// Blockquote.
	if strings.HasPrefix(line, "> ") {
		return bulletStyle.Render(line)
	}
	// List bullet / numbered item: keep the marker styled, body unstyled.
	if b, rest, ok := listItem(line); ok {
		return bulletStyle.Render(b) + " " + renderEmphasis(rest)
	}
	// Table separator row (--- | ---) renders as a plain divider.
	if isTableSep(line) {
		return ""
	}
	return renderEmphasis(line)
}

// header splits a markdown header line. Returns the body text and ok.
func header(line string) (prefix, body string, ok bool) {
	i := 0
	for i < len(line) && line[i] == '#' {
		i++
	}
	if i == 0 || i > 6 || i >= len(line) || line[i] != ' ' {
		return "", "", false
	}
	return line[:i], line[i+1:], true
}

// listItem splits a list bullet ("- ", "* ", "1. ") or task item ("- [x] ").
func listItem(line string) (marker, body string, ok bool) {
	for _, m := range []string{"- [x] ", "- [ ] ", "- ", "* ", "1. ", "2. ", "3. "} {
		if strings.HasPrefix(line, m) {
			return strings.TrimSuffix(m, " "), line[len(m):], true
		}
	}
	return "", "", false
}

func isTableSep(line string) bool {
	if !strings.Contains(line, "|") {
		return false
	}
	fields := strings.Split(line, "|")
	for _, f := range fields {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		if !strings.HasPrefix(f, "-") {
			return false
		}
	}
	return true
}

// renderEmphasis applies bold/italic/code-span/link styling inline.
func renderEmphasis(line string) string {
	// Code spans first so backticks aren't mistaken for emphasis.
	line = codeSpans(line)
	line = bold(line)
	line = italic(line)
	line = links(line)
	return line
}

// codeSpans styles `...` spans as inline code.
func codeSpans(line string) string {
	if !strings.Contains(line, "`") {
		return line
	}
	var sb strings.Builder
	rest := line
	for {
		idx := strings.Index(rest, "`")
		if idx < 0 {
			sb.WriteString(rest)
			break
		}
		sb.WriteString(rest[:idx])
		rest = rest[idx+1:]
		end := strings.Index(rest, "`")
		if end < 0 {
			sb.WriteString("`")
			sb.WriteString(rest)
			break
		}
		sb.WriteString(codeStyle.Render(rest[:end]))
		rest = rest[end+1:]
	}
	return sb.String()
}

// bold styles **...** and __...__ as bold.
func bold(line string) string {
	if !strings.Contains(line, "**") && !strings.Contains(line, "__") {
		return line
	}
	line = replaceDelimited(line, "**", "**", emphasisBold)
	line = replaceDelimited(line, "__", "__", emphasisBold)
	return line
}

// italic styles *...* and _..._ as italic.
func italic(line string) string {
	// Guard against stray single asterisks that are list markers.
	if !strings.Contains(line, "*") && !strings.Contains(line, "_") {
		return line
	}
	line = replaceDelimited(line, "*", "*", emphasisItal)
	line = replaceDelimited(line, "_", "_", emphasisItal)
	return line
}

// links styles [text](url) as underlined text.
func links(line string) string {
	if !strings.Contains(line, "](") {
		return line
	}
	var sb strings.Builder
	rest := line
	for {
		open := strings.Index(rest, "[")
		if open < 0 {
			sb.WriteString(rest)
			break
		}
		sb.WriteString(rest[:open])
		rest = rest[open+1:]
		close := strings.Index(rest, "](")
		if close < 0 {
			sb.WriteString("[")
			sb.WriteString(rest)
			break
		}
		text := rest[:close]
		rest = rest[close+2:]
		end := strings.Index(rest, ")")
		if end < 0 {
			sb.WriteString(linkStyle.Render(text))
			sb.WriteString(rest)
			break
		}
		sb.WriteString(linkStyle.Render(text))
		rest = rest[end+1:]
	}
	return sb.String()
}

// replaceDelimited styles content between open and close delimiters.
func replaceDelimited(s, open, close string, style lipgloss.Style) string {
	if !strings.Contains(s, open) {
		return s
	}
	var sb strings.Builder
	rest := s
	for {
		i := strings.Index(rest, open)
		if i < 0 {
			sb.WriteString(rest)
			break
		}
		sb.WriteString(rest[:i])
		rest = rest[i+len(open):]
		j := strings.Index(rest, close)
		if j < 0 {
			sb.WriteString(open)
			sb.WriteString(rest)
			break
		}
		sb.WriteString(style.Render(rest[:j]))
		rest = rest[j+len(close):]
	}
	return sb.String()
}

// Table renders a simple markdown table (a header row, a separator row, and
// data rows) as aligned columns. Degrades gracefully: if the table is
// malformed, it falls back to the raw lines.
func Table(lines []string) string {
	// Find the header row and separator row.
	sepIdx := -1
	headerIdx := -1
	for i := 0; i < len(lines); i++ {
		if isTableSep(lines[i]) {
			sepIdx = i
			headerIdx = i - 1
			break
		}
	}
	if sepIdx < 0 || headerIdx < 0 || headerIdx >= len(lines) {
		return ""
	}
	cols := splitRow(lines[headerIdx])
	if len(cols) == 0 {
		return ""
	}
	widths := make([]int, len(cols))
	for i, c := range cols {
		widths[i] = displayWidth(c)
	}
	// Accumulate data rows.
	var dataRows [][]string
	for _, line := range lines[sepIdx+1:] {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "`") {
			continue
		}
		row := splitRow(line)
		dataRows = append(dataRows, row)
		for i, c := range row {
			if i < len(widths) && displayWidth(c) > widths[i] {
				widths[i] = displayWidth(c)
			}
		}
	}
	var sb strings.Builder
	sb.WriteString(formatRow(cols, widths, true))
	sb.WriteString("\n")
	for _, row := range dataRows {
		sb.WriteString(formatRow(row, widths, false))
		sb.WriteString("\n")
	}
	return sb.String()
}

// splitRow splits a markdown table line on "|" and trims cells.
func splitRow(line string) []string {
	line = strings.Trim(line, "|")
	parts := strings.Split(line, "|")
	cells := make([]string, 0, len(parts))
	for _, p := range parts {
		cells = append(cells, strings.TrimSpace(p))
	}
	return cells
}

// formatRow pads cells to widths and styles the header row.
func formatRow(row []string, widths []int, isHeader bool) string {
	cells := make([]string, len(row))
	for i, c := range row {
		pad := 0
		if i < len(widths) {
			pad = widths[i] - displayWidth(c)
		}
		if pad < 0 {
			pad = 0
		}
		cell := c + strings.Repeat(" ", pad)
		if isHeader {
			cell = tableHeader.Render(cell)
		}
		cells[i] = cell
	}
	return "│ " + strings.Join(cells, " │ ") + " │"
}

// displayWidth returns the visible width of s, ignoring ANSI escapes.
func displayWidth(s string) int {
	return lipgloss.Width(s)
}
