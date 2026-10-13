package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// Tests for input-message recall: up on an empty input box loads the most
// recently submitted message, and up/down walk the history once recall starts.

import (
	"fmt"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func pressUp() tea.KeyPressMsg   { return tea.KeyPressMsg{Code: tea.KeyUp} }
func pressDown() tea.KeyPressMsg { return tea.KeyPressMsg{Code: tea.KeyDown} }

// submit drives a value through Enter, which records it in the recall history.
func submit(i InputArea, text string) InputArea {
	i.SetValue(text)
	i, _ = i.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	return i
}

func TestInputArea_HistoryRecordsSubmits(t *testing.T) {
	i := NewInputArea(80)
	i = submit(i, "first")
	i = submit(i, "second")
	if len(i.history) != 2 {
		t.Fatalf("history len = %d, want 2", len(i.history))
	}
}

func TestInputArea_UpOnEmptyRecallsMostRecent(t *testing.T) {
	i := NewInputArea(80)
	i = submit(i, "first")
	i = submit(i, "second")

	i, _ = i.Update(pressUp())
	if got := i.Value(); got != "second" {
		t.Fatalf("up on empty = %q, want %q", got, "second")
	}
	i, _ = i.Update(pressUp())
	if got := i.Value(); got != "first" {
		t.Fatalf("second up = %q, want %q", got, "first")
	}
	// Already at the oldest entry: further up is a no-op.
	i, _ = i.Update(pressUp())
	if got := i.Value(); got != "first" {
		t.Fatalf("up past oldest = %q, want %q", got, "first")
	}
	// Down walks newer, then restores the (empty) draft.
	i, _ = i.Update(pressDown())
	if got := i.Value(); got != "second" {
		t.Fatalf("down = %q, want %q", got, "second")
	}
	i, _ = i.Update(pressDown())
	if got := i.Value(); got != "" {
		t.Fatalf("down past newest should restore draft, got %q", got)
	}
}

func TestInputArea_UpWithTextMovesCursorNotHistory(t *testing.T) {
	i := NewInputArea(80)
	i = submit(i, "sent")
	i.SetValue("draft")
	if got := i.histIdx; got != -1 {
		t.Fatalf("precondition: histIdx = %d, want -1", got)
	}
	i, _ = i.Update(pressUp())
	if got := i.Value(); got != "draft" {
		t.Fatalf("up with text should not recall history, got %q", got)
	}
}

func TestInputArea_HistoryDedupesConsecutive(t *testing.T) {
	i := NewInputArea(80)
	i = submit(i, "same")
	i = submit(i, "same")
	if len(i.history) != 1 {
		t.Fatalf("history len = %d, want 1 (deduped)", len(i.history))
	}
}

func TestInputArea_HistoryCapped(t *testing.T) {
	i := NewInputArea(80)
	for n := 0; n < maxInputHistory+5; n++ {
		i = submit(i, fmt.Sprintf("m%d", n))
	}
	if len(i.history) != maxInputHistory {
		t.Fatalf("history len = %d, want %d", len(i.history), maxInputHistory)
	}
	want := fmt.Sprintf("m%d", maxInputHistory+4)
	if got := i.history[len(i.history)-1]; got != want {
		t.Fatalf("newest entry = %q, want %q", got, want)
	}
}

func TestInputArea_EscClearsRecallState(t *testing.T) {
	i := NewInputArea(80)
	i = submit(i, "sent")
	i, _ = i.Update(pressUp())
	if i.histIdx == -1 {
		t.Fatal("precondition: expected recall in progress")
	}
	i, _ = i.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if i.histIdx != -1 {
		t.Fatalf("esc should end recall, histIdx = %d", i.histIdx)
	}
	if got := i.Value(); got != "" {
		t.Fatalf("esc should clear input, got %q", got)
	}
}

// TestModel_UpOnEmptyInputRecallsLastMessage drives the full Update path to
// confirm the input box (not just InputArea in isolation) recalls history.
func TestModel_UpOnEmptyInputRecallsLastMessage(t *testing.T) {
	m := newTestModel()
	m.input.SetValue("hello world")
	m, _ = callUpdate(m, keyMsg(tea.KeyEnter, 0))
	if got := m.input.Value(); got != "" {
		t.Fatalf("input should be cleared after submit, got %q", got)
	}
	m, _ = callUpdate(m, keyMsg(tea.KeyUp, 0))
	if got := m.input.Value(); got != "hello world" {
		t.Fatalf("up should recall last message, got %q", got)
	}
}

// TestInputArea_UpBetweenEscResetsDoubleEsc guards the lastWasEsc invariant:
// Up/Down are "any other key" and must reset the double-Esc abort, otherwise
// an Esc/Up/Esc sequence would fire an abort that the user never intended.
func TestInputArea_UpBetweenEscResetsDoubleEsc(t *testing.T) {
	i := NewInputArea(80)
	i, _ = i.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	i, _ = i.Update(pressUp())
	_, cmd := i.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if cmd != nil {
		if _, isAbort := cmd().(abortStreamMsg); isAbort {
			t.Fatal("up between two Escs should reset the double-Esc abort")
		}
	}
}
