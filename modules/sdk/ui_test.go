package sdk

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestUINodeRoundTrip(t *testing.T) {
	n := UINode{
		ID:   "msg-1",
		Type: UINodeVStack,
		Props: &UIProps{
			Border:  "rounded",
			Padding: []int{0, 1},
			Fg:      "accent",
			Width:   "fill",
		},
		Children: []UINode{
			{ID: "msg-1.txt", Type: UINodeText, Text: "hello"},
		},
	}
	data, err := json.Marshal(n)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got UINode
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.ID != n.ID || got.Type != n.Type || len(got.Children) != 1 {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
	if got.Children[0].Text != "hello" {
		t.Fatalf("child text lost: %+v", got.Children[0])
	}
}

func TestUINodeTextNodeOmitsChildren(t *testing.T) {
	data, _ := json.Marshal(UINode{ID: "t", Type: UINodeText, Text: "x"})
	if strings.Contains(string(data), "children") {
		t.Fatalf("text node must omit children: %s", data)
	}
	if strings.Contains(string(data), "props") {
		t.Fatalf("nil props must be omitted: %s", data)
	}
}

func TestUIPatchOpIndexZeroPreserved(t *testing.T) {
	// Index is *int so that a valid index of 0 survives the wire; a nil Index
	// (append) must be omitted entirely.
	zero := 0
	withIndex, _ := json.Marshal(UIPatchOp{Op: UIOpInsert, Index: &zero, Node: &UINode{ID: "n", Type: UINodeText}})
	if !strings.Contains(string(withIndex), `"index":0`) {
		t.Fatalf("index 0 must be preserved: %s", withIndex)
	}
	appendOp, _ := json.Marshal(UIPatchOp{Op: UIOpInsert, Node: &UINode{ID: "n", Type: UINodeText}})
	if strings.Contains(string(appendOp), "index") {
		t.Fatalf("nil index must be omitted: %s", appendOp)
	}
}

func TestUIPatchParamsRoundTrip(t *testing.T) {
	p := UIPatchParams{
		Area: "chat",
		Ops: []UIPatchOp{
			{Op: UIOpAppendText, ID: "msg-1.txt", Text: "world"},
			{Op: UIOpRemove, ID: "spinner"},
		},
	}
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got UIPatchParams
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Area != "chat" || len(got.Ops) != 2 {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
	if got.Ops[0].Op != UIOpAppendText || got.Ops[0].Text != "world" {
		t.Fatalf("append_text op lost: %+v", got.Ops[0])
	}
}

func TestUIAreaRoundTrip(t *testing.T) {
	a := UICreateAreaParams{Area: UIArea{ID: "chat", Placement: UIAreaMain, Weight: 3}}
	data, _ := json.Marshal(a)
	var got UICreateAreaParams
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Area.ID != "chat" || got.Area.Placement != UIAreaMain || got.Area.Weight != 3 {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
}
