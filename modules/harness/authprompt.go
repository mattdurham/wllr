package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
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

// loginMsg opens the auth prompt for the active provider. Emitted by the /login
// command so users can (re)authenticate on demand, not just on first run.
type loginMsg struct{}

// oauthCallbackMsg carries the result of the local OAuth callback server: the
// raw redirect query ("code=…&state=…") and whether a code was actually
// received (ok=false means the login was cancelled/superseded or no server ran).
type oauthCallbackMsg struct {
	Input string
	OK    bool
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
	if method == authMethodAPIKey {
		m.pushNotification(fmt.Sprintf("%s set to use an API key.", provider))
	}
	// OAuth: begin the interactive login flow (start handled by beginOAuthLogin).
}

// beginOAuthLogin starts the OAuth login for a provider: it asks BeginOAuthFn
// for the authorize URL, shows it in a modal, and enters code-capture mode so
// the next submitted line is treated as the pasted authorization code/URL.
func (m *Model) beginOAuthLogin(provider string) tea.Cmd {
	if m.BeginOAuthFn == nil {
		m.pushNotification(fmt.Sprintf("OAuth login is not available for %s.", provider))
		return nil
	}
	authURL, err := m.BeginOAuthFn(provider)
	if err != nil {
		m.pushNotification(fmt.Sprintf("⚠ could not start OAuth login: %v", err))
		return nil
	}
	m.oauthCaptureProvider = provider
	m.modalContent = fmt.Sprintf(
		"Sign in to %s\n\n"+
			"1. Open this URL in a browser (it's been copied to your clipboard):\n\n%s\n\n"+
			"2. Approve access.\n\n"+
			"If the browser is on THIS machine, you're done automatically once it\n"+
			"redirects (the \"localhost\" page failing to load is fine).\n\n"+
			"If the browser is on ANOTHER machine (e.g. SSH), copy the address it\n"+
			"redirects to \u2014 close this box (esc), paste it into the input line, Enter.",
		provider, authURL)
	m.modalScroll = 0
	// Copy the authorize URL to the system clipboard via OSC52 (works over SSH
	// and tmux where the terminal supports it). Harmless if unsupported.
	cmds := []tea.Cmd{tea.SetClipboard(authURL)}
	// If a local callback server is available, race it against the manual paste:
	// on a successful browser redirect the code is captured automatically.
	if m.AwaitOAuthFn != nil {
		await := m.AwaitOAuthFn
		cmds = append(cmds, func() tea.Msg {
			input, ok := await()
			return oauthCallbackMsg{Input: input, OK: ok}
		})
	}
	return tea.Batch(cmds...)
}

// completeOAuthFromCallback completes a login using the code auto-captured by
// the local callback server. No-op if the login already finished (e.g. via a
// manual paste) or was cancelled.
func (m *Model) completeOAuthFromCallback(msg oauthCallbackMsg) tea.Cmd {
	if !msg.OK || m.oauthCaptureProvider == "" {
		return nil
	}
	m.modalContent = ""
	m.modalScroll = 0
	return m.completeOAuthLogin(msg.Input)
}

// completeOAuthLogin exchanges the pasted authorization input for tokens via
// CompleteOAuthFn (which persists and applies them), returning a Cmd that runs
// the exchange off-loop and reports the outcome as a notification.
func (m *Model) completeOAuthLogin(input string) tea.Cmd {
	provider := m.oauthCaptureProvider
	m.oauthCaptureProvider = ""
	fn := m.CompleteOAuthFn
	return func() tea.Msg {
		if fn == nil {
			return NotifyMsg{Text: "OAuth completion is not available."}
		}
		if err := fn(provider, input); err != nil {
			return NotifyMsg{Text: fmt.Sprintf("⚠ OAuth login failed: %v", err)}
		}
		return NotifyMsg{Text: fmt.Sprintf("✓ Logged in to %s.", provider)}
	}
}
