package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
	"github.com/mattdurham/wllr/modules/sdk"
)

// ModelChoice is one selectable model for the /model picker. ID is the wire
// model identifier passed to the provider; Name is a human label.
type ModelChoice struct {
	ID       string
	Name     string
	Sublabel string
}

// openModelPicker builds the picker items from ModelListFn and opens the picker
// with the core model-selection callback. No-op (with a notification) when no
// model lister is wired or the list is empty.
func (m *Model) openModelPicker() {
	if m.ModelListFn == nil {
		m.pushNotification("Model selection is not available.")
		return
	}
	choices := m.ModelListFn()
	if len(choices) == 0 {
		m.pushNotification("No models available for the current provider.")
		return
	}
	items := make([]sdk.ShowPickerItem, 0, len(choices))
	for _, c := range choices {
		sub := c.Sublabel
		if sub == "" {
			sub = c.ID
		}
		if c.ID == m.activeModel {
			sub += "  (current)"
		}
		items = append(items, sdk.ShowPickerItem{ID: c.ID, Label: c.Name, Sublabel: sub})
	}
	m.picker.Open("Select a model  (↑↓ · enter · esc)", items, modelPickerCallback)
	m.picker.SetSize(m.width, m.chatHeight())
}

// applyModelSelection switches the active model via SelectModelFn (which rebuilds
// the main agent's language model, updates the context window, and persists the
// choice), then updates the status display. Errors are surfaced as notifications.
func (m *Model) applyModelSelection(modelID string) tea.Cmd {
	if modelID == "" {
		return nil
	}
	if m.SelectModelFn != nil {
		if err := m.SelectModelFn(modelID); err != nil {
			m.pushNotification(fmt.Sprintf("⚠ could not switch model: %v", err))
			return nil
		}
	}
	cmd := m.setActiveProviderModel("", modelID)
	m.pushNotification("Model set to: " + modelID)
	return cmd
}
