package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
	"github.com/mattdurham/wllr/modules/sdk"
)

// ProviderChoice is one selectable provider for the startup login picker.
type ProviderChoice struct {
	ID       string
	Name     string
	Sublabel string
}

func (m *Model) openLoginProviderPicker() {
	if m.ProviderListFn == nil {
		m.pushNotification("Provider selection is not available.")
		return
	}
	choices := m.ProviderListFn()
	if len(choices) == 0 {
		m.pushNotification("No login providers are available.")
		return
	}
	items := make([]sdk.ShowPickerItem, 0, len(choices))
	for _, c := range choices {
		items = append(items, sdk.ShowPickerItem{ID: c.ID, Label: c.Name, Sublabel: c.Sublabel})
	}
	m.picker.Open("Select a provider  (↑↓ · enter · esc)", items, loginProviderPickerCallback)
	m.picker.SetSize(m.width, m.chatHeight())
}

func (m *Model) applyLoginProviderSelection(provider string) tea.Cmd {
	if provider == "" {
		return nil
	}
	requiresLogin := false
	model := ""
	if m.SelectProviderFn != nil {
		var err error
		model, requiresLogin, err = m.SelectProviderFn(provider)
		if err != nil {
			m.pushNotification(fmt.Sprintf("⚠ could not select provider: %v", err))
			return nil
		}
	}
	m.activeProvider = provider
	m.live.setProvider(provider)
	if model != "" {
		m.activeModel = model
		m.live.setModel(model)
	}
	if !requiresLogin {
		m.pushNotification("Provider set to: " + provider)
		return nil
	}
	if m.RecordAuthFn != nil {
		if err := m.RecordAuthFn(provider, authMethodOAuth); err != nil {
			m.pushNotification(fmt.Sprintf("⚠ could not record auth choice: %v", err))
		}
	}
	return m.beginOAuthLogin(provider)
}
