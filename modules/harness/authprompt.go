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

// startLoginMsg begins OAuth login for a provider immediately. It is emitted at
// startup for blank first-run config so the login modal opens without an
// intermediate auth-method picker.
type startLoginMsg struct {
	Provider string
}

// showLoginProviderPickerMsg opens a startup provider picker before OAuth. It
// is used when neither provider nor model has been configured.
type showLoginProviderPickerMsg struct{}

// loginProviderSelectedMsg carries the provider chosen from the startup login
// picker.
type loginProviderSelectedMsg struct {
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
// received (ok=false means the login was cancelled/superseded).
type oauthCallbackMsg struct {
	Input string
	OK    bool
}

// authPickerCallback is the reserved PickerView.Callback value that routes an
// auth-prompt selection to the core recordAuthMsg handler instead of dispatching
// EventOnCommand to a WASM extension. "__wllr:" is reserved for core pickers.
const authPickerCallback = "__wllr:auth"

const loginProviderPickerCallback = "__wllr:login_provider"

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
// for the authorize URL (which also starts the local callback server), shows it
// in a modal, and waits for the browser redirect to be auto-captured by the
// callback server. There is no manual-paste fallback — login requires the
// browser to be able to reach the local callback server.
func (m *Model) beginOAuthLogin(provider string) tea.Cmd {
	if m.BeginOAuthFn == nil || m.AwaitOAuthFn == nil {
		m.pushNotification(fmt.Sprintf("OAuth login is not available for %s.", provider))
		return nil
	}
	modalBody, clipboard, err := m.BeginOAuthFn(provider)
	if err != nil {
		m.pushNotification(fmt.Sprintf("⚠ could not start OAuth login: %v", err))
		return nil
	}
	m.oauthCaptureProvider = provider
	m.modalContent = modalBody
	m.modalScroll = 0
	// Copy the URL to the system clipboard via OSC52 (works over SSH/tmux where
	// the terminal supports it; harmless otherwise). AwaitOAuthFn blocks until
	// login completes: the local callback server (Anthropic) or device-code poll
	// (Codex) captures approval and yields the result.
	await := m.AwaitOAuthFn
	cmds := []tea.Cmd{func() tea.Msg {
		input, ok := await()
		return oauthCallbackMsg{Input: input, OK: ok}
	}}
	if clipboard != "" {
		cmds = append(cmds, tea.SetClipboard(clipboard))
	}
	return tea.Batch(cmds...)
}

// completeOAuthFromCallback completes a login using the code auto-captured by
// the local callback server, exchanging it via CompleteOAuthFn (which persists
// and applies the tokens). No-op if the login was cancelled/superseded.
func (m *Model) completeOAuthFromCallback(msg oauthCallbackMsg) tea.Cmd {
	if !msg.OK || m.oauthCaptureProvider == "" {
		m.oauthCaptureProvider = ""
		return nil
	}
	provider := m.oauthCaptureProvider
	m.oauthCaptureProvider = ""
	m.modalContent = ""
	m.modalScroll = 0
	fn := m.CompleteOAuthFn
	input := msg.Input
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
