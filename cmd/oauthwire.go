package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	fantasy "charm.land/fantasy"
	"github.com/mattdurham/wllr/modules/agent"
)

// oauthCallbackAddr is the local address the Anthropic OAuth callback server
// listens on. It matches the port in anthropicOAuthRedirectURI. A var (not
// const) so tests can point it at an ephemeral port.
var oauthCallbackAddr = "127.0.0.1:53692"

// oauthLoginState coordinates one interactive OAuth login, driven by the TUI
// (one at a time). Two provider styles share a uniform harness contract
// (begin → await → complete):
//
//   - Anthropic: browser + local callback server. begin starts the server and
//     returns the authorize URL; await blocks on the browser redirect and
//     returns its query; complete exchanges that (with the stored PKCE verifier).
//   - Codex (ChatGPT Plus/Pro): device-code. begin requests a user code and
//     returns it + the verification URL; await polls until the user approves and
//     returns the authorization code + server-issued verifier; complete
//     exchanges those.
//
// begin returns (modalBody, clipboard); await blocks until login resolves and
// returns the material complete needs (encoded per provider), or an error.
type oauthLoginState struct {
	ctx    context.Context
	pool   *agent.AgentPool
	server *http.Server
	codeCh chan string   // buffered(1); callback handler sends the raw redirect query
	done   chan struct{} // closed to cancel a pending await

	// Codex (device-code):
	device   *codexDeviceAuth
	provider string
	model    string

	// Anthropic (callback server):
	verifier  string
	boundAddr string // actual listen address (tests use an ephemeral port)
	mu        sync.Mutex
}

// newOAuthLoginState binds the login coordinator to the context, pool, and model
// used to apply a token once login completes.
func newOAuthLoginState(ctx context.Context, pool *agent.AgentPool, model string) *oauthLoginState {
	return &oauthLoginState{ctx: ctx, pool: pool, model: model}
}

// begin starts an OAuth login for the provider and returns the modal body to
// show plus a string to copy to the clipboard (the URL to open). Dispatches by
// provider; only Anthropic and Codex (openai) are supported.
func (s *oauthLoginState) begin(provider string) (modalBody, clipboard string, err error) {
	switch provider {
	case providerAnthropic:
		return s.beginAnthropic()
	case providerOpenAI:
		return s.beginCodex()
	default:
		return "", "", fmt.Errorf("OAuth login is not supported for provider %q", provider)
	}
}

// await blocks until the pending login resolves. It returns the material
// complete needs (Anthropic: the redirect query; Codex: "code\x00verifier"), or
// an error. An empty input with nil error means the login was cancelled.
func (s *oauthLoginState) await() (string, error) {
	s.mu.Lock()
	provider := s.provider
	s.mu.Unlock()
	switch provider {
	case providerAnthropic:
		return s.awaitAnthropic()
	case providerOpenAI:
		return s.awaitCodex()
	default:
		return "", nil
	}
}

// complete exchanges the awaited material for tokens, persists them (0600 auth
// file), and swaps the live provider + main agent LM to use the new token.
func (s *oauthLoginState) complete(provider, input string) error {
	switch provider {
	case providerAnthropic:
		return s.completeAnthropic(input)
	case providerOpenAI:
		return s.completeCodex(input)
	default:
		return fmt.Errorf("OAuth login is not supported for provider %q", provider)
	}
}

// ─── Anthropic (browser + local callback server) ────────────────────────────

func (s *oauthLoginState) beginAnthropic() (string, string, error) {
	pkce, err := generatePKCE()
	if err != nil {
		return "", "", err
	}
	s.mu.Lock()
	s.stopLocked() // cancel any previous in-flight login
	s.provider = providerAnthropic
	s.verifier = pkce.Verifier
	s.codeCh = make(chan string, 1)
	s.done = make(chan struct{})
	s.startServerLocked()
	s.mu.Unlock()

	authURL := anthropicAuthorizeURLFor(pkce.Challenge, pkce.Verifier)
	body := fmt.Sprintf(
		"Sign in to anthropic\n\n"+
			"1. Open this URL in a browser on THIS machine\n"+
			"   (it's been copied to your clipboard):\n\n%s\n\n"+
			"2. Approve access. You'll be logged in automatically once the browser\n"+
			"   redirects back — this box will close on its own.",
		authURL,
	)
	return body, authURL, nil
}

// startServerLocked starts the localhost callback listener. Best-effort: on bind
// failure it logs and leaves the server unset. Caller holds s.mu.
func (s *oauthLoginState) startServerLocked() {
	ln, err := net.Listen("tcp", oauthCallbackAddr)
	if err != nil {
		slog.Warn(
			"wllr: oauth callback server could not bind; login will time out until the port is free",
			"addr",
			oauthCallbackAddr,
			"error",
			err,
		)
		return
	}
	s.boundAddr = ln.Addr().String()
	ch := s.codeCh
	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if r.URL.Query().Get(oauthParamCode) == "" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(
				w,
				oauthCallbackHTML("Sign-in did not complete. You can close this window and try /login again."),
			)
			return
		}
		_, _ = io.WriteString(w, oauthCallbackHTML("Signed in. You can close this window and return to wllr."))
		select {
		case ch <- r.URL.RawQuery:
		default:
		}
	})
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	s.server = srv
	go func() { _ = srv.Serve(ln) }()
}

