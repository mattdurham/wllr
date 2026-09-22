package harness

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func treeNodes() []AgentTreeNode {
	return []AgentTreeNode{
		{ID: "main", Label: "main", Detail: "root"},
		{ID: "main/planner", ParentID: "main", Label: "planner"},
		{ID: "main/planner/helper", ParentID: "main/planner", Label: "helper"},
		{ID: "main/zebra", ParentID: "main", Label: "zebra"},
	}
}

func newTree() *AgentTreeView {
	var t AgentTreeView
	t.Open(treeNodes(), AgentTreeCallback)
	t.SetSize(80, 12)
	return &t
}

// The tree must order siblings by ID so it does not reshuffle between openings,
// which a map-backed node list would otherwise cause.
func TestAgentTree_OrderIsStable(t *testing.T) {
	tree := newTree()
	first := tree.rows()
	if len(first) != 4 {
		t.Fatalf("rows = %d, want 4", len(first))
	}
	want := []string{"main", "main/planner", "main/planner/helper", "main/zebra"}
	for i, w := range want {
		if first[i].node.ID != w {
			t.Errorf("row %d = %q, want %q", i, first[i].node.ID, w)
		}
	}
	// Depth drives indentation: the root is 0, its children 1, the grandchild 2.
	for i, wantDepth := range []int{0, 1, 2, 1} {
		if first[i].depth != wantDepth {
			t.Errorf("row %d depth = %d, want %d", i, first[i].depth, wantDepth)
		}
	}
}

// Collapsing a parent hides its children; the parent itself stays visible so it
// can be unfolded again.
func TestAgentTree_CollapseHidesSubtreeKeepsParent(t *testing.T) {
	tree := newTree()
	tree.cursor = 1 // main/planner (the parent)
	tree.collapse()
	if got := len(tree.rows()); got != 3 {
		t.Fatalf("rows after collapsing planner = %d, want 3 (helper hidden)", got)
	}
	node, ok := tree.Highlighted()
	if !ok || node.ID != "main/planner" {
		t.Fatalf("cursor = %+v ok=%v, want it to stay on main/planner", node, ok)
	}
}

func TestAgentTree_ExpandRevealsSubtree(t *testing.T) {
	tree := newTree()
	tree.cursor = 1 // main/planner
	if r := tree.HandleKey(tea.KeyPressMsg{Code: tea.KeyLeft}); !r.Folded || !r.Handled {
		t.Fatal("left on a parent should fold")
	}
	if got := len(tree.rows()); got != 3 {
		t.Fatalf("rows = %d, want 3 after fold", got)
	}
	if r := tree.HandleKey(tea.KeyPressMsg{Code: tea.KeyRight}); !r.Folded || !r.Handled {
		t.Fatal("right on a parent should unfold")
	}
	if got := len(tree.rows()); got != 4 {
		t.Fatalf("rows = %d, want 4 after unfold", got)
	}
}

// A cursor inside a collapsed subtree must move to a row that still exists;
// otherwise the highlight would point past the visible rows.
func TestAgentTree_CollapseClampsCursor(t *testing.T) {
	tree := newTree()
	tree.cursor = 2 // main/planner/helper, about to be hidden
	tree.expanded["main/planner"] = false
	tree.clampCursor()
	node, ok := tree.Highlighted()
	if !ok {
		t.Fatal("cursor should still resolve to a visible row")
	}
	if node.ID == "main/planner/helper" {
		t.Fatalf("cursor left on a hidden row: %+v", node)
	}
}

// Space toggles; a leaf has nothing to fold, so it must not be reported as
// folded (which would suppress a re-render).
func TestAgentTree_LeafDoesNotFold(t *testing.T) {
	tree := newTree()
	tree.cursor = 3 // main/zebra, a leaf
	if r := tree.HandleKey(tea.KeyPressMsg{Code: tea.KeySpace}); r.Folded {
		t.Error("leaf should not report a fold")
	}
}

// Enter focuses the highlighted agent; the harness relays this to the extension.
func TestAgentTree_EnterFocusesHighlighted(t *testing.T) {
	tree := newTree()
	tree.cursor = 1
	r := tree.HandleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !r.Handled {
		t.Fatal("enter should be handled")
	}
	if r.Focused != "main/planner" {
		t.Fatalf("focused %q, want main/planner", r.Focused)
	}
}

// esc deliberately falls through: it keeps its global meaning of cancelling the
// current ask, so the tree must not swallow it.
func TestAgentTree_EscIsNotHandled(t *testing.T) {
	tree := newTree()
	r := tree.HandleKey(tea.KeyPressMsg{Code: tea.KeyEsc})
	if r.Handled || r.Closed || r.Focused != "" {
		t.Fatalf("esc must fall through to the caller, got %+v", r)
	}
}

// q closes the overlay, per the header hint.
func TestAgentTree_QCloses(t *testing.T) {
	tree := newTree()
	r := tree.HandleKey(tea.KeyPressMsg{Code: 'q', Text: "q"})
	if !r.Closed || !r.Handled {
		t.Fatalf("q should close the tree, got %+v", r)
	}
}

// A node whose parent is absent must still render, as a root, rather than being
// dropped from the view.
func TestAgentTree_OrphanRendersAsRoot(t *testing.T) {
	var tree AgentTreeView
	tree.Open([]AgentTreeNode{
		{ID: "main"},
		{ID: "gone/child", ParentID: "gone"},
	}, AgentTreeCallback)
	tree.SetSize(80, 10)
	if got := len(tree.rows()); got != 2 {
		t.Fatalf("rows = %d, want 2 (orphan shown as a root)", got)
	}
}

func TestAgentTree_ViewRendersFoldMarkersAndIndent(t *testing.T) {
	tree := newTree()
	view := tree.View()
	for _, want := range []string{"main", "planner", "helper", "▾"} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q:\n%s", want, view)
		}
	}
}
