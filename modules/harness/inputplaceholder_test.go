package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

const inputLaunchHint = "Type a message"

func TestInputArea_PlaceholderOnlyShownAtLaunch(t *testing.T) {
	const hint = inputLaunchHint

	for _, tt := range []struct {
		name  string
		value string
	}{
		{name: "command", value: "/help"},
		{name: "message", value: "hello"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			input := NewInputArea(80)
			view := ansi.Strip(input.View())
			if !strings.Contains(view, hint) {
				t.Fatalf("launch view should contain the input hint; view = %q", view)
			}

			input.SetValue(tt.value)
			input, _ = input.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			view = ansi.Strip(input.View())
			if strings.Contains(view, hint) {
				t.Fatal("input hint returned after the first submission")
			}

			input.SetValue("draft")
			input.Reset()
			view = ansi.Strip(input.View())
			if strings.Contains(view, hint) {
				t.Fatal("input hint returned after a later reset")
			}
		})
	}
}

func TestModel_AutocompleteCommandRetiresLaunchPlaceholder(t *testing.T) {
	m := newTestModel()
	m.input.SetValue("/help")
	m.updateSuggestions()
	if len(m.suggestions) == 0 {
		t.Fatal("precondition: /help should have an autocomplete suggestion")
	}

	m, _ = callUpdate(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if view := ansi.Strip(m.input.View()); strings.Contains(view, inputLaunchHint) {
		t.Fatalf("autocomplete command restored the launch hint: %q", view)
	}
}
