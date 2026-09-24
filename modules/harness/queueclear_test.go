package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// Tests for issue #48: the queued-messages pane is clearable from the UI —
// via the ctrl+x keybinding and via a left-click on the [ clear ] button in
// the pane header — including while a turn is running. Clearing discards the
// messages (no silent replay into a later turn) and reports the count.

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/mattdurham/wllr/modules/sdk"
)

func ctrlX() tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: 'x', Mod: tea.ModCtrl}
}

// inboxCount reports how many messages are queued for the model's main agent.
func inboxCount(t *testing.T, m Model) int {
	t.Helper()
	n, err := m.agentPool.SnapshotInbox(m.mainAgentID)
	if err != nil {
		t.Fatalf("SnapshotInbox: %v", err)
	}
	return len(n)
}

// queueTwoMessages appends two messages to the test model's main-agent inbox.
func queueTwoMessages(t *testing.T, m Model) {
	t.Helper()
	main := m.agentPool.Get(m.mainAgentID)
	if main == nil {
		t.Fatal("no main agent in test pool")
	}
	main.AppendInbox(sdk.Message{Role: sdk.RoleUser, Content: "queued one"})
	main.AppendInbox(sdk.Message{Role: sdk.RoleUser, Content: "queued two"})
}

func TestClearQueue_Keybinding(t *testing.T) {
	m := newTestModel()
	queueTwoMessages(t, m)

	if n := inboxCount(t, m); n != 2 {
		t.Fatalf("precondition: inbox has %d messages, want 2", n)
	}

	m2, _, handled := m.updateKeyPress(ctrlX())
	if !handled {
		t.Fatal("ctrl+x was not handled")
	}
	if got := inboxCount(t, m2); got != 0 {
		t.Errorf("inbox not cleared by ctrl+x: %d message(s) remain", got)
	}
}

// TestClearQueue_KeybindingWhileStreaming pins that clearing works during an
// active main turn — the common window per issue #48.
func TestClearQueue_KeybindingWhileStreaming(t *testing.T) {
	m := newTestModel()
	queueTwoMessages(t, m)
	m.live.setStreaming(true, m.live.streamStart, false)

	m2, _, handled := m.updateKeyPress(ctrlX())
	if !handled {
		t.Fatal("ctrl+x was not handled while streaming")
	}
	if got := inboxCount(t, m2); got != 0 {
		t.Errorf("inbox not cleared while streaming: %d message(s) remain", got)
	}
}

func TestClearQueue_KeybindingEmptyQueue(t *testing.T) {
	m := newTestModel()
	m2, _, handled := m.updateKeyPress(ctrlX())
	if !handled {
		t.Fatal("ctrl+x on an empty queue should still be handled")
	}
	if got := inboxCount(t, m2); got != 0 {
		t.Errorf("unexpected messages in inbox: %d", got)
	}
}

// TestClearQueue_MouseClick renders the model with a queued pane, then clicks
// the [ clear ] button at the geometry recorded by View.
func TestClearQueue_MouseClick(t *testing.T) {
	m := newTestModel()
	m.height = 40
	m.width = 100
	queueTwoMessages(t, m)

	// Run an update so syncLayout sizes the panes, then render to record the
	// button geometry.
	m, _ = callUpdateModel(m, tea.MouseClickMsg{X: 0, Y: 0, Button: tea.MouseLeft})
	view := m.View()
	headerRow, start, end := m.live.queueGeom()
	if end <= start {
		t.Fatalf("clear button geometry not recorded by View; first line=%q", firstLine(view.Content))
	}

	// Click in the middle of the button.
	m2, cmd := callUpdateModel(m, tea.MouseClickMsg{X: (start + end) / 2, Y: headerRow, Button: tea.MouseLeft})
	if got := inboxCount(t, m2); got != 0 {
		t.Errorf("click on [ clear ] did not clear inbox: %d message(s) remain", got)
	}
	if cmd != nil {
		t.Errorf("clear action should not schedule a command, got %v", cmd)
	}

	// Next render must drop the pane and reset the geometry.
	m3, _ := callUpdateModel(m2, tea.MouseClickMsg{X: 0, Y: 0, Button: tea.MouseLeft})
	_ = m3.View()
	if _, start3, end3 := m3.live.queueGeom(); end3 > start3 {
		t.Error("geometry not cleared after queue emptied")
	}
}

// TestClearQueue_MouseClickOutsideButton pins that clicks elsewhere (even on
// the same header row) do not clear.
func TestClearQueue_MouseClickOutsideButton(t *testing.T) {
	m := newTestModel()
	m.height = 40
	m.width = 100
	queueTwoMessages(t, m)

	m, _ = callUpdateModel(m, tea.MouseClickMsg{X: 0, Y: 0, Button: tea.MouseLeft})
	row, start, _ := m.live.queueGeom()
	if start <= 0 {
		t.Skip("button not rendered at width 100")
	}

	for _, x := range []int{0, start - 1} {
		m2, _ := callUpdateModel(m, tea.MouseClickMsg{X: x, Y: row, Button: tea.MouseLeft})
		if got := inboxCount(t, m2); got != 2 {
			t.Errorf("click at x=%d cleared the inbox; %d message(s) remain, want 2", x, got)
		}
	}
}

// TestClearQueue_RendererButton pins the renderer contract: the header ends
// with "[ clear ] ╮" when it fits, the returned columns cover exactly the
// button, and too-narrow terminals fall back to the button-less header.
func TestClearQueue_RendererButton(t *testing.T) {
	m := newTestModel()
	m.width = 100

	view, start, end := m.renderQueuedMessagesFrom([]sdk.Message{{Role: sdk.RoleUser, Content: "hello"}})
	if !strings.Contains(view, "[ clear ]") {
		t.Errorf("header missing [ clear ] button: %q", firstLine(view))
	}
	if end <= start {
		t.Fatalf("button columns not set: [%d,%d]", start, end)
	}
	btnWidth := len("[ clear ]")
	if end-start+1 != btnWidth {
		t.Errorf("button spans %d columns, want %d", end-start+1, btnWidth)
	}

	// Too narrow: label + fill + button + corner cannot fit.
	m2 := newTestModel()
	m2.width = 18
	view2, start2, end2 := m2.renderQueuedMessagesFrom([]sdk.Message{{Role: sdk.RoleUser, Content: "hello"}})
	if strings.Contains(view2, "[ clear ]") {
		t.Errorf("button rendered on a narrow terminal: %q", firstLine(view2))
	}
	if end2 > start2 {
		t.Errorf("click target must stay unset on narrow terminals: [%d,%d]", start2, end2)
	}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// callUpdateModel is callUpdate from model_test.go; redeclared name-safe via
// a thin wrapper to keep this file independent.
func callUpdateModel(m Model, msg tea.Msg) (Model, tea.Cmd) {
	newModel, cmd := m.Update(msg)
	return newModel.(Model), cmd
}