func (s *oauthLoginState) awaitAnthropic() (string, error) {
	s.mu.Lock()
	ch, done := s.codeCh, s.done
	s.mu.Unlock()
	if ch == nil {
		return "", nil
	}
	select {
	case q := <-ch:
		return q, nil
	case <-done:
		return "", nil
	}
}

func (s *oauthLoginState) completeAnthropic(input string) error {
	s.mu.Lock()
	verifier := s.verifier
	s.verifier = "" // claim it — a concurrent completion sees ""
	s.mu.Unlock()
	if verifier == "" {
		return errors.New("no OAuth login in progress")
	}

	code, state := parseAuthorizationInput(input)
	if code == "" {
		s.restoreVerifier(verifier)
		return errors.New("no authorization code found")
	}
	if state != "" && state != verifier {
		s.restoreVerifier(verifier)
		return errors.New("OAuth state mismatch")
	}
	if state == "" {
		state = verifier
	}

	tok, err := exchangeAnthropicCode(s.ctx, nil, code, state, verifier)
	if err != nil {
		s.restoreVerifier(verifier)
		return err
	}

	s.mu.Lock()
	s.stopLocked()
	s.mu.Unlock()

	if saveErr := saveAuthCredential(providerAnthropic, authCredential{
		Type:    authTypeOAuth,
		Access:  tok.Access,
		Refresh: tok.Refresh,
		Expires: tok.ExpiresAt,
	}); saveErr != nil {
		return fmt.Errorf("save credential: %w", saveErr)
	}
	return applyAnthropicToken(s.ctx, s.pool, s.model, tok.Access)
}

// ─── Codex (device-code) ─────────────────────────────────────────────────────

func (s *oauthLoginState) beginCodex() (string, string, error) {
	device, err := startCodexDeviceAuth(s.ctx, nil)
	if err != nil {
		return "", "", err
	}
	s.mu.Lock()
	s.stopLocked()
	s.provider = providerOpenAI
	s.device = &device
	s.mu.Unlock()

	body := fmt.Sprintf(
		"Sign in to openai (Codex / ChatGPT Plus·Pro)\n\n"+
			"1. Open this URL in a browser (on any machine — it's been copied to\n"+
			"   your clipboard):\n\n%s\n\n"+
			"2. Enter this code when prompted:\n\n   %s\n\n"+
			"3. Approve access. You'll be logged in automatically once you do —\n"+
			"   this box will close on its own. (Waiting…)",
		device.VerificationURI, device.UserCode,
	)
	return body, device.VerificationURI, nil
}

func (s *oauthLoginState) awaitCodex() (string, error) {
	s.mu.Lock()
	device := s.device
	s.mu.Unlock()
	if device == nil {
		return "", nil
	}
	code, verifier, err := pollCodexDeviceAuth(s.ctx, nil, *device)
	if err != nil {
		return "", err
	}
	return code + "\x00" + verifier, nil
}

func (s *oauthLoginState) completeCodex(input string) error {
	parts := strings.SplitN(input, "\x00", 2)
	if len(parts) != 2 || parts[0] == "" {
		return errors.New("codex login did not produce an authorization code")
	}
	tok, err := exchangeCodexCode(s.ctx, nil, parts[0], parts[1])
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.device = nil
	s.mu.Unlock()

	if saveErr := saveAuthCredential(providerOpenAI, authCredential{
		Type:      authTypeOAuth,
		Access:    tok.Access,
		Refresh:   tok.Refresh,
		Expires:   tok.ExpiresAt,
		AccountID: tok.AccountID,
	}); saveErr != nil {
		return fmt.Errorf("save credential: %w", saveErr)
	}
	return applyCodexToken(s.ctx, s.pool, s.model, tok.Access, tok.AccountID)
}

// ─── shared helpers ──────────────────────────────────────────────────────────

// stopLocked cancels a pending await and shuts the callback server down. Caller
// holds s.mu. Safe to call repeatedly.
func (s *oauthLoginState) stopLocked() {
	if s.done != nil {
		close(s.done)
		s.done = nil
	}
	if s.server != nil {
		_ = s.server.Close()
		s.server = nil
	}
	s.codeCh = nil
	s.boundAddr = ""
	s.device = nil
}

// restoreVerifier puts the verifier back after a failed completion so the user
// can retry without restarting the login.
func (s *oauthLoginState) restoreVerifier(verifier string) {
	s.mu.Lock()
	if s.verifier == "" {
		s.verifier = verifier
	}
	s.mu.Unlock()
}

