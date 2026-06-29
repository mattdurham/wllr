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

func TestSceneInsertAndRemove(t *testing.T) {
	s := NewSceneRenderer()
	_ = s.CreateArea(sdk.UIArea{ID: "a"})
	_ = s.ApplyPatch(sdk.UIPatchParams{Area: "a", Ops: []sdk.UIPatchOp{
		{Op: sdk.UIOpSetRoot, Node: &sdk.UINode{ID: "root", Type: sdk.UINodeVStack}},
	}})
	// Insert two children, second at index 0 so it appears first.
	_ = s.ApplyPatch(sdk.UIPatchParams{Area: "a", Ops: []sdk.UIPatchOp{
		{Op: sdk.UIOpInsert, Parent: "root", Node: &sdk.UINode{ID: "b", Type: sdk.UINodeText, Text: "B"}},
		{Op: sdk.UIOpInsert, Parent: "root", Index: intp(0), Node: &sdk.UINode{ID: "a1", Type: sdk.UINodeText, Text: "A"}},
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

func TestSceneUnknownNodeTypeRendersEmpty(t *testing.T) {
	s := NewSceneRenderer()
	_ = s.CreateArea(sdk.UIArea{ID: "a"})
	_ = s.ApplyPatch(sdk.UIPatchParams{Area: "a", Ops: []sdk.UIPatchOp{
		{Op: sdk.UIOpSetRoot, Node: &sdk.UINode{ID: "x", Type: sdk.UINodeType("future"), Text: "ignored"}},
	}})
	// Should not panic; unknown type renders as empty box.
	_ = s.Render("a", 40)
}
