package server

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"strings"
	"testing"

	"github.com/mattdurham/wllr/modules/sdk"
)

func intp(i int) *int { return &i }

func TestSceneModel_CreateDuplicateRemove(t *testing.T) {
	m := NewSceneModel()
	if err := m.CreateArea(sdk.UIArea{ID: "chat", Placement: sdk.UIAreaMain}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := m.CreateArea(sdk.UIArea{ID: ""}); err == nil {
		t.Fatal("empty id must error")
	}
	if err := m.CreateArea(sdk.UIArea{ID: "chat"}); err == nil {
		t.Fatal("duplicate id must error")
	}
	if !m.HasArea("chat") {
		t.Fatal("area should exist")
	}
	m.RemoveArea("chat")
	if m.HasArea("chat") {
		t.Fatal("area should be removed")
	}
	m.RemoveArea("missing") // no-op, must not panic
}

// TestSceneModel_MatchesHarnessPatchSemantics pins the behaviors the terminal
// renderer tests assert, proving the shared sdk ops keep both front ends
// identical.
func TestSceneModel_MatchesHarnessPatchSemantics(t *testing.T) {
	m := NewSceneModel()
	if err := m.CreateArea(sdk.UIArea{ID: "a"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	// set_root + streaming append across a batch.
	if err := m.ApplyPatch(sdk.UIPatchParams{Area: "a", Ops: []sdk.UIPatchOp{
		{Op: sdk.UIOpSetRoot, Node: &sdk.UINode{ID: "txt", Type: sdk.UINodeText, Text: "Hel"}},
	}}); err != nil {
		t.Fatalf("set_root: %v", err)
	}
	if err := m.ApplyPatch(sdk.UIPatchParams{Area: "a", Ops: []sdk.UIPatchOp{
		{Op: sdk.UIOpAppendText, ID: "txt", Text: "lo "},
		{Op: sdk.UIOpAppendText, ID: "txt", Text: "world"},
	}}); err != nil {
		t.Fatalf("append: %v", err)
	}
	html, ok := m.AreaHTML("a")
	if !ok || !strings.Contains(html, "Hello world") {
		t.Fatalf("append result missing: ok=%v html=%q", ok, html)
	}

	// Batch atomicity: a failing op must leave the tree untouched.
	err := m.ApplyPatch(sdk.UIPatchParams{Area: "a", Ops: []sdk.UIPatchOp{
		{Op: sdk.UIOpAppendText, ID: "txt", Text: "X"},
		{Op: sdk.UIOpAppendText, ID: "missing", Text: "Y"},
	}})
	if err == nil {
		t.Fatal("expected batch error")
	}
	if html, _ := m.AreaHTML("a"); strings.Contains(html, "X") {
		t.Fatalf("failed batch must not mutate tree: %q", html)
	}

	// Unknown area is rejected.
	if err := m.ApplyPatch(sdk.UIPatchParams{Area: "nope"}); err == nil {
		t.Fatal("expected unknown area error")
	}

	// Insert at index 0 must order before an appended sibling.
	_ = m.ApplyPatch(sdk.UIPatchParams{Area: "a", Ops: []sdk.UIPatchOp{
		{Op: sdk.UIOpSetRoot, Node: &sdk.UINode{ID: "root", Type: sdk.UINodeVStack}},
		{Op: sdk.UIOpInsert, Parent: "root", Node: &sdk.UINode{ID: "b", Type: sdk.UINodeText, Text: "B"}},
	}})
	_ = m.ApplyPatch(sdk.UIPatchParams{Area: "a", Ops: []sdk.UIPatchOp{
		{Op: sdk.UIOpInsert, Parent: "root", Index: intp(0), Node: &sdk.UINode{ID: "a1", Type: sdk.UINodeText, Text: "A"}},
	}})
	html, _ = m.AreaHTML("a")
	if strings.Index(html, ">A<") > strings.Index(html, ">B<") {
		t.Fatalf("index-0 insert should order A before B: %q", html)
	}

	// replace_text swaps content; append_text on a non-text node errors.
	if err := m.ApplyPatch(sdk.UIPatchParams{Area: "a", Ops: []sdk.UIPatchOp{
		{Op: sdk.UIOpReplaceText, ID: "a1", Text: "renamed"},
	}}); err != nil {
		t.Fatalf("replace_text: %v", err)
	}
	if err := m.ApplyPatch(sdk.UIPatchParams{Area: "a", Ops: []sdk.UIPatchOp{
		{Op: sdk.UIOpAppendText, ID: "root", Text: "x"},
	}}); err == nil {
		t.Fatal("append_text on non-text node must error")
	}
}

func TestSceneModel_UpdateArea(t *testing.T) {
	m := NewSceneModel()
	_ = m.CreateArea(sdk.UIArea{ID: "a", MinHeight: "1"})
	if err := m.UpdateArea(sdk.UIUpdateAreaParams{ID: "a", MaxHeight: "5"}); err != nil {
		t.Fatalf("update: %v", err)
	}
	if err := m.UpdateArea(sdk.UIUpdateAreaParams{ID: ""}); err == nil {
		t.Fatal("empty id must error")
	}
	if err := m.UpdateArea(sdk.UIUpdateAreaParams{ID: "nope"}); err == nil {
		t.Fatal("unknown id must error")
	}
	snap := m.Snapshot()
	if len(snap) != 1 || snap[0].Area.MaxHeight != "5" || snap[0].Area.MinHeight != "1" {
		t.Fatalf("update should merge, not replace: %+v", snap)
	}
}

func TestSceneModel_SnapshotOrderAndEmptyRoot(t *testing.T) {
	m := NewSceneModel()
	_ = m.CreateArea(sdk.UIArea{ID: "first", Placement: sdk.UIAreaMain})
	_ = m.CreateArea(sdk.UIArea{ID: "second", Placement: sdk.UIAreaSidebar})
	snap := m.Snapshot()
	if len(snap) != 2 || snap[0].Area.ID != "first" || snap[1].Area.ID != "second" {
		t.Fatalf("snapshot must follow creation order: %+v", snap)
	}
	// A registered-but-unpopulated area still renders a (empty) region.
	if !strings.Contains(snap[0].HTML, `data-area-id="first"`) {
		t.Fatalf("empty area should render a region: %q", snap[0].HTML)
	}
}

func TestIsTextAppendOnly(t *testing.T) {
	if IsTextAppendOnly(nil) {
		t.Fatal("empty batch is not an append")
	}
	if !IsTextAppendOnly([]sdk.UIPatchOp{{Op: sdk.UIOpAppendText}, {Op: sdk.UIOpAppendText}}) {
		t.Fatal("all-append batch should be append-only")
	}
	if IsTextAppendOnly([]sdk.UIPatchOp{{Op: sdk.UIOpAppendText}, {Op: sdk.UIOpSetRoot}}) {
		t.Fatal("mixed batch is not append-only")
	}
}
