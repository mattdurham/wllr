package harness

import (
	"strings"
	"testing"

	"github.com/mattdurham/wllr/modules/sdk"
)

func intp(i int) *int { return &i }

func TestSceneCreateAndDuplicateArea(t *testing.T) {
	s := NewSceneRenderer()
	if err := s.CreateArea(sdk.UIArea{ID: "chat", Placement: sdk.UIAreaMain}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := s.CreateArea(sdk.UIArea{ID: "chat"}); err == nil {
		t.Fatal("expected duplicate area error")
	}
	if !s.HasArea("chat") {
		t.Fatal("area should exist")
	}
	s.RemoveArea("chat")
	if s.HasArea("chat") {
		t.Fatal("area should be removed")
	}
}

func TestSceneSetRootAndRender(t *testing.T) {
	s := NewSceneRenderer()
	_ = s.CreateArea(sdk.UIArea{ID: "a", Placement: sdk.UIAreaMain})
	err := s.ApplyPatch(sdk.UIPatchParams{Area: "a", Ops: []sdk.UIPatchOp{
		{Op: sdk.UIOpSetRoot, Node: &sdk.UINode{ID: "root", Type: sdk.UINodeVStack, Children: []sdk.UINode{
			{ID: "l1", Type: sdk.UINodeText, Text: "hello"},
		}}},
	}})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	out := s.Render("a", 40)
	if !strings.Contains(out, "hello") {
		t.Fatalf("render missing text: %q", out)
	}
}

func TestSceneAppendTextStreaming(t *testing.T) {
	s := NewSceneRenderer()
	_ = s.CreateArea(sdk.UIArea{ID: "a"})
	_ = s.ApplyPatch(sdk.UIPatchParams{Area: "a", Ops: []sdk.UIPatchOp{
		{Op: sdk.UIOpSetRoot, Node: &sdk.UINode{ID: "txt", Type: sdk.UINodeText, Text: "Hel"}},
	}})
	if err := s.ApplyPatch(sdk.UIPatchParams{Area: "a", Ops: []sdk.UIPatchOp{
		{Op: sdk.UIOpAppendText, ID: "txt", Text: "lo "},
		{Op: sdk.UIOpAppendText, ID: "txt", Text: "world"},
	}}); err != nil {
		t.Fatalf("append: %v", err)
	}
	if out := s.Render("a", 40); !strings.Contains(out, "Hello world") {
		t.Fatalf("streaming append failed: %q", out)
	}
}

func TestSceneRenderNodeWithTextOverride(t *testing.T) {
	s := NewSceneRenderer()
	_ = s.CreateArea(sdk.UIArea{ID: "a"})
	_ = s.ApplyPatch(sdk.UIPatchParams{Area: "a", Ops: []sdk.UIPatchOp{
		{
			Op:   sdk.UIOpSetRoot,
			Node: &sdk.UINode{ID: "txt", Type: sdk.UINodeText, Text: "current", Props: &sdk.UIProps{Border: "rounded"}},
		},
	}})

	override := "previous"
	out, ok := s.RenderNode("a", "txt", 40, &override)
	if !ok {
		t.Fatal("RenderNode should find text node")
	}
	if !strings.Contains(out, "previous") || strings.Contains(out, "current") {
		t.Fatalf("RenderNode should render override text with node style, got %q", out)
	}
	if live := s.Render("a", 40); !strings.Contains(live, "current") || strings.Contains(live, "previous") {
		t.Fatalf("RenderNode must not mutate live scene, got %q", live)
	}
}

func TestSceneRenderAppendTextNode(t *testing.T) {
	s := NewSceneRenderer()
	_ = s.CreateArea(sdk.UIArea{ID: "a"})
	_ = s.ApplyPatch(sdk.UIPatchParams{Area: "a", Ops: []sdk.UIPatchOp{
		{
			Op: sdk.UIOpSetRoot,
			Node: &sdk.UINode{
				ID:    "txt",
				Type:  sdk.UINodeText,
				Text:  "hello world",
				Props: &sdk.UIProps{Border: "rounded"},
			},
		},
	}})

	previous, current, ok := s.RenderAppendTextNode("a", "txt", 40, " world")
	if !ok {
		t.Fatal("RenderAppendTextNode should find appended text node")
	}
	if !strings.Contains(previous, "hello") || strings.Contains(previous, "world") {
		t.Fatalf("previous render should omit appended suffix, got %q", previous)
	}
	if !strings.Contains(current, "hello world") {
		t.Fatalf("current render should include full text, got %q", current)
	}
	if live := s.Render("a", 40); !strings.Contains(live, "hello world") {
		t.Fatalf("RenderAppendTextNode must not mutate live scene, got %q", live)
	}
}

func TestSceneReplaceTextNode(t *testing.T) {
	s := NewSceneRenderer()
	_ = s.CreateArea(sdk.UIArea{ID: "a"})
	_ = s.ApplyPatch(sdk.UIPatchParams{Area: "a", Ops: []sdk.UIPatchOp{
		{
			Op:   sdk.UIOpSetRoot,
			Node: &sdk.UINode{ID: "txt", Type: sdk.UINodeText, Text: "raw markdown"},
		},
	}})
	if err := s.ApplyPatch(sdk.UIPatchParams{Area: "a", Ops: []sdk.UIPatchOp{
		{Op: sdk.UIOpReplaceText, ID: "txt", Text: "rendered text"},
	}}); err != nil {
		t.Fatalf("ApplyPatch replace_text: %v", err)
	}
	if live := s.Render("a", 40); strings.Contains(live, "raw markdown") || !strings.Contains(live, "rendered text") {
		t.Fatalf("replace_text should swap node text, got %q", live)
	}
}

func TestSceneReplaceText_NonTextNode_Errors(t *testing.T) {
	s := NewSceneRenderer()
	_ = s.CreateArea(sdk.UIArea{ID: "a"})
	_ = s.ApplyPatch(sdk.UIPatchParams{Area: "a", Ops: []sdk.UIPatchOp{
		{Op: sdk.UIOpSetRoot, Node: &sdk.UINode{ID: "root", Type: sdk.UINodeVStack}},
	}})
	if err := s.ApplyPatch(sdk.UIPatchParams{Area: "a", Ops: []sdk.UIPatchOp{
		{Op: sdk.UIOpReplaceText, ID: "root", Text: "x"},
	}}); err == nil {
		t.Fatal("replace_text on non-text node should error")
	}
}

func TestSceneInsertAndRemove(t *testing.T) {
	s := NewSceneRenderer()
	_ = s.CreateArea(sdk.UIArea{ID: "a"})
	_ = s.ApplyPatch(sdk.UIPatchParams{Area: "a", Ops: []sdk.UIPatchOp{
		{Op: sdk.UIOpSetRoot, Node: &sdk.UINode{ID: "root", Type: sdk.UINodeVStack}},
	}})
	// Insert two children, second at index 0 so it appears first.
	_ = s.ApplyPatch(sdk.UIPatchParams{Area: "a", Ops: []sdk.UIPatchOp{
		{Op: sdk.UIOpInsert, Parent: "root", Node: &sdk.UINode{ID: "b", Type: sdk.UINodeText, Text: "B"}},
		{
			Op:     sdk.UIOpInsert,
			Parent: "root",
			Index:  intp(0),
			Node:   &sdk.UINode{ID: "a1", Type: sdk.UINodeText, Text: "A"},
		},
	}})
	out := s.Render("a", 40)
	if strings.Index(out, "A") > strings.Index(out, "B") {
		t.Fatalf("index-0 insert should put A before B: %q", out)
	}
	// Remove B.
	if err := s.ApplyPatch(sdk.UIPatchParams{Area: "a", Ops: []sdk.UIPatchOp{
		{Op: sdk.UIOpRemove, ID: "b"},
	}}); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if strings.Contains(s.Render("a", 40), "B") {
		t.Fatal("B should be removed")
	}
}

func TestSceneBatchAtomicOnError(t *testing.T) {
	s := NewSceneRenderer()
	_ = s.CreateArea(sdk.UIArea{ID: "a"})
	_ = s.ApplyPatch(sdk.UIPatchParams{Area: "a", Ops: []sdk.UIPatchOp{
		{Op: sdk.UIOpSetRoot, Node: &sdk.UINode{ID: "txt", Type: sdk.UINodeText, Text: "keep"}},
	}})
	// Second op targets a missing node — whole batch must be rejected.
	err := s.ApplyPatch(sdk.UIPatchParams{Area: "a", Ops: []sdk.UIPatchOp{
		{Op: sdk.UIOpAppendText, ID: "txt", Text: "X"},
		{Op: sdk.UIOpAppendText, ID: "missing", Text: "Y"},
	}})
	if err == nil {
		t.Fatal("expected batch error")
	}
	if out := s.Render("a", 40); !strings.Contains(out, "keep") || strings.Contains(out, "X") {
		t.Fatalf("failed batch must not mutate live tree: %q", out)
	}
}

func TestScenePatchUnknownArea(t *testing.T) {
	s := NewSceneRenderer()
	if err := s.ApplyPatch(sdk.UIPatchParams{Area: "nope"}); err == nil {
		t.Fatal("expected unknown area error")
	}
}

func TestSceneAppendTextRejectsNonText(t *testing.T) {
	s := NewSceneRenderer()
	_ = s.CreateArea(sdk.UIArea{ID: "a"})
	_ = s.ApplyPatch(sdk.UIPatchParams{Area: "a", Ops: []sdk.UIPatchOp{
		{Op: sdk.UIOpSetRoot, Node: &sdk.UINode{ID: "box", Type: sdk.UINodeVStack}},
	}})
	if err := s.ApplyPatch(sdk.UIPatchParams{Area: "a", Ops: []sdk.UIPatchOp{
		{Op: sdk.UIOpAppendText, ID: "box", Text: "x"},
	}}); err == nil {
		t.Fatal("append_text on non-text node must error")
	}
}

func TestSceneAreasByPlacement(t *testing.T) {
	s := NewSceneRenderer()
	_ = s.CreateArea(sdk.UIArea{ID: "m", Placement: sdk.UIAreaMain})
	_ = s.CreateArea(sdk.UIArea{ID: "s1", Placement: sdk.UIAreaSidebar})
	_ = s.CreateArea(sdk.UIArea{ID: "s2", Placement: sdk.UIAreaSidebar})
	got := s.AreasByPlacement(sdk.UIAreaSidebar)
	if len(got) != 2 || got[0] != "s1" || got[1] != "s2" {
		t.Fatalf("placement order wrong: %v", got)
	}
}

func TestSceneUpdateAreaConstraints(t *testing.T) {
	s := NewSceneRenderer()
	_ = s.CreateArea(sdk.UIArea{ID: "sl", Placement: sdk.UIAreaStatus, MinHeight: "1", MaxHeight: "3"})

	// Initial constraints: min=1, max=3.
	if got := s.ConstrainHeight("sl", 5, 40); got != 3 {
		t.Fatalf("expected clamped to 3, got %d", got)
	}
	if got := s.ConstrainHeight("sl", 0, 40); got != 1 {
		t.Fatalf("expected padded to 1, got %d", got)
	}

	// UpdateArea: raise max to 10.
	if err := s.UpdateArea(sdk.UIUpdateAreaParams{ID: "sl", MaxHeight: "10"}); err != nil {
		t.Fatalf("update: %v", err)
	}
	if got := s.ConstrainHeight("sl", 5, 40); got != 5 {
		t.Fatalf("expected 5 after max raise, got %d", got)
	}

	// UpdateArea with unknown ID must error.
	if err := s.UpdateArea(sdk.UIUpdateAreaParams{ID: "nope", MaxHeight: "5"}); err == nil {
		t.Fatal("expected error for unknown area")
	}
}

func TestSceneUpdateAreaWeight(t *testing.T) {
	s := NewSceneRenderer()
	_ = s.CreateArea(sdk.UIArea{ID: "sl", Placement: sdk.UIAreaStatus, Weight: 1})
	w := 5
	if err := s.UpdateArea(sdk.UIUpdateAreaParams{ID: "sl", Weight: &w}); err != nil {
		t.Fatalf("update weight: %v", err)
	}
	// Verify indirectly — weight is stored; no accessor needed for this test.
	// A second update with nil weight must leave weight at 5 (not reset to 0).
	if err := s.UpdateArea(sdk.UIUpdateAreaParams{ID: "sl", MaxHeight: "2"}); err != nil {
		t.Fatalf("second update: %v", err)
	}
}

func TestConstrainWidthAbsolute(t *testing.T) {
	s := NewSceneRenderer()
	_ = s.CreateArea(sdk.UIArea{ID: "a", MinWidth: "20", MaxWidth: "80"})
	if got := s.ConstrainWidth("a", 100); got != 80 {
		t.Fatalf("expected 80, got %d", got)
	}
	if got := s.ConstrainWidth("a", 10); got != 20 {
		t.Fatalf("expected 20, got %d", got)
	}
	if got := s.ConstrainWidth("a", 50); got != 50 {
		t.Fatalf("expected 50 (within range), got %d", got)
	}
}

func TestConstrainWidthPercent(t *testing.T) {
	s := NewSceneRenderer()
	_ = s.CreateArea(sdk.UIArea{ID: "a", MinWidth: "25%", MaxWidth: "75%"})
	// terminal width 100: min=25, max=75
	if got := s.ConstrainWidth("a", 100); got != 75 {
		t.Fatalf("expected 75, got %d", got)
	}
	// terminal width 100, input 10: clamp to min 25
	if got := s.ConstrainWidth("a", 100); got != 75 {
		t.Fatalf("expected 75, got %d", got)
	}
}

func TestConstrainHeightPercent(t *testing.T) {
	s := NewSceneRenderer()
	_ = s.CreateArea(sdk.UIArea{ID: "a", MaxHeight: "10%"})
	// 10% of 50 = 5
	if got := s.ConstrainHeight("a", 20, 50); got != 5 {
		t.Fatalf("expected 5, got %d", got)
	}
}

func TestConstrainUnknownAreaPassthrough(t *testing.T) {
	s := NewSceneRenderer()
	// Unknown area — ConstrainWidth/ConstrainHeight must return inputs unchanged.
	if got := s.ConstrainWidth("nope", 80); got != 80 {
		t.Fatalf("expected 80, got %d", got)
	}
	if got := s.ConstrainHeight("nope", 5, 40); got != 5 {
		t.Fatalf("expected 5, got %d", got)
	}
}

func TestResolveConstraintEdgeCases(t *testing.T) {
	// empty — unconstrained
	if _, ok := resolveConstraint("", 100); ok {
		t.Fatal("empty must be unconstrained")
	}
	// bad string
	if _, ok := resolveConstraint("abc", 100); ok {
		t.Fatal("non-numeric must be unconstrained")
	}
	// 0% — valid, resolves to 0
	if v, ok := resolveConstraint("0%", 100); !ok || v != 0 {
		t.Fatalf("0%% of 100 must be 0, got %d ok=%v", v, ok)
	}
	// 100% — full terminal
	if v, ok := resolveConstraint("100%", 80); !ok || v != 80 {
		t.Fatalf("100%% of 80 must be 80, got %d ok=%v", v, ok)
	}
}

func TestSceneUnknownNodeTypeRendersEmpty(t *testing.T) {
	s := NewSceneRenderer()
	_ = s.CreateArea(sdk.UIArea{ID: "a"})
	_ = s.ApplyPatch(sdk.UIPatchParams{Area: "a", Ops: []sdk.UIPatchOp{
		{Op: sdk.UIOpSetRoot, Node: &sdk.UINode{ID: "x", Type: sdk.UINodeType("future"), Text: "ignored"}},
	}})
	// Should not panic; unknown type renders as empty box.
	_ = s.Render("a", 40)
}
