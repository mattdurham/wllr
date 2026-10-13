package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// Mouse text selection for the chat transcript. The transcript is rendered as a
// single ANSI string into a bubbles viewport, so a selection is expressed in
// *viewport-relative screen cells* rather than byte offsets into the content:
// row 0 is the first visible transcript line, col 0 is its left edge. Copying
// therefore yields exactly the characters painted on screen, and highlighting
// is a per-line span overlay. This deliberately avoids replicating the
// viewport's soft-wrap maths to map cells back to content bytes.

import (
	"strings"

	lipgloss "charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// selCell is a viewport-relative screen coordinate within the chat transcript.
type selCell struct {
	Row int
	Col int
}

// selectionStyle paints the selected span. A dark blue background with white
// foreground reads as "selected" against both dark transcript text and dimmed
// metadata, in the same family as the picker's selected style.
var selectionStyle = lipgloss.NewStyle().
	Background(lipgloss.Color("#1A4A8A")).
	Foreground(lipgloss.Color("#FFFFFF"))

// BeginSelection starts a selection at the given viewport-relative cell. The
// selection is active but not yet "dirty": a press with no drag copies nothing.
func (c *ChatView) BeginSelection(col, row int) {
	start := c.clampCell(col, row)
	c.selAnchor = start
	c.selFocus = start
	c.selActive = true
	c.selDirty = false
}

// ExtendSelection moves the selection focus to the given cell. The focus is
// clamped to the currently visible transcript so a drag beyond the edges keeps
// tracking the nearest valid cell. No-op when no selection is active.
func (c *ChatView) ExtendSelection(col, row int) {
	if !c.selActive {
		return
	}
	c.selFocus = c.clampCell(col, row)
	c.selDirty = c.selFocus != c.selAnchor
}

// HasSelection reports whether a selection is in progress (a press has landed
// and no release has cleared it yet).
func (c ChatView) HasSelection() bool { return c.selActive }

// ClearSelection ends any selection without copying.
func (c *ChatView) ClearSelection() {
	c.selActive = false
	c.selDirty = false
	c.selAnchor = selCell{}
	c.selFocus = selCell{}
}

// EndSelection returns the selected text (empty when the selection never moved)
// and clears the selection. ok is true only when there is real text to copy.
func (c *ChatView) EndSelection() (string, bool) {
	text, ok := c.selectedText()
	c.ClearSelection()
	return text, ok
}

// SelectedText returns the currently selected text without clearing it.
func (c ChatView) SelectedText() string {
	text, _ := c.selectedText()
	return text
}

// clampCell restricts a raw viewport-relative coordinate to the visible
// transcript area.
func (c ChatView) clampCell(col, row int) selCell {
	lines := c.visibleLines()
	if len(lines) == 0 {
		return selCell{}
	}
	if row < 0 {
		row = 0
	}
	if row > len(lines)-1 {
		row = len(lines) - 1
	}
	maxCol := c.width - 1
	if maxCol < 0 {
		maxCol = 0
	}
	if col < 0 {
		col = 0
	}
	if col > maxCol {
		col = maxCol
	}
	return selCell{Row: row, Col: col}
}

// selectionBounds returns the normalized (startRow, startCol, endRow, endCol)
// of the current selection. ok is false when there is no non-empty selection.
func (c ChatView) selectionBounds() (startRow, startCol, endRow, endCol int, ok bool) {
	if !c.selActive || !c.selDirty {
		return 0, 0, 0, 0, false
	}
	a, f := c.selAnchor, c.selFocus
	if a.Row > f.Row || (a.Row == f.Row && a.Col > f.Col) {
		a, f = f, a
	}
	if a.Row == f.Row && a.Col == f.Col {
		return 0, 0, 0, 0, false
	}
	return a.Row, a.Col, f.Row, f.Col, true
}

// visibleLines returns the transcript lines currently painted, ANSI-stripped and
// with trailing padding spaces removed, so indices line up with screen rows and
// copied text has no layout padding. It mirrors what View renders because it is
// derived from the viewport's own output.
func (c ChatView) visibleLines() []string {
	raw := c.vp.View()
	if raw == "" {
		return nil
	}
	raw = strings.TrimRight(raw, " \n")
	if raw == "" {
		return nil
	}
	lines := strings.Split(raw, "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(ansi.Strip(lines[i]), " ")
	}
	return lines
}

// selectedText renders the selected span as plain text, one screen line per
// output line.
func (c ChatView) selectedText() (string, bool) {
	startRow, startCol, endRow, endCol, ok := c.selectionBounds()
	if !ok {
		return "", false
	}
	lines := c.visibleLines()
	if len(lines) == 0 || startRow >= len(lines) {
		return "", false
	}
	if endRow >= len(lines) {
		endRow = len(lines) - 1
	}
	var b strings.Builder
	for r := startRow; r <= endRow; r++ {
		line := lines[r]
		width := ansi.StringWidth(line)
		colStart, colEnd := 0, width
		if r == startRow {
			colStart = startCol
		}
		if r == endRow {
			colEnd = endCol
		}
		if colStart < 0 {
			colStart = 0
		}
		if colEnd > width {
			colEnd = width
		}
		if colEnd < colStart {
			colEnd = colStart
		}
		b.WriteString(ansi.Cut(line, colStart, colEnd))
		if r != endRow {
			b.WriteString("\n")
		}
	}
	text := b.String()
	if strings.TrimSpace(text) == "" {
		return "", false
	}
	return text, true
}

// highlightSelection overlays the selection style on the affected columns of the
// rendered viewport output. It must be called with the un-highlighted render.
func (c ChatView) highlightSelection(view string) string {
	startRow, startCol, endRow, endCol, ok := c.selectionBounds()
	if !ok {
		return view
	}
	lines := strings.Split(view, "\n")
	for r := startRow; r <= endRow && r < len(lines); r++ {
		colStart, colEnd := 0, lineWidth(lines[r])
		if r == startRow {
			colStart = startCol
		}
		if r == endRow {
			colEnd = endCol
		}
		lines[r] = highlightCols(lines[r], colStart, colEnd)
	}
	return strings.Join(lines, "\n")
}

// highlightCols wraps the display-column span [colStart, colEnd) of an ANSI
// line in the selection style, leaving the rest of the line intact. ansi.Cut is
// display-width aware and does not break escape sequences.
func highlightCols(line string, colStart, colEnd int) string {
	width := ansi.StringWidth(line)
	if colStart < 0 {
		colStart = 0
	}
	if colEnd > width {
		colEnd = width
	}
	if colEnd <= colStart {
		return line
	}
	left := ansi.Cut(line, 0, colStart)
	mid := ansi.Cut(line, colStart, colEnd)
	right := ansi.Cut(line, colEnd, width)
	return left + selectionStyle.Render(mid) + right
}

// lineWidth returns the visible width of a rendered line with trailing padding
// spaces excluded, so a full-line selection does not highlight layout padding.
func lineWidth(line string) int {
	return ansi.StringWidth(strings.TrimRight(ansi.Strip(line), " "))
}
