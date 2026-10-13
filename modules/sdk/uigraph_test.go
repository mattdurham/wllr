package sdk

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"strings"
	"testing"
)

func TestApplyOp_SetRootAndClone(t *testing.T) {
	src := &UINode{ID: "r", Type: UINodeVStack, Children: []UINode{
		{ID: "a", Type: UINodeText, Text: "hi", Props: &UIProps{Padding: []int{1, 2}}},
	}}
	got, err := ApplyOp(nil, UIPatchOp{Op: UIOpSetRoot, Node: src})
	if err != nil {
		t.Fatalf("set_root: %v", err)
	}
	// set_root must deep-copy: mutating the result must not touch the source.
	got.Children[0].Text = "changed"
	got.Children[0].Props.Padding[0] = 9
	if src.Children[0].Text != "hi" {
		t.Errorf("set_root aliased node text: %q", src.Children[0].Text)
	}
	if src.Children[0].Props.Padding[0] != 1 {
		t.Errorf("set_root aliased props slice: %v", src.Children[0].Props.Padding)
	}
}

func TestApplyOp_InsertIndexAndErrors(t *testing.T) {
	root := &UINode{ID: "root", Type: UINodeVStack, Children: []UINode{
		{ID: "b", Type: UINodeText, Text: "B"},
	}}
	zero := 0
	if _, err := ApplyOp(root, UIPatchOp{
		Op: UIOpInsert, Parent: "root", Index: &zero,
		Node: &UINode{ID: "a", Type: UINodeText, Text: "A"},
	}); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if root.Children[0].ID != "a" || root.Children[1].ID != "b" {
		t.Fatalf("index-0 insert order wrong: %+v", root.Children)
	}

	if _, err := ApplyOp(root, UIPatchOp{Op: UIOpInsert, Parent: "missing", Node: &UINode{ID: "x"}}); err == nil {
		t.Error("insert under a missing parent must error")
	}
	if _, err := ApplyOp(nil, UIPatchOp{Op: UIOpInsert, Node: &UINode{ID: "x"}}); err == nil {
		t.Error("insert with no root must error")
	}
	out := 5
	if _, err := ApplyOp(root, UIPatchOp{Op: UIOpInsert, Parent: "root", Index: &out, Node: &UINode{ID: "y"}}); err == nil {
		t.Error("out-of-range insert index must error")
	}
	if _, err := ApplyOp(root, UIPatchOp{Op: UIOpInsert, Parent: "root"}); err == nil {
		t.Error("insert without a node must error")
	}
}

func TestApplyOp_TextOpsRejectNonText(t *testing.T) {
	root := &UINode{ID: "box", Type: UINodeVStack}
	for _, op := range []UIPatchOp{
		{Op: UIOpAppendText, ID: "box", Text: "x"},
		{Op: UIOpReplaceText, ID: "box", Text: "x"},
	} {
		if _, err := ApplyOp(root, op); err == nil {
			t.Errorf("%s on a non-text node must error", op.Op)
		}
	}
	if _, err := ApplyOp(root, UIPatchOp{Op: UIOpAppendText, ID: "missing"}); err == nil {
		t.Error("append_text to a missing node must error")
	}
}

func TestApplyOp_RemoveRootVsChild(t *testing.T) {
	root := &UINode{ID: "root", Type: UINodeVStack, Children: []UINode{
		{ID: "c", Type: UINodeText, Text: "C"},
	}}
	got, err := ApplyOp(root, UIPatchOp{Op: UIOpRemove, ID: "c"})
	if err != nil {
		t.Fatalf("remove child: %v", err)
	}
	if len(got.Children) != 0 {
		t.Fatalf("child not removed: %+v", got.Children)
	}
	// Removing the root itself yields nil (the area becomes empty), not an error.
	got, err = ApplyOp(root, UIPatchOp{Op: UIOpRemove, ID: "root"})
	if err != nil || got != nil {
		t.Fatalf("remove root: got %v err %v, want nil,nil", got, err)
	}
	if _, err := ApplyOp(root, UIPatchOp{Op: UIOpRemove, ID: "missing"}); err == nil {
		t.Error("removing a missing node must error")
	}
}

