package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"fmt"

	"github.com/mattdurham/wllr/modules/sdk"
)

// ThinkingChoice is one selectable reasoning level for the /thinking picker.
// ID is the level identifier (e.g. "off", "low", "high"); Label is a human
// description.
type ThinkingChoice struct {
	ID    string
	Label string
}

// openThinkingPicker builds the picker items from ThinkingListFn and opens the
// picker with the core thinking-selection callback. No-op (with a notification)
// when no lister is wired or the list is empty.
func (m *Model) openThinkingPicker() {
	if m.ThinkingListFn == nil {
		m.pushNotification("Thinking-level selection is not available.")
		return
	}
	choices := m.ThinkingListFn()
	if len(choices) == 0 {
		m.pushNotification("No thinking levels available.")
		return
	}
	items := make([]sdk.ShowPickerItem, 0, len(choices))
	for _, c := range choices {
		sub := c.ID
		if c.ID == m.activeThinking {
			sub = c.ID + "  (current)"
		}
		items = append(items, sdk.ShowPickerItem{ID: c.ID, Label: c.Label, Sublabel: sub})
	}
	m.picker.Open("Select a thinking level  (↑↓ · enter · esc)", items, thinkingPickerCallback)
	m.picker.SetSize(m.width, m.chatHeight())
}

// applyThinkingSelection switches the active thinking level via SelectThinkingFn
// (which updates the main agent's provider options and persists the choice),
// then updates the status display. Errors are surfaced as notifications.
func (m *Model) applyThinkingSelection(levelID string) {
	if levelID == "" {
		return
	}
	if m.SelectThinkingFn != nil {
		if err := m.SelectThinkingFn(levelID); err != nil {
			m.pushNotification(fmt.Sprintf("⚠ could not set thinking level: %v", err))
			return
		}
	}
	m.activeThinking = levelID
	m.live.setStatus("think", levelID)
	m.pushNotification("Thinking level set to: " + levelID)
}
