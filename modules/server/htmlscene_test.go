package server

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"strings"
	"testing"

	"github.com/mattdurham/wllr/modules/sdk"
)

func TestRenderAreaHTML_TextEscapingAndStructure(t *testing.T) {
	area := sdk.UIArea{ID: "chat", Placement: sdk.UIAreaMain}
	root := &sdk.UINode{ID: "root", Type: sdk.UINodeVStack, Children: []sdk.UINode{
		{ID: "m1", Type: sdk.UINodeText, Text: `<script>alert("x")</script> & "quoted"`},
	}}
	out := RenderAreaHTML(area, root)
	if !strings.Contains(out, `data-area-id="chat"`) {
		t.Fatalf("missing area id: %q", out)
	}
	if !strings.Contains(out, `data-placement="main"`) {
		t.Fatalf("missing placement: %q", out)
	}
	if !strings.Contains(out, `data-node-id="m1"`) {
		t.Fatalf("missing node id: %q", out)
	}
	if !strings.Contains(out, "wn-text-content") {
		t.Fatalf("text node must render an addressable text span: %q", out)
	}
	// The message text must never appear as live markup.
	if strings.Contains(out, "<script>") {
		t.Fatalf("text was not escaped: %q", out)
	}
	if !strings.Contains(out, "&lt;script&gt;") || !strings.Contains(out, "&amp;") {
		t.Fatalf("expected escaped entities: %q", out)
	}
}

// TestRenderNodeHTML_CollapsesBlankLineRuns pins the HTML renderer to the shared
// spacing rule: the terminal renderer (harness renderNode) runs a text node
// through sdk.CollapseBlankLineRuns, so the browser must produce the same body.
func TestRenderNodeHTML_CollapsesBlankLineRuns(t *testing.T) {
	text := "line one\n\n\n\nline two\n\n\nline three"
	node := sdk.UINode{ID: "n", Type: sdk.UINodeText, Text: text}
	out := RenderNodeHTML(node)

	// The shared helper is the contract both renderers consume.
	want := ANSItoHTML(sdk.CollapseBlankLineRuns(text))
	if !strings.Contains(out, want) {
		t.Fatalf("text body = %q, want %q", out, want)
	}
	// No run of 3+ newlines survives into the browser output.
	if strings.Contains(out, "\n\n\n") {
		t.Fatalf("blank-line run not collapsed: %q", out)
	}
}

func TestRenderAreaHTML_AllNodeTypes(t *testing.T) {
	cases := []struct {
		node sdk.UINode
		want string
	}{
		{sdk.UINode{ID: "n", Type: sdk.UINodeText, Text: "hi"}, "wn-text"},
		{sdk.UINode{ID: "n", Type: sdk.UINodeVStack}, "wn-vstack"},
		{sdk.UINode{ID: "n", Type: sdk.UINodeHStack}, "wn-hstack"},
		{sdk.UINode{ID: "n", Type: sdk.UINodeViewport}, "wn-viewport"},
		{sdk.UINode{ID: "n", Type: sdk.UINodeSpinner}, "wn-spinner"},
		{sdk.UINode{ID: "n", Type: sdk.UINodeDivider}, "wn-divider"},
		// Unknown type must degrade to an empty box, not error.
		{sdk.UINode{ID: "n", Type: sdk.UINodeType("future"), Text: "ignored"}, "wn-unknown"},
	}
	for _, tc := range cases {
		out := RenderNodeHTML(tc.node)
		if !strings.Contains(out, tc.want) {
			t.Errorf("type %q: want class %q in %q", tc.node.Type, tc.want, out)
		}
		if !strings.Contains(out, `data-node-id="n"`) {
			t.Errorf("type %q: missing node id: %q", tc.node.Type, out)
		}
	}
}

func TestRenderNodeHTML_PropsToCSS(t *testing.T) {
	node := sdk.UINode{ID: "n", Type: sdk.UINodeText, Text: "x", Props: &sdk.UIProps{
		Fg:        "accent",
		Bg:        "bg",
		Bold:      true,
		Italic:    true,
		Underline: true,
		Faint:     true,
		Border:    "rounded",
		Align:     "center",
		Width:     "fill",
		Height:    "3",
		Padding:   []int{1, 2},
		Margin:    []int{1},
		Wrap:      true,
	}}
	out := RenderNodeHTML(node)
	for _, want := range []string{
		"color:var(--wn-accent",
		"background-color:var(--wn-bg",
		"font-weight:700;",
		"font-style:italic;",
		"text-decoration:underline;",
		"opacity:0.6;",
		"border-radius:6px;",
		"text-align:center;",
		"width:100%;",
		"height:3lh;",
		"padding:1ch 2ch 1ch 2ch;",
		"margin:1ch 1ch 1ch 1ch;",
		"white-space:pre-wrap;",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("props: want %q in %q", want, out)
		}
	}
}

func TestRenderNodeHTML_NumericWidthAndHexColor(t *testing.T) {
	node := sdk.UINode{ID: "n", Type: sdk.UINodeText, Text: "x", Props: &sdk.UIProps{
		Width: "20",
		Fg:    "#123456",
	}}
	out := RenderNodeHTML(node)
	if !strings.Contains(out, "width:20ch;") {
		t.Errorf("numeric width should map to ch: %q", out)
	}
	if !strings.Contains(out, "color:#123456;") {
		t.Errorf("hex colour should pass through: %q", out)
	}
}

func TestRenderNodeHTML_UnknownThemeTokenOmitted(t *testing.T) {
	node := sdk.UINode{ID: "n", Type: sdk.UINodeText, Text: "x", Props: &sdk.UIProps{Fg: "chartreuse"}}
	out := RenderNodeHTML(node)
	if strings.Contains(out, "color:") {
		t.Errorf("unresolvable token must emit no colour: %q", out)
	}
}

func TestRenderAreaHTML_Constraints(t *testing.T) {
	area := sdk.UIArea{
		ID:        "s",
		Placement: sdk.UIAreaStatus,
		MinHeight: "1", MaxHeight: "25%",
		MinWidth: "40", MaxWidth: "80%",
		Weight: 2,
	}
	out := RenderAreaHTML(area, nil)
	for _, want := range []string{
		"min-height:1lh;", "max-height:25%;",
		"min-width:40ch;", "max-width:80%;",
		"flex-grow:2;",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("constraints: want %q in %q", want, out)
		}
	}
}