// oauthCallbackHTML renders the minimal page shown in the browser after the
// redirect (Anthropic callback flow).
func oauthCallbackHTML(msg string) string {
	return "<!doctype html><html><head><meta charset=\"utf-8\"><title>wllr</title></head>" +
		"<body style=\"font-family:system-ui;padding:3rem;text-align:center\"><h2>wllr</h2><p>" +
		msg + "</p></body></html>"
}

// applyAnthropicToken rebuilds the Anthropic provider with the given access
// token and points the pool + main agent at it (no restart).
func applyAnthropicToken(ctx context.Context, pool *agent.AgentPool, model, accessToken string) error {
	if pool == nil {
		return nil
	}
	prov, err := newAnthropicProvider(accessToken)
	if err != nil {
		return fmt.Errorf("rebuild provider: %w", err)
	}
	return applyProviderModel(ctx, pool, prov, model)
}

// applyCodexToken rebuilds the OpenAI provider pointed at the ChatGPT Codex
// backend with the OAuth access token + account id, and points the pool + main
// agent at it (no restart).
func applyCodexToken(ctx context.Context, pool *agent.AgentPool, model, accessToken, accountID string) error {
	if pool == nil {
		return nil
	}
	prov, err := newCodexProvider(accessToken, accountID)
	if err != nil {
		return fmt.Errorf("rebuild provider: %w", err)
	}
	return applyProviderModel(ctx, pool, prov, model)
}

// applyProviderModel swaps the pool's provider and the main agent's LM to model.
func applyProviderModel(ctx context.Context, pool *agent.AgentPool, prov fantasy.Provider, model string) error {
	pool.SetProvider(prov)
	lm, err := pool.LanguageModelForModel(ctx, model)
	if err != nil {
		return fmt.Errorf("get model %q: %w", model, err)
	}
	if main := pool.Get(agent.MainAgentID); main != nil {
		main.SetModel(lm, model)
	}
	return nil
}

// resolveStartupOAuth applies a stored, valid OAuth token at startup (refreshing
// if expired) so a prior /login persists across restarts. Best-effort: logs and
// returns on any error, leaving the env/API-key path in place. Returns true if an
// OAuth token was applied.
func resolveStartupOAuth(ctx context.Context, pool *agent.AgentPool, cfg *Config) bool {
	switch cfg.Provider {
	case providerAnthropic:
		return resolveStartupAnthropicOAuth(ctx, pool, cfg)
	case providerOpenAI:
		return resolveStartupCodexOAuth(ctx, pool, cfg)
	default:
		return false
	}
}

func resolveStartupAnthropicOAuth(ctx context.Context, pool *agent.AgentPool, cfg *Config) bool {
	cred, ok := loadAuthCredential(providerAnthropic)
	if !ok || cred.Type != authTypeOAuth || cred.Access == "" {
		return false
	}
	if cred.isExpired() && cred.Refresh != "" {
		tok, err := refreshAnthropicToken(ctx, nil, cred.Refresh)
		if err != nil {
			slog.Warn("wllr: anthropic oauth refresh failed; falling back to API key", "error", err)
			return false
		}
		cred.Access, cred.Refresh, cred.Expires = tok.Access, tok.Refresh, tok.ExpiresAt
		if saveErr := saveAuthCredential(providerAnthropic, cred); saveErr != nil {
			slog.Warn("wllr: could not persist refreshed oauth token", "error", saveErr)
		}
	}
	if err := applyAnthropicToken(ctx, pool, cfg.Model, cred.Access); err != nil {
		slog.Warn("wllr: could not apply stored oauth token; falling back to API key", "error", err)
		return false
	}
	return true
}

func resolveStartupCodexOAuth(ctx context.Context, pool *agent.AgentPool, cfg *Config) bool {
	cred, ok := loadAuthCredential(providerOpenAI)
	if !ok || cred.Type != authTypeOAuth || cred.Access == "" {
		return false
	}
	if cred.isExpired() && cred.Refresh != "" {
		tok, err := refreshCodexToken(ctx, nil, cred.Refresh)
		if err != nil {
			slog.Warn("wllr: codex oauth refresh failed; falling back to API key", "error", err)
			return false
		}
		cred.Access, cred.Refresh, cred.Expires = tok.Access, tok.Refresh, tok.ExpiresAt
		if id := chatGPTAccountID(tok.Access); id != "" {
			cred.AccountID = id
		}
		if saveErr := saveAuthCredential(providerOpenAI, cred); saveErr != nil {
			slog.Warn("wllr: could not persist refreshed codex token", "error", saveErr)
		}
	}
	if err := applyCodexToken(ctx, pool, cfg.Model, cred.Access, cred.AccountID); err != nil {
		slog.Warn("wllr: could not apply stored codex token; falling back to API key", "error", err)
		return false
	}
	return true
}
