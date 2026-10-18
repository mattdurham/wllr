package harness

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/mattdurham/wllr/modules/sdk"
)

func TestStatusLineFitsWidth(t *testing.T) {
	s := NewSceneRenderer()
	if err := s.CreateArea(sdk.UIArea{ID: statuslineAreaID}); err != nil {
		t.Fatal(err)
	}
	root := &sdk.UINode{ID: "statusline-root", Type: sdk.UINodeHStack, Children: []sdk.UINode{
		{ID: "sl-provider", Type: sdk.UINodeText, Text: "OpenRouter"},
		{ID: "sl-cwd", Type: sdk.UINodeText, Text: "~/source/a/very/long/directory/project"},
		{ID: "sl-sep2", Type: sdk.UINodeText, Text: "  "},
		{ID: "sl-model", Type: sdk.UINodeText, Text: "\x1b[32m模型-very-long-model-name\x1b[0m"},
		{ID: "sl-agent", Type: sdk.UINodeText, Text: "agent:main"},
		{ID: "sl-working", Type: sdk.UINodeText, Text: "working"},
		{ID: "sl-tps", Type: sdk.UINodeText, Text: "  87 t/s"},
		{ID: "sl-ctx", Type: sdk.UINodeText, Text: "  ctx:995/200k"},
		{ID: "sl-compact", Type: sdk.UINodeText, Text: "  C1"},
	}}
	if err := s.ApplyPatch(sdk.UIPatchParams{Area: statuslineAreaID, Ops: []sdk.UIPatchOp{{Op: sdk.UIOpSetRoot, Node: root}}}); err != nil {
		t.Fatal(err)
	}
	for _, width := range []int{160, 90, 60, 40, 20, 2, 1, 0} {
		out := s.Render(statuslineAreaID, width)
		plain := ansi.Strip(out)
		if ansi.StringWidth(out) > width || strings.Contains(out, "\n") {
			t.Errorf("width %d: overflowing status %q", width, out)
		}
		if strings.Contains(plain, "OpenRouter") {
			t.Errorf("width %d: provider still visible", width)
		}
		if width >= 40 && (!strings.Contains(plain, "ctx:995/200k") || !strings.Contains(plain, "模型")) {
			t.Errorf("width %d: lost model/context: %q", width, plain)
		}
		if width == 90 && !strings.Contains(plain, "~/…/project") {
			t.Errorf("directory not collapsed: %q", plain)
		}
		if width == 40 && strings.Contains(plain, "project") {
			t.Errorf("directory not hidden: %q", plain)
		}
	}
	if out := s.Render(statuslineAreaID, 160); !strings.Contains(out, "~/source/a/very/long/directory/project") {
		t.Fatalf("resize did not restore directory: %q", out)
	}
}
