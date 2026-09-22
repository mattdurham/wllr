package harness

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestOpenRouterSpeedCommand(t *testing.T) {
	r := NewRegistry()
	registerBuiltins(r)

	msg := r.Dispatch("openrouter-speed", nil)()
	if _, ok := msg.(showOpenRouterSpeedPickerMsg); !ok {
		t.Fatalf("no-arg message = %T, want showOpenRouterSpeedPickerMsg", msg)
	}
	msg = r.Dispatch("openrouter-speed", []string{"nitro"})()
	got, ok := msg.(setOpenRouterSpeedMsg)
	if !ok {
		t.Fatalf("arg message = %T, want setOpenRouterSpeedMsg", msg)
	}
	if got.ID != "nitro" {
		t.Errorf("ID = %q, want nitro", got.ID)
	}
}

func TestOpenRouterSpeedPickerMarksCurrentAndApplies(t *testing.T) {
	m := newTestModel()
	m.width, m.height = 80, 24
	m.SpeedListFn = func() []SpeedChoice {
		return []SpeedChoice{
			{ID: "default", Label: "Default", Description: "balanced"},
			{ID: "nitro", Label: "Nitro", Description: "fastest"},
		}
	}
	applied := ""
	m.SelectSpeedFn = func(id string) error { applied = id; return nil }
	m.SetSpeedDisplay("default")

	m, _ = callUpdate(m, showOpenRouterSpeedPickerMsg{})
	if !m.picker.IsActive() {
		t.Fatal("picker not active")
	}
	if got := m.picker.Items[0].Sublabel; got != "balanced  (current)" {
		t.Errorf("current marker missing: %q", got)
	}

	// Select the second entry through the picker callback path.
	m, _ = callUpdate(m, tea.KeyPressMsg{Code: tea.KeyDown})
	m, cmd := callUpdate(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("no command from selection")
	}
	m, _ = callUpdate(m, cmd())
	if applied != "nitro" {
		t.Fatalf("applied = %q, want nitro", applied)
	}
	if m.ActiveSpeed() != "nitro" {
		t.Errorf("ActiveSpeed = %q, want nitro", m.ActiveSpeed())
	}
}

func TestOpenRouterSpeedUnavailableExplainsWhy(t *testing.T) {
	m := newTestModel()
	m.SpeedListFn = func() []SpeedChoice { return nil }
	m.SpeedUnavailableReasonFn = func() string { return "only OpenRouter models route" }
	// Must not open an empty picker.
	m, _ = callUpdate(m, showOpenRouterSpeedPickerMsg{})
	if m.picker.IsActive() {
		t.Error("empty routing list must not open the picker")
	}
}
