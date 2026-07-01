package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"fmt"

	"github.com/mattdurham/wllr/modules/sdk"
)

// showAuthPromptMsg opens the first-run auth-method prompt for a provider.
// Emitted at startup (from Init) when the active provider has no recorded auth
// choice yet.
type showAuthPromptMsg struct {
	Provider string
}

// recordAuthMsg records the user's chosen auth method for a provider. Emitted by
// the auth prompt picker on selection.
type recordAuthMsg struct {
	Provider string
	Method   string
}

// authPickerCallback is the reserved PickerView.Callback value that routes an
// auth-prompt selection to the core recordAuthMsg handler instead of dispatching
// EventOnCommand to a WASM extension. "__wllr:" is reserved for core pickers.
const authPickerCallback = "__wllr:auth"

// Auth method IDs presented in the prompt.
const (
	authMethodOAuth  = "oauth"
	authMethodAPIKey = "api_key"
)

// openAuthPrompt opens a two-item picker asking how the user wants to
// authenticate the given provider. The choice is recorded once (see
// RecordAuthFn) so the prompt is not shown again for that provider.
func (m *Model) openAuthPrompt(provider string) {
	m.authPromptProvider = provider
	items := []sdk.ShowPickerItem{
		{ID: authMethodOAuth, Label: "Set up OAuth / login", Sublabel: "sign in via the provider"},
		{ID: authMethodAPIKey, Label: "Use an API key", Sublabel: "from the environment or auth file"},
	}
	m.picker.Open(fmt.Sprintf("Authenticate %s  (↑↓ · enter · esc)", provider), items, authPickerCallback)
	m.picker.SetSize(m.width, m.chatHeight())
}

// applyAuthChoice records the chosen auth method for the pending provider via
// RecordAuthFn, then notifies. Errors are surfaced as notifications.
func (m *Model) applyAuthChoice(method string) {
	provider := m.authPromptProvider
	m.authPromptProvider = ""
	if provider == "" || method == "" {
		return
	}
	if m.RecordAuthFn != nil {
		if err := m.RecordAuthFn(provider, method); err != nil {
			m.pushNotification(fmt.Sprintf("⚠ could not record auth choice: %v", err))
			return
		}
	}
	switch method {
	case authMethodOAuth:
		m.pushNotification(fmt.Sprintf("%s set to use OAuth. Provide an sk-ant-oat… token (ANTHROPIC_API_KEY) or the forthcoming login flow.", provider))
	default:
		m.pushNotification(fmt.Sprintf("%s set to use an API key.", provider))
	}
}
