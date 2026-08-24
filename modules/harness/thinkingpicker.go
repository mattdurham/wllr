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
		if reason := m.thinkingUnsupportedReason(); reason != "" {
			m.pushNotification("Thinking not available — " + reason)
		}
		if m.ThinkingStatusFn != nil {
			if status := m.ThinkingStatusFn(); status != "" {
				m.live.setStatus("think", status)
			}
		}
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

// thinkingUnsupportedReason returns the current model's "not supported" reason
// from the optional reason fn, or "" for the generic message.
func (m *Model) thinkingUnsupportedReason() string {
	if m.ThinkingUnsupportedReasonFn == nil {
		return ""
	}
	return m.ThinkingUnsupportedReasonFn()
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

// SetThinkingForModel switches the active thinking display after a model or
// provider switch: it applies the mode via SelectThinkingFn (so the main
// agent's provider options match the new model, and the choice is persisted
// for the provider) and refreshes the status. An empty level clears the
// display. No-op when the callback is not wired.
func (m *Model) SetThinkingForModel(levelID string) {
	if m.SelectThinkingFn == nil {
		return
	}
	if err := m.SelectThinkingFn(levelID); err != nil {
		m.pushNotification(fmt.Sprintf("⚠ could not set thinking level: %v", err))
		return
	}
	m.activeThinking = levelID
	m.live.setStatus("think", levelID)
}

// ActiveThinking returns the currently displayed reasoning level ("" when
// none, "unavailable" when the model cannot reason).
func (m *Model) ActiveThinking() string { return m.activeThinking }

// SetThinkingUnavailable reflects a model without reasoning support in the
// status bar and clears the displayed level.
func (m *Model) SetThinkingUnavailable() {
	m.activeThinking = ""
	m.live.setStatus("think", "unavailable")
}
