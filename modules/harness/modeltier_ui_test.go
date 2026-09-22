package harness

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// newTierTestModel returns a Model whose model picker is backed by tier state,
// with tier callbacks recording into calls. Tags are stored per model so
// re-reads reflect prior tagging, exactly as the real cmd wiring does.
func newTierTestModel(calls *[]string) Model {
	m := newTestModel()
	m.width = 80
	m.height = 24
	m.activeModel = "m1"
	tiers := map[string][]string{}
	m.TagModelTierFn = func(tier, modelID string) error {
		tiers[modelID] = []string{tier}
		*calls = append(*calls, "tag:"+tier+"/"+modelID)
		return nil
	}
	m.ClearModelTierFn = func(tier string) error {
		for id, ts := range tiers {
			kept := ts[:0]
			for _, x := range ts {
				if x != tier {
					kept = append(kept, x)
				}
			}
			tiers[id] = kept
		}
		*calls = append(*calls, "clear:"+tier)
		return nil
	}
	m.ModelListFn = func() []ModelChoice {
		return []ModelChoice{
			{ID: "m1", Name: "One", ContextWindowKnown: true, Tiers: tiers["m1"]},
			{ID: "m2", Name: "Two", ContextWindowKnown: true, Tiers: tiers["m2"]},
		}
	}
	return m
}

// TestModelPickerTierKeysThroughUpdate drives real keystrokes through Update
// (not the helper methods) to prove the tagging keys work end-to-end.
func TestModelPickerTierKeysThroughUpdate(t *testing.T) {
	var calls []string
	m := newTierTestModel(&calls)

	m, cmd := callUpdate(m, CommandMsg{Name: "models"})
	if cmd == nil {
		t.Fatal("/models produced no command")
	}
	m, _ = callUpdate(m, cmd())
	if !m.picker.IsActive() {
		t.Fatal("picker not active after /models")
	}
	if !strings.Contains(m.picker.Title, "h=high") {
		t.Errorf("title = %q, want tagging hint", m.picker.Title)
	}

	m, _ = callUpdate(m, tea.KeyPressMsg{Code: 'h', Text: "h"})
	if len(calls) != 1 || calls[0] != "tag:high/m1" {
		t.Fatalf("after h: calls = %v, want [tag:high/m1]", calls)
	}
	if got := m.picker.Items[0].Sublabel; !strings.Contains(got, "tier: high") {
		t.Errorf("sublabel = %q, want a tier tag", got)
	}

	m, _ = callUpdate(m, tea.KeyPressMsg{Code: tea.KeyDown})
	m, _ = callUpdate(m, tea.KeyPressMsg{Code: 'l', Text: "l"})
	m, _ = callUpdate(m, tea.KeyPressMsg{Code: 'u', Text: "u"})
	want := []string{"tag:high/m1", "tag:low/m2", "clear:low"}
	if fmt.Sprint(calls) != fmt.Sprint(want) {
		t.Fatalf("calls = %v, want %v", calls, want)
	}
}

// TestModelPickerKeepsCursorAfterTagging guards the regression where reopening
// the picker reset the cursor to the first row, so a second key press edited a
// different model than the one the user was looking at.
func TestModelPickerKeepsCursorAfterTagging(t *testing.T) {
	var calls []string
	m := newTierTestModel(&calls)
	m, _ = callUpdate(m, showModelPickerMsg{})

	m, _ = callUpdate(m, tea.KeyPressMsg{Code: tea.KeyDown})
	if id, _ := m.picker.Highlighted(); id != "m2" {
		t.Fatalf("highlighted = %q, want m2", id)
	}
	m, _ = callUpdate(m, tea.KeyPressMsg{Code: 'h', Text: "h"})
	if id, _ := m.picker.Highlighted(); id != "m2" {
		t.Fatalf("after tagging, highlighted = %q, want m2", id)
	}
	m, _ = callUpdate(m, tea.KeyPressMsg{Code: 'u', Text: "u"})
	want := []string{"tag:high/m2", "clear:high"}
	if fmt.Sprint(calls) != fmt.Sprint(want) {
		t.Fatalf("calls = %v, want %v", calls, want)
	}
}

