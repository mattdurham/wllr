package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/mattdurham/wllr/modules/sdk"
)

// TestEsc_AgentsViewOpen_ClosesWithoutCancelling verifies that pressing esc
// while the agents tree overlay is open dismisses the overlay and does NOT
// cancel an in-flight turn behind it.
func TestEsc_AgentsViewOpen_ClosesWithoutCancelling(t *testing.T) {
	m := newTestModel()
	m.width, m.height = 80, 24
	m.streaming = true

	m, _ = callUpdate(m, ShowAgentTreeMsg{
		Callback: AgentTreeCallback,
		Nodes: []sdk.AgentTreeNode{
			{ID: "main", Label: "main"},
		},
	})
	if !m.agentTree.IsActive() {
		t.Fatal("tree overlay did not open")
	}

	next, _ := m.Update(keyMsg(tea.KeyEscape, 0))
	m = next.(Model)

	if m.agentTree.IsActive() {
		t.Error("esc should close the agents view")
	}
	if v := m.live.getStatus("stream"); v == "cancelling…" {
		t.Error("esc in the agents view must not cancel the running turn")
	}
}

// TestEsc_FocusedAgentWindow_DispatchesFocusCallback (killagents_test.go)
// supersedes the old synchronous-unfocus test: esc routes the unfocus through
// the extension dispatch (agents:focus with the root ID) so the extension
// rebuilds the root transcript. The field clear itself is owned by the
// FocusAgentMsg handler, covered by TestFocusPublishesAgentStatus.

// TestEsc_RootFocus_StillCancelsTurn verifies the pre-existing behavior is
// preserved once the window is closed: esc with no sub-agent focused cancels
// an active main turn.
func TestEsc_RootFocus_StillCancelsTurn(t *testing.T) {
	m := newTestModel()
	m.streaming = true

	next, _ := m.Update(keyMsg(tea.KeyEscape, 0))
	m = next.(Model)

	if v := m.live.getStatus("stream"); v != "cancelling…" {
		t.Errorf("expected stream status 'cancelling…', got %q", v)
	}
}
