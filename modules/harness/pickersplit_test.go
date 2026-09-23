package harness

// pickersplit_test.go covers the two-pane split picker used by /history:
// type-to-filter on the left, the highlighted item's Preview on the right,
// pgup/pgdn preview scrolling, and the ShowPickerMsg routing that opens it.

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/mattdurham/wllr/modules/sdk"
)

func splitTestItems() []sdk.ShowPickerItem {
	return []sdk.ShowPickerItem{
		{ID: "a.jsonl", Label: "2026-09-22 10:00", Sublabel: "first session", Preview: "you:\n  hello from alpha"},
		{ID: "b.jsonl", Label: "2026-09-22 11:00", Sublabel: "second session", Preview: "you:\n  hello from beta"},
	}
}

// ---- layout ----

func TestSplitPicker_RendersBothPanes(t *testing.T) {
	var p PickerView
	p.OpenSplit("Select a session", splitTestItems(), "history:session_selected")
	p.SetSize(80, 20)

	out := p.View()
	lines := strings.Split(out, "\n")
	if len(lines) != 20 {
		t.Fatalf("View rendered %d lines, want exactly height (20)", len(lines))
	}
	if !strings.Contains(out, "2026-09-22 10:00") {
		t.Error("left pane missing first item label")
	}
	if !strings.Contains(out, "hello from alpha") {
		t.Error("right pane missing highlighted item preview")
	}
	if strings.Contains(out, "hello from beta") {
		t.Error("right pane should show only the highlighted item's preview")
	}
}

func TestSplitPicker_PreviewFollowsSelection(t *testing.T) {
	var p PickerView
	p.OpenSplit("Select a session", splitTestItems(), "history:session_selected")
	p.SetSize(80, 20)

	p.HandleKey(keyMsg(tea.KeyDown, 0))
	out := p.View()
	if !strings.Contains(out, "hello from beta") {
		t.Error("moving down should show the second item's preview")
	}
	if strings.Contains(out, "hello from alpha") {
		t.Error("previous preview should be gone after moving down")
	}
}

func TestSplitPicker_EmptyPreviewFallsBackToSublabel(t *testing.T) {
	var p PickerView
	p.OpenSplit("Select", []sdk.ShowPickerItem{
		{ID: "x", Label: "X", Sublabel: "the sublabel"},
	}, "cb")
	p.SetSize(80, 20)

	if out := p.View(); !strings.Contains(out, "the sublabel") {
		t.Error("empty Preview should fall back to Sublabel in the right pane")
	}
}

// ---- type-to-filter ----

func TestSplitPicker_FiltersOnPreviewContent(t *testing.T) {
	var p PickerView
	p.OpenSplit("Select a session", splitTestItems(), "history:session_selected")
	p.SetSize(80, 20)

	// "beta" appears only in the second item's preview text.
	p.HandleKey(keyMsg(0, 0)) // no-op sanity; Text empty types nothing
	p.HandleKey(tea.KeyPressMsg{Text: "b"})
	p.HandleKey(tea.KeyPressMsg{Text: "e"})
	p.HandleKey(tea.KeyPressMsg{Text: "t"})
	p.HandleKey(tea.KeyPressMsg{Text: "a"})

	if len(p.filtered) != 1 || p.Items[p.filtered[0]].ID != "b.jsonl" {
		t.Fatalf("preview filtering failed: filtered=%v", p.filtered)
	}
	selected, id, _ := p.HandleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !selected || id != "b.jsonl" {
		t.Fatalf("enter after filter: selected=%v id=%q", selected, id)
	}
}

func TestSplitPicker_SelectionResetRestartsPreviewScroll(t *testing.T) {
	longPreview := strings.Repeat("row\n", 200)
	var p PickerView
	p.OpenSplit("Select", []sdk.ShowPickerItem{
		{ID: "a", Label: "A", Preview: longPreview},
		{ID: "b", Label: "B", Preview: longPreview},
	}, "cb")
	p.SetSize(80, 20)

	p.HandleKey(keyMsg(tea.KeyPgDown, 0))
	if p.previewScroll == 0 {
		t.Fatal("pgdown did not scroll the preview")
	}
	p.HandleKey(keyMsg(tea.KeyDown, 0))
	if p.previewScroll != 0 {
		t.Fatalf("selection change should reset preview scroll, got %d", p.previewScroll)
	}
}

func TestSplitPicker_PgUpClampsAtTop(t *testing.T) {
	var p PickerView
	p.OpenSplit("Select", []sdk.ShowPickerItem{
		{ID: "a", Label: "A", Preview: strings.Repeat("row\n", 200)},
	}, "cb")
	p.SetSize(80, 20)

	p.HandleKey(keyMsg(tea.KeyPgUp, 0))
	if p.previewScroll != 0 {
		t.Fatalf("pgup above the top should clamp to 0, got %d", p.previewScroll)
	}
}

func TestSplitPicker_PgDownScrollsPreviewNotList(t *testing.T) {
	var p PickerView
	p.OpenSplit("Select", []sdk.ShowPickerItem{
		{ID: "a", Label: "A", Preview: strings.Repeat("row\n", 200)},
		{ID: "b", Label: "B", Preview: strings.Repeat("row\n", 200)},
	}, "cb")
	p.SetSize(80, 20)

	p.HandleKey(keyMsg(tea.KeyPgDown, 0))
	if p.selectedIdx != 0 {
		t.Fatalf("pgdown must not move the list selection, selectedIdx=%d", p.selectedIdx)
	}
	if p.previewScroll == 0 {
		t.Fatal("pgdown must scroll the preview pane")
	}
}

// ---- Model routing ----

func TestShowPickerMsg_SplitOpensSplitPicker(t *testing.T) {
	m := newTestModel()
	m, _ = callUpdate(m, tea.WindowSizeMsg{Width: 120, Height: 40})
	m, _ = callUpdate(m, ShowPickerMsg{
		Title:    "Select a session",
		Callback: "history:session_selected",
		Items:    splitTestItems(),
		Split:    true,
	})
	if !m.picker.IsActive() || !m.picker.preview || !m.picker.searchable {
		t.Fatalf("split msg should open a searchable preview picker (active=%v preview=%v searchable=%v)",
			m.picker.IsActive(), m.picker.preview, m.picker.searchable)
	}
	if out := m.picker.View(); !strings.Contains(out, "hello from alpha") {
		t.Error("split picker view should include the preview pane")
	}
}

func TestShowPickerMsg_PlainOpensSinglePane(t *testing.T) {
	m := newTestModel()
	m, _ = callUpdate(m, tea.WindowSizeMsg{Width: 120, Height: 40})
	m, _ = callUpdate(m, ShowPickerMsg{
		Title:    "Plain",
		Callback: "cb",
		Items:    splitTestItems(),
	})
	if !m.picker.IsActive() || m.picker.preview {
		t.Fatalf("plain msg must not open the split layout (preview=%v)", m.picker.preview)
	}
}
