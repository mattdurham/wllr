package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"fmt"

	"github.com/mattdurham/wllr/modules/sdk"
)

// openRouterSpeedPickerCallback routes a routing-preference choice to the core
// handler instead of dispatching EventOnCommand to a WASM extension.
const openRouterSpeedPickerCallback = "__wllr:openrouter_speed"

// SpeedChoice is one selectable OpenRouter provider-routing preference. ID is
// the stored value; Label and Description are shown in the picker.
type SpeedChoice struct {
	ID          string
	Label       string
	Description string
}

// showOpenRouterSpeedPickerMsg opens the routing-preference picker. Emitted by
// /openrouter-speed with no argument.
type showOpenRouterSpeedPickerMsg struct{}

// setOpenRouterSpeedMsg applies a routing preference. Emitted by
// /openrouter-speed <option>.
type setOpenRouterSpeedMsg struct{ ID string }

// openOpenRouterSpeedPicker lists the routing preferences. An empty list means
// the command is unavailable (not wired, or the provider is not OpenRouter), so
// the picker reports why instead of opening empty.
func (m *Model) openOpenRouterSpeedPicker() {
	if m.SpeedListFn == nil {
		m.pushNotification("Provider routing options are not available.")
		return
	}
	choices := m.SpeedListFn()
	if len(choices) == 0 {
		if reason := m.speedUnavailableReason(); reason != "" {
			m.pushNotification("Provider routing not available — " + reason)
		}
		return
	}
	items := make([]sdk.ShowPickerItem, 0, len(choices))
	for _, c := range choices {
		sub := c.Description
		if c.ID == m.activeSpeed {
			sub += "  (current)"
		}
		if sub == "" {
			sub = c.ID
		}
		items = append(items, sdk.ShowPickerItem{ID: c.ID, Label: c.Label, Sublabel: sub})
	}
	m.picker.Open("OpenRouter provider routing  (↑↓ · enter · esc)", items, openRouterSpeedPickerCallback)
	m.picker.SetSize(m.width, m.chatHeight())
}

// speedUnavailableReason returns the explanation for an empty routing list.
func (m *Model) speedUnavailableReason() string {
	if m.SpeedUnavailableReasonFn == nil {
		return ""
	}
	return m.SpeedUnavailableReasonFn()
}

// applySpeedSelection applies a routing preference via SelectSpeedFn (which
// updates the main agent's provider options and persists the choice), then
// updates the status display. Errors surface as notifications.
func (m *Model) applySpeedSelection(id string) {
	if id == "" {
		return
	}
	if m.SelectSpeedFn != nil {
		if err := m.SelectSpeedFn(id); err != nil {
			m.pushNotification(fmt.Sprintf("⚠ could not set provider routing: %v", err))
			return
		}
	}
	m.activeSpeed = id
	m.live.setStatus("speed", id)
	m.pushNotification("Provider routing set to: " + id)
}

// SetSpeedDisplay reflects the persisted routing preference without applying it.
// Used at startup and after a provider switch.
func (m *Model) SetSpeedDisplay(id string) {
	m.activeSpeed = id
	m.live.setStatus("speed", id)
}

// ActiveSpeed returns the currently displayed routing preference.
func (m *Model) ActiveSpeed() string { return m.activeSpeed }
