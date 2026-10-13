package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// Tests for mouse drag-to-select text copying in the chat transcript. Selection
// is stored in viewport-relative screen cells; a left-button press starts it,
// drag motion extends it, and release copies the selected text via the
// clipboard command. A press without drag copies nothing.

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// newSelectionModel builds a model with a fixed transcript and renders it once
// so the chat viewport geometry is recorded for mouse hit-testing.
func newSelectionModel(t *testing.T, content string) Model {
	t.Helper()
	m := newTestModel()
	m.width = 80
	m.height = 30
	m.chat.SetExternalContent(content)
	m.View() // syncLayout + render records liveState.chatGeom
	return m
}

func TestChatView_SelectionSingleLine(t *testing.T) {
	c := NewChatView(40, 5)
	c.SetExternalContent("hello world\nsecond line\nthird line")

	c.BeginSelection(6, 0)
	c.ExtendSelection(11, 0)
	if !c.HasSelection() {
		t.Fatal("selection should be active after press + drag")
	}
	if got, want := c.SelectedText(), "world"; got != want {
		t.Errorf("SelectedText = %q, want %q", got, want)
	}
	text, ok := c.EndSelection()
	if !ok || text != "world" {
		t.Errorf("EndSelection = (%q, %v), want (%q, true)", text, ok, "world")
	}
	if c.HasSelection() {
		t.Error("EndSelection should clear the selection")
	}
}

func TestChatView_SelectionMultiLine(t *testing.T) {
	c := NewChatView(40, 5)
	c.SetExternalContent("hello world\nsecond line\nthird line")

	c.BeginSelection(6, 0)
	c.ExtendSelection(6, 1)
	if got, want := c.SelectedText(), "world\nsecond"; got != want {
		t.Errorf("SelectedText = %q, want %q", got, want)
	}
}

func TestChatView_BareClickCopiesNothing(t *testing.T) {
	c := NewChatView(40, 5)
	c.SetExternalContent("hello world")

	c.BeginSelection(3, 0)
	text, ok := c.EndSelection()
	if ok || text != "" {
		t.Errorf("bare click produced (%q, %v), want empty and false", text, ok)
	}
}

func TestChatView_SelectionClampsToVisible(t *testing.T) {
	c := NewChatView(40, 5)
	c.SetExternalContent("alpha\nbeta\ngamma")

	// Drag far beyond the transcript: focus must clamp to the last visible cell.
	c.BeginSelection(0, 0)
	c.ExtendSelection(999, 999)
	if got, want := c.SelectedText(), "alpha\nbeta\ngamma"; got != want {
		t.Errorf("SelectedText = %q, want %q", got, want)
	}
}

func TestHighlightCols_OnlySelectedSpan(t *testing.T) {
	line := "abcdef"
	out := highlightCols(line, 2, 4)
	if ansi.Strip(out) != line {
		t.Errorf("highlight changed the text: %q", ansi.Strip(out))
	}
	if out == line {
		t.Error("highlight did not apply any styling")
	}
	if !strings.HasPrefix(out, "ab") {
		t.Errorf("prefix before selection altered: %q", out)
	}
	if !strings.HasSuffix(out, "ef") {
		t.Errorf("suffix after selection altered: %q", out)
	}
}

func TestChatView_ViewHighlightKeepsText(t *testing.T) {
	c := NewChatView(40, 5)
	c.SetExternalContent("hello world\nsecond line")

	plain := c.View()
	c.BeginSelection(6, 0)
	c.ExtendSelection(11, 0)
	highlighted := c.View()

	if highlighted == plain {
		t.Error("View did not apply the selection highlight")
	}
	if ansi.Strip(highlighted) != ansi.Strip(plain) {
		t.Errorf("highlight changed rendered text:\n got  %q\n want %q", ansi.Strip(highlighted), ansi.Strip(plain))
	}
	// Deselection restores the plain render.
	c.ClearSelection()
	if c.View() != plain {
		t.Error("clearing the selection did not restore the plain render")
	}
}

func TestMouseSelection_DragCopies(t *testing.T) {
	m := newSelectionModel(t, "the quick brown fox\njumps over the dog\n")
	row, height := m.live.chatGeom()
	if height <= 0 {
		t.Fatal("chat geometry not recorded by View")
	}

	// "quick" occupies columns 4..8 on the first visible line.
	m2, _ := callUpdate(m, tea.MouseClickMsg{X: 4, Y: row, Button: tea.MouseLeft})
	m3, _ := callUpdate(m2, tea.MouseMotionMsg{X: 9, Y: row, Button: tea.MouseLeft})
	if got, want := m3.chat.SelectedText(), "quick"; got != want {
		t.Fatalf("SelectedText = %q, want %q", got, want)
	}
	m4, cmd := callUpdate(m3, tea.MouseReleaseMsg{X: 9, Y: row, Button: tea.MouseLeft})
	if cmd == nil {
		t.Error("release after a drag should schedule the clipboard command")
	}
	if m4.chat.HasSelection() {
		t.Error("selection should be cleared after release")
	}
}

func TestMouseSelection_BareClickCopiesNothing(t *testing.T) {
	m := newSelectionModel(t, "the quick brown fox\n")
	row, _ := m.live.chatGeom()

	m2, _ := callUpdate(m, tea.MouseClickMsg{X: 4, Y: row, Button: tea.MouseLeft})
	m3, cmd := callUpdate(m2, tea.MouseReleaseMsg{X: 4, Y: row, Button: tea.MouseLeft})
	if cmd != nil {
		t.Error("a click without drag must not schedule a clipboard command")
	}
	if m3.chat.HasSelection() {
		t.Error("selection should be cleared after a bare click")
	}
}

func TestMouseSelection_OutsideTranscriptNotHandled(t *testing.T) {
	m := newSelectionModel(t, "the quick brown fox\n")
	row, height := m.live.chatGeom()

	if _, _, handled := m.updateMouseSelection(tea.MouseClickMsg{X: 0, Y: row - 1, Button: tea.MouseLeft}); handled {
		t.Error("click above the transcript should not start a selection")
	}
	if _, _, handled := m.updateMouseSelection(tea.MouseClickMsg{X: 0, Y: row + height, Button: tea.MouseLeft}); handled {
		t.Error("click below the transcript should not start a selection")
	}
}

func TestMouseSelection_SuppressedByOverlay(t *testing.T) {
	m := newSelectionModel(t, "the quick brown fox\n")
	row, _ := m.live.chatGeom()
	m.modalContent = "modal owns the display"

	if _, _, handled := m.updateMouseSelection(tea.MouseClickMsg{X: 0, Y: row, Button: tea.MouseLeft}); handled {
		t.Error("an active overlay must suppress transcript selection")
	}
}