// TestModelPickerKeepsCursorWhenScrolled checks the highlight survives a reopen
// for a row that is only visible after scrolling.
func TestModelPickerKeepsCursorWhenScrolled(t *testing.T) {
	m := newTestModel()
	m.width = 80
	m.height = 12
	m.activeModel = "m0"
	items := make([]ModelChoice, 0, 30)
	for i := 0; i < 30; i++ {
		items = append(items, ModelChoice{ID: fmt.Sprintf("m%d", i), Name: "M", ContextWindowKnown: true})
	}
	m.ModelListFn = func() []ModelChoice { return items }
	m.TagModelTierFn = func(string, string) error { return nil }

	m, _ = callUpdate(m, showModelPickerMsg{})
	for i := 0; i < 29; i++ {
		m, _ = callUpdate(m, tea.KeyPressMsg{Code: tea.KeyDown})
	}
	m, _ = callUpdate(m, tea.KeyPressMsg{Code: 'h', Text: "h"})
	if id, _ := m.picker.Highlighted(); id != "m29" {
		t.Fatalf("highlighted = %q, want m29 after tagging", id)
	}
}

func TestShowModelTiersModal(t *testing.T) {
	m := newTestModel()
	m.width = 80
	m.height = 24
	m.ModelTierLabelsFn = func() []string {
		return []string{"high: anthropic · claude-opus-4-8 · thinking:high", "low: local · qwen3"}
	}
	m, _ = callUpdate(m, showModelTiersMsg{})
	for _, want := range []string{"Model tiers", "high:", "low:", "/model <tier>"} {
		if !strings.Contains(m.modalContent, want) {
			t.Errorf("modal missing %q:\n%s", want, m.modalContent)
		}
	}
}

func TestShowModelTiersEmptyNotifies(t *testing.T) {
	m := newTestModel()
	m.ModelTierLabelsFn = func() []string { return nil }
	m, _ = callUpdate(m, showModelTiersMsg{})
	if m.modalContent != "" {
		t.Errorf("empty tiers should not open a modal, got %q", m.modalContent)
	}
}

// TestSetModelMsgFromSkillPath mirrors what the skills extension triggers on
// activation: the host's set_model call becomes a setModelMsg, so a skill's
// `model:`/`thinking:` frontmatter applies through the normal Update cycle.
func TestSetModelMsgFromSkillPath(t *testing.T) {
	m := newTestModel()
	m.width = 80
	m.height = 24
	m.activeModel = "old"

	applied := ""
	m.TierNamesFn = func() []string { return []string{"high"} }
	m.ApplyModelTierFn = func(tier string) (string, string, error) {
		applied = tier
		return "anthropic", "claude-opus-4-8", nil
	}
	thinking := ""
	m.SetThinkingLevelFn = func(level string) error {
		thinking = level
		return nil
	}

	m, _ = callUpdate(m, setModelMsg{Model: "high", Thinking: "high"})
	if applied != "high" || m.activeModel != "claude-opus-4-8" || thinking != "high" {
		t.Fatalf("skill model switch: applied=%q model=%q thinking=%q",
			applied, m.activeModel, thinking)
	}
}

// TestSetModelMsgThinkingOnly covers a skill that declares only `thinking:`,
// which must not switch the model.
func TestSetModelMsgThinkingOnly(t *testing.T) {
	m := newTestModel()
	m.activeModel = "keep-me"
	selectCalled := false
	m.SelectModelFn = func(string) error { selectCalled = true; return nil }
	m.SetThinkingLevelFn = func(string) error { return nil }

	m, _ = callUpdate(m, setModelMsg{Model: "", Thinking: "medium"})
	if selectCalled {
		t.Error("an empty model must not trigger a model switch")
	}
	if m.activeModel != "keep-me" {
		t.Errorf("activeModel = %q, want unchanged", m.activeModel)
	}
}

// TestTierNameShadowsModelID pins the resolution order: a name that matches a
// configured tier is applied as a tier and never treated as a model ID.
func TestTierNameShadowsModelID(t *testing.T) {
	m := newTestModel()
	m.width, m.height = 80, 24
	m.TierNamesFn = func() []string { return []string{"high"} }
	m.ApplyModelTierFn = func(string) (string, string, error) { return "p", "tier-target", nil }
	selected := ""
	m.SelectModelFn = func(id string) error { selected = id; return nil }

	m.applyModelSelection("high")
	if selected != "" {
		t.Errorf("tier name fell through to model selection: %q", selected)
	}
	if m.activeModel != "tier-target" {
		t.Errorf("activeModel = %q, want tier-target", m.activeModel)
	}
}