func TestApplyOp_UnknownOp(t *testing.T) {
	if _, err := ApplyOp(nil, UIPatchOp{Op: UIPatchOpType("bogus")}); err == nil {
		t.Fatal("unknown op must error")
	}
}

func TestCollapseBlankLineRuns(t *testing.T) {
	if got := CollapseBlankLineRuns("a\n\nb"); got != "a\n\nb" {
		t.Errorf("single blank line must survive: %q", got)
	}
	if got := CollapseBlankLineRuns("a\n\n\n\nb"); got != "a\n\nb" {
		t.Errorf("runs of 3+ must collapse to 2: %q", got)
	}
}

func TestPropSides(t *testing.T) {
	cases := []struct {
		in         []int
		t, r, b, l int
		ok         bool
	}{
		{[]int{1}, 1, 1, 1, 1, true},
		{[]int{1, 2}, 1, 2, 1, 2, true},
		{[]int{1, 2, 3, 4}, 1, 2, 3, 4, true},
		{nil, 0, 0, 0, 0, false},
		{[]int{1, 2, 3}, 0, 0, 0, 0, false},
	}
	for _, tc := range cases {
		gotT, gotR, gotB, gotL, ok := PropSides(tc.in)
		if ok != tc.ok || gotT != tc.t || gotR != tc.r || gotB != tc.b || gotL != tc.l {
			t.Errorf("PropSides(%v) = %d,%d,%d,%d,%v want %d,%d,%d,%d,%v",
				tc.in, gotT, gotR, gotB, gotL, ok, tc.t, tc.r, tc.b, tc.l, tc.ok)
		}
	}
}

func TestParseDimensionAndConstraint(t *testing.T) {
	if w, ok := ParseDimension("fill", 80); !ok || w != 80 {
		t.Errorf("fill: got %d,%v want 80,true", w, ok)
	}
	if _, ok := ParseDimension("fill", 0); ok {
		t.Error("fill with no available width must report ok=false")
	}
	if _, ok := ParseDimension("auto", 80); ok {
		t.Error("auto must report ok=false")
	}
	if w, ok := ParseDimension("20", 80); !ok || w != 20 {
		t.Errorf("numeric: got %d,%v want 20,true", w, ok)
	}
	if _, ok := ParseDimension("abc", 80); ok {
		t.Error("non-numeric must report ok=false")
	}
	if v, ok := ResolveConstraint("25%", 100); !ok || v != 25 {
		t.Errorf("25%% of 100: got %d,%v", v, ok)
	}
	if _, ok := ResolveConstraint("", 100); ok {
		t.Error("empty constraint must be unconstrained")
	}
	// An oversized digit string must be rejected, not wrap to a negative value
	// that the absolute branch would return as a valid constraint.
	if _, ok := ParseDecimal("9999999999999999999999999999"); ok {
		t.Error("overflowing decimal must report ok=false")
	}
	if _, ok := ResolveConstraint("9999999999999999999999999999", 100); ok {
		t.Error("overflowing absolute constraint must be unconstrained")
	}
}

func TestExpandSideCSS(t *testing.T) {
	if got := ExpandSideCSS([]int{1, 2}, "ch"); got != "1ch 2ch 1ch 2ch" {
		t.Errorf("got %q", got)
	}
	if got := ExpandSideCSS(nil, "ch"); got != "" {
		t.Errorf("no sides must yield empty: %q", got)
	}
	if !strings.Contains(ExpandSideCSS([]int{3}, "lh"), "3lh 3lh 3lh 3lh") {
		t.Error("single side must expand to all four")
	}
}
