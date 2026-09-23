package harness

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/mattdurham/wllr/modules/sdk"
)

// Selecting a child in the tree must dispatch the child's ID, not the root's.
// This drives the real Update flow and keeps each returned model; an earlier
// probe discarded the updated model and so read a stale cursor.
func TestAgentTreeSelectionDispatchesChildID(t *testing.T) {
	m := newTestModel()
	m.width, m.height = 80, 24
	m, _ = callUpdate(m, ShowAgentTreeMsg{
		Callback: AgentTreeCallback,
		Nodes: []sdk.AgentTreeNode{
			{ID: "main", Label: "main"},
			{ID: "main/aardvark", ParentID: "main", Label: "aardvark"},
			{ID: "main/scout", ParentID: "main", Label: "scout"},
		},
	})

	// Siblings render alphabetically by ID, so the first child after the root is
	// "aardvark" regardless of the order the nodes were supplied in.
	m, _ = callUpdate(m, tea.KeyPressMsg{Code: tea.KeyDown})
	if n, _ := m.agentTree.Highlighted(); n.ID != "main/aardvark" {
		t.Fatalf("after one down, highlighted = %q, want main/aardvark", n.ID)
	}
	m, _ = callUpdate(m, tea.KeyPressMsg{Code: tea.KeyDown})
	if n, _ := m.agentTree.Highlighted(); n.ID != "main/scout" {
		t.Fatalf("after two downs, highlighted = %q, want main/scout", n.ID)
	}
	m, cmd := callUpdate(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter produced no command")
	}
	d, ok := cmd().(dispatchOnCommandMsg)
	if !ok {
		t.Fatalf("got %T, want dispatchOnCommandMsg", d)
	}
	if len(d.Args) != 1 || d.Args[0] != "main/scout" {
		t.Fatalf("dispatched %v, want [main/scout]", d.Args)
	}
	if d.Name != AgentTreeCallback {
		t.Fatalf("callback = %q, want %q", d.Name, AgentTreeCallback)
	}
}

// The same, with three downs: the cursor must track to the last node.
func TestAgentTreeCursorTracksRepeatedDown(t *testing.T) {
	m := newTestModel()
	m.width, m.height = 80, 24
	m, _ = callUpdate(m, ShowAgentTreeMsg{
		Callback: AgentTreeCallback,
		Nodes: []sdk.AgentTreeNode{
			{ID: "main", Label: "main"},
			{ID: "main/a", ParentID: "main", Label: "a"},
			{ID: "main/b", ParentID: "main", Label: "b"},
			{ID: "main/c", ParentID: "main", Label: "c"},
		},
	})
	for _, want := range []string{"main/a", "main/b", "main/c"} {
		m, _ = callUpdate(m, tea.KeyPressMsg{Code: tea.KeyDown})
		if n, _ := m.agentTree.Highlighted(); n.ID != want {
			t.Fatalf("highlighted = %q, want %q", n.ID, want)
		}
	}
	// Down at the end must clamp, not wrap or overflow.
	m, _ = callUpdate(m, tea.KeyPressMsg{Code: tea.KeyDown})
	if n, _ := m.agentTree.Highlighted(); n.ID != "main/c" {
		t.Fatalf("after clamp, highlighted = %q, want main/c", n.ID)
	}
}
