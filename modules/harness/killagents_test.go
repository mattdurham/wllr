package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/mattdurham/wllr/modules/sdk"
)

// TestEsc_FocusedAgentWindow_DispatchesFocusCallback verifies that esc in a
// focused sub-agent window routes the unfocus through the extension dispatch
// (agents:focus with the root ID) so the extension rebuilds the root
// transcript. Clearing the field locally is not enough — the transcript is
// extension-owned, and without the rebuild the sub-agent scene stays on
// screen, making esc appear to do nothing (the reported bug).
func TestEsc_FocusedAgentWindow_DispatchesFocusCallback(t *testing.T) {
	m := newTestModel()
	m.width, m.height = 80, 24
	m.streaming = true
	m.focusedAgent = "main/worker"
	m.live.setStatus("agent", "main/worker")

	next, cmd := m.Update(keyMsg(tea.KeyEscape, 0))
	m = next.(Model)

	if cmd == nil {
		t.Fatal("esc on a focused sub-agent must dispatch agents:focus, got no cmd")
	}
	msg := cmd()
	d, ok := msg.(dispatchOnCommandMsg)
	if !ok {
		t.Fatalf("got %T, want dispatchOnCommandMsg", msg)
	}
	if d.Name != AgentTreeCallback || len(d.Args) != 1 || d.Args[0] != "main" {
		t.Fatalf("callback = %+v, want %s with [main]", d, AgentTreeCallback)
	}
	if v := m.live.getStatus("stream"); v == "cancelling…" {
		t.Error("esc in a focused agent window must not cancel the main turn")
	}
}

// TestAgentTree_KillKeyOnSubagent verifies that x on a sub-agent row closes
// the tree and dispatches the kill callback with that agent's ID.
func TestAgentTree_KillKeyOnSubagent(t *testing.T) {
	m := newTestModel()
	m.width, m.height = 80, 24
	m, _ = callUpdate(m, ShowAgentTreeMsg{
		Callback: AgentTreeCallback,
		Nodes: []sdk.AgentTreeNode{
			{ID: "main", Label: "main"},
			{ID: "main/worker", ParentID: "main", Label: "worker"},
		},
	})
	if !m.agentTree.IsActive() {
		t.Fatal("tree not active")
	}
	// Move the cursor down to main/worker (row 0 is main).
	m, _ = callUpdate(m, tea.KeyPressMsg{Code: tea.KeyDown})

	next, cmd := m.Update(tea.KeyPressMsg{Code: 'x'})
	m = next.(Model)
	if m.agentTree.IsActive() {
		t.Error("kill should close the tree overlay")
	}
	if cmd == nil {
		t.Fatal("x on a sub-agent should dispatch the kill callback")
	}
	msg := cmd()
	d, ok := msg.(dispatchOnCommandMsg)
	if !ok {
		t.Fatalf("got %T, want dispatchOnCommandMsg", msg)
	}
	if d.Name != AgentTreeKillCallback || len(d.Args) != 1 || d.Args[0] != "main/worker" {
		t.Fatalf("kill callback = %+v, want %s with [main/worker]", d, AgentTreeKillCallback)
	}
}

// TestAgentTree_KillKeyOnRootIgnored verifies the root agent cannot be killed
// from the tree: the key is swallowed and no kill callback is dispatched.
func TestAgentTree_KillKeyOnRootIgnored(t *testing.T) {
	m := newTestModel()
	m.width, m.height = 80, 24
	m, _ = callUpdate(m, ShowAgentTreeMsg{
		Callback: AgentTreeCallback,
		Nodes: []sdk.AgentTreeNode{
			{ID: "main", Label: "main"},
		},
	})
	// Cursor starts on row 0 = main.
	next, cmd := m.Update(tea.KeyPressMsg{Code: 'x'})
	m = next.(Model)
	if !m.agentTree.IsActive() {
		t.Error("a rejected kill must not close the tree")
	}
	if cmd != nil {
		t.Fatalf("x on the root must not dispatch a kill callback, got %T", cmd())
	}
}

// TestAgentTreeEscStillClosesKept verifies esc still closes the overlay after
// the kill key was added to the same switch.
func TestAgentTreeEscStillClosesKept(t *testing.T) {
	m := newTestModel()
	m.width, m.height = 80, 24
	m, _ = callUpdate(m, ShowAgentTreeMsg{
		Callback: AgentTreeCallback,
		Nodes:    []sdk.AgentTreeNode{{ID: "main", Label: "main"}},
	})
	next, _ := m.Update(keyMsg(tea.KeyEscape, 0))
	m = next.(Model)
	if m.agentTree.IsActive() {
		t.Error("esc must still close the tree")
	}
}
