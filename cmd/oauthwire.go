package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/mattdurham/wllr/modules/agent"
)

// oauthCallbackAddr is the local address the OAuth callback server listens on.
// It matches the port in anthropicOAuthRedirectURI. A var (not const) so tests
// can point it at an ephemeral port.
var oauthCallbackAddr = "127.0.0.1:53692"

// oauthLoginState coordinates one interactive OAuth login. It holds the PKCE
// verifier between begin (which generates it + starts the local callback
// server) and complete (which exchanges the code). Only one login runs at a
// time, driven by the TUI. The local callback server auto-captures the code
// when the browser redirect succeeds (local runs); manual paste is the fallback
// for SSH/remote where localhost can't reach the user's machine.
type oauthLoginState struct {
	mu        sync.Mutex
	verifier  string
	server    *http.Server
	boundAddr string        // actual listen address (for tests using an ephemeral port)
	codeCh    chan string   // buffered(1); the callback handler sends the raw redirect query
	done      chan struct{} // closed to cancel a pending awaitCallback
}

// beginAnthropicOAuth generates PKCE, starts the local callback server, stores
// the verifier, and returns the authorize URL for the user to open. Only
// Anthropic is supported today. If the callback server cannot bind (port in use
// or a remote/SSH session), login falls back to manual paste.
func (s *oauthLoginState) beginAnthropicOAuth(provider string) (string, error) {
	if provider != providerAnthropic {
		return "", fmt.Errorf("OAuth login is not supported for provider %q", provider)
	}
	pkce, err := generatePKCE()
	if err != nil {
		return "", err
	}

	s.mu.Lock()
	s.stopLocked() // cancel any previous in-flight login
	s.verifier = pkce.Verifier
	s.codeCh = make(chan string, 1)
	s.done = make(chan struct{})
	s.startServerLocked()
	s.mu.Unlock()

	// state is set to the verifier, matching the client convention.
	return anthropicAuthorizeURLFor(pkce.Challenge, pkce.Verifier), nil
}

// startServerLocked starts the localhost callback listener. Best-effort: on bind
// failure it logs and leaves the server unset (paste-only). Caller holds s.mu.
func (s *oauthLoginState) startServerLocked() {
	ln, err := net.Listen("tcp", oauthCallbackAddr)
	if err != nil {
		slog.Info("wllr: oauth callback server not started; using paste flow", "error", err)
		return
	}
	s.boundAddr = ln.Addr().String()
	ch := s.codeCh
	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if r.URL.Query().Get("code") == "" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, oauthCallbackHTML("Sign-in did not complete. You can close this window and try /login again."))
			return
		}
		_, _ = io.WriteString(w, oauthCallbackHTML("Signed in. You can close this window and return to wllr."))
		// Non-blocking send: buffered(1), and select-default so a duplicate hit
		// (or a hit after completion) never blocks the handler.
		select {
		case ch <- r.URL.RawQuery:
		default:
		}
	})
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	s.server = srv
	go func() { _ = srv.Serve(ln) }()
}

// stopLocked cancels a pending awaitCallback and shuts the server down. Caller
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
}

// awaitCallback blocks until the browser redirect delivers a code (returning the
// raw redirect query and true) or the login is cancelled/superseded (false).
// The harness runs this off-loop; a successful result completes the login on the
// same path as a manual paste.
func (s *oauthLoginState) awaitCallback() (string, bool) {
	s.mu.Lock()
	ch, done := s.codeCh, s.done
	s.mu.Unlock()
	if ch == nil {
		return "", false
	}
	select {
	case q := <-ch:
		return q, true
	case <-done:
		return "", false
	}
}

// completeAnthropicOAuth exchanges the code (from a paste or the callback) for
// tokens, persists them to the auth file, and swaps the live provider + main
// agent LM. The verifier is claimed under the lock so a paste and a callback
// carrying the same code cannot both run the (single-use) exchange; on failure
// the verifier is restored so the user can retry.
func (s *oauthLoginState) completeAnthropicOAuth(ctx context.Context, pool *agent.AgentPool, model, provider, input string) error {
	if provider != providerAnthropic {
		return fmt.Errorf("OAuth login is not supported for provider %q", provider)
	}

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
		return errors.New("no authorization code found in input")
	}
	if state != "" && state != verifier {
		s.restoreVerifier(verifier)
		return errors.New("OAuth state mismatch")
	}
	if state == "" {
		state = verifier
	}

	tok, err := exchangeAnthropicCode(ctx, nil, code, state, verifier)
	if err != nil {
		s.restoreVerifier(verifier)
		return err
	}

	// Success: tear down the callback server and cancel any pending awaiter.
	s.mu.Lock()
	s.stopLocked()
	s.mu.Unlock()

	if saveErr := saveAuthCredential(provider, authCredential{
		Type:    authTypeOAuth,
		Access:  tok.Access,
		Refresh: tok.Refresh,
		Expires: tok.ExpiresAt,
	}); saveErr != nil {
		return fmt.Errorf("save credential: %w", saveErr)
	}
	return applyAnthropicToken(ctx, pool, model, tok.Access)
}

// restoreVerifier puts the verifier back after a failed completion so the user
// can retry (e.g. re-paste) without restarting the login.
func (s *oauthLoginState) restoreVerifier(verifier string) {
	s.mu.Lock()
	if s.verifier == "" {
		s.verifier = verifier
	}
	s.mu.Unlock()
}

// oauthCallbackHTML renders the minimal page shown in the browser after the
// redirect.
func oauthCallbackHTML(msg string) string {
	return "<!doctype html><html><head><meta charset=\"utf-8\"><title>wllr</title></head>" +
		"<body style=\"font-family:system-ui;padding:3rem;text-align:center\"><h2>wllr</h2><p>" +
		msg + "</p></body></html>"
}

// applyAnthropicToken rebuilds the Anthropic provider with the given access
// token and points the pool + main agent at it, so subsequent turns use the
// OAuth credential without a restart.
func applyAnthropicToken(ctx context.Context, pool *agent.AgentPool, model, accessToken string) error {
	prov, err := newAnthropicProvider(accessToken)
	if err != nil {
		return fmt.Errorf("rebuild provider: %w", err)
	}
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

// resolveStartupAnthropicOAuth applies a stored, valid Anthropic OAuth token at
// startup (refreshing it first if expired) so a prior /login persists across
// restarts. Best-effort: logs and returns on any error, leaving the
// env/API-key path in place. Returns true if an OAuth token was applied.
func resolveStartupAnthropicOAuth(ctx context.Context, pool *agent.AgentPool, cfg *Config) bool {
	if cfg.Provider != providerAnthropic {
		return false
	}
	cred, ok := loadAuthCredential(providerAnthropic)
	if !ok || cred.Type != authTypeOAuth || cred.Access == "" {
		return false
	}
	// Refresh if expired and a refresh token is available.
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
