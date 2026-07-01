package main

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/mattdurham/wllr/modules/agent"
)

// oauthLoginState holds the in-flight PKCE verifier between beginOAuth (which
// generates it and returns the authorize URL) and completeOAuth (which needs it
// for the token exchange). Only one login runs at a time (driven by the TUI),
// so a single value is sufficient.
type oauthLoginState struct {
	verifier string
}

// beginAnthropicOAuth generates PKCE, stores the verifier, and returns the
// authorize URL for the user to open. Only Anthropic is supported today.
func (s *oauthLoginState) beginAnthropicOAuth(provider string) (string, error) {
	if provider != providerAnthropic {
		return "", fmt.Errorf("OAuth login is not supported for provider %q", provider)
	}
	pkce, err := generatePKCE()
	if err != nil {
		return "", err
	}
	s.verifier = pkce.Verifier
	// state is set to the verifier, matching the client convention.
	return anthropicAuthorizeURLFor(pkce.Challenge, pkce.Verifier), nil
}

// completeAnthropicOAuth exchanges the pasted code for tokens, persists them to
// the auth file, and swaps the live provider + main agent LM to use the new
// access token. Only Anthropic is supported today.
func (s *oauthLoginState) completeAnthropicOAuth(ctx context.Context, pool *agent.AgentPool, model string, provider, input string) error {
	if provider != providerAnthropic {
		return fmt.Errorf("OAuth login is not supported for provider %q", provider)
	}
	if s.verifier == "" {
		return fmt.Errorf("no OAuth login in progress")
	}
	code, state := parseAuthorizationInput(input)
	if code == "" {
		return fmt.Errorf("no authorization code found in input")
	}
	if state == "" {
		state = s.verifier
	}
	tok, err := exchangeAnthropicCode(ctx, nil, code, state, s.verifier)
	if err != nil {
		return err
	}
	s.verifier = ""

	// Persist the OAuth credential (0600 auth file).
	if saveErr := saveAuthCredential(provider, authCredential{
		Type:    authTypeOAuth,
		Access:  tok.Access,
		Refresh: tok.Refresh,
		Expires: tok.ExpiresAt,
	}); saveErr != nil {
		return fmt.Errorf("save credential: %w", saveErr)
	}

	// Swap the live provider + main agent LM to use the new access token.
	return applyAnthropicToken(ctx, pool, model, tok.Access)
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
