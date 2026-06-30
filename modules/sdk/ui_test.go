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

func TestUIAreaConstraintsRoundTrip(t *testing.T) {
	a := UICreateAreaParams{Area: UIArea{
		ID:        "statusline",
		Placement: UIAreaStatus,
		Weight:    1,
		MinHeight: "1",
		MaxHeight: "5",
		MinWidth:  "20%",
		MaxWidth:  "100%",
	}}
	data, err := json.Marshal(a)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got UICreateAreaParams
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	g := got.Area
	if g.ID != "statusline" || g.Placement != UIAreaStatus || g.Weight != 1 {
		t.Fatalf("base fields lost: %+v", g)
	}
	if g.MinHeight != "1" || g.MaxHeight != "5" {
		t.Fatalf("height constraints lost: min=%q max=%q", g.MinHeight, g.MaxHeight)
	}
	if g.MinWidth != "20%" || g.MaxWidth != "100%" {
		t.Fatalf("width constraints lost: min=%q max=%q", g.MinWidth, g.MaxWidth)
	}
}

func TestUIUpdateAreaParamsRoundTrip(t *testing.T) {
	w := 3
	p := UIUpdateAreaParams{
		ID:        "statusline",
		MinHeight: "2",
		MaxHeight: "10",
		MinWidth:  "50%",
		MaxWidth:  "100%",
		Weight:    &w,
	}
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got UIUpdateAreaParams
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.ID != "statusline" || got.MinHeight != "2" || got.MaxHeight != "10" {
		t.Fatalf("fields lost: %+v", got)
	}
	if got.MinWidth != "50%" || got.MaxWidth != "100%" {
		t.Fatalf("width constraints lost: %+v", got)
	}
	if got.Weight == nil || *got.Weight != 3 {
		t.Fatalf("weight lost: %+v", got.Weight)
	}
}

func TestUIUpdateAreaParamsOmitsUnsetFields(t *testing.T) {
	// Only ID and MaxHeight set — other constraint fields must be absent in JSON.
	p := UIUpdateAreaParams{ID: "statusline", MaxHeight: "3"}
	data, _ := json.Marshal(p)
	s := string(data)
	for _, absent := range []string{"min_height", "min_width", "max_width", "weight"} {
		if strings.Contains(s, absent) {
			t.Fatalf("unexpected field %q in %s", absent, s)
		}
	}
	if !strings.Contains(s, "max_height") {
		t.Fatalf("max_height must be present: %s", s)
	}
}

func TestUIAreaInputPlacement(t *testing.T) {
	if UIAreaInput != UIAreaPlacement("input") {
		t.Fatalf("UIAreaInput must equal \"input\", got %q", UIAreaInput)
	}
}

func TestTokenPayloadRoundTrip(t *testing.T) {
	p := TokenPayload{AgentID: "main", Text: "hello "}
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got TokenPayload
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.AgentID != "main" || got.Text != "hello " {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
}

func TestLogBatchPayloadRoundTrip(t *testing.T) {
	p := LogBatchPayload{Records: []LogRecord{
		{
			Time:    "2026-06-30T12:00:00Z",
			Level:   "info",
			Message: "stream done",
			Attrs:   []LogAttr{{Key: "tokens", Value: "42"}, {Key: "agent", Value: "main"}},
		},
		{Time: "2026-06-30T12:00:01Z", Level: "error", Message: "boom"},
	}}
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got LogBatchPayload
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.Records) != 2 {
		t.Fatalf("records: got %d, want 2", len(got.Records))
	}
	r0 := got.Records[0]
	if r0.Level != "info" || r0.Message != "stream done" || len(r0.Attrs) != 2 {
		t.Fatalf("record 0 mismatch: %+v", r0)
	}
	if r0.Attrs[0].Key != "tokens" || r0.Attrs[0].Value != "42" {
		t.Fatalf("attr order/content lost: %+v", r0.Attrs)
	}
	// Empty Attrs omitted on the wire.
	if strings.Contains(string(data), `"attrs":[]`) {
		t.Fatalf("empty attrs must be omitted: %s", data)
	}
}
