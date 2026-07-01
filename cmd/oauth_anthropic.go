package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Anthropic OAuth (Claude Pro/Max subscription) — authorization code + PKCE.
//
// These constants match the Claude Code / pi client so wllr authenticates the
// same way. The authorize URL carries code=true, which makes Claude display the
// authorization code (in "<code>#<state>" form) on the approval page, enabling
// a paste-back flow with no local callback server — this works over SSH.
//
// Endpoints are package vars (not consts) so tests can point them at httptest.
const anthropicOAuthClientID = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"

var (
	anthropicAuthorizeURL = "https://claude.ai/oauth/authorize"
	anthropicTokenURL     = "https://platform.claude.com/v1/oauth/token"
)

// anthropicOAuthRedirectURI is the redirect_uri sent with the authorize request
// and echoed on token exchange. It must match on both calls. We do not run a
// server on it; the code is pasted back manually.
const anthropicOAuthRedirectURI = "http://localhost:53692/callback"

// anthropicOAuthScopes is the space-separated scope set requested.
const anthropicOAuthScopes = "org:create_api_key user:profile user:inference user:sessions:claude_code user:mcp_servers user:file_upload"

// oauthToken is the result of a token exchange or refresh.
type oauthToken struct {
	Access  string
	Refresh string
	// ExpiresAt is the absolute expiry (unix ms), computed as now + expires_in,
	// minus a 5-minute safety margin so callers refresh before hard expiry.
	ExpiresAt int64
}

// pkcePair is a PKCE verifier and its S256 challenge.
type pkcePair struct {
	Verifier  string
	Challenge string
}

// generatePKCE creates a PKCE verifier (32 random bytes, base64url) and its
// SHA-256 challenge (base64url). Matches the Web Crypto flow pi uses.
func generatePKCE() (pkcePair, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return pkcePair{}, fmt.Errorf("pkce: read random: %w", err)
	}
	verifier := base64.RawURLEncoding.EncodeToString(b[:])
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	return pkcePair{Verifier: verifier, Challenge: challenge}, nil
}

// anthropicAuthorizeURLFor builds the authorize URL the user opens in a browser.
// state is set to the PKCE verifier (matching the client's convention).
func anthropicAuthorizeURLFor(challenge, state string) string {
	q := url.Values{
		"code":                  {"true"},
		"client_id":             {anthropicOAuthClientID},
		"response_type":         {"code"},
		"redirect_uri":          {anthropicOAuthRedirectURI},
		"scope":                 {anthropicOAuthScopes},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"state":                 {state},
	}
	return anthropicAuthorizeURL + "?" + q.Encode()
}

// parseAuthorizationInput extracts the code and state from whatever the user
// pastes: a full redirect URL, a "code#state" string, a "code=…&state=…" query
// fragment, or a bare code.
func parseAuthorizationInput(input string) (code, state string) {
	v := strings.TrimSpace(input)
	if v == "" {
		return "", ""
	}
	if u, err := url.Parse(v); err == nil && (u.Scheme == "http" || u.Scheme == "https") {
		return u.Query().Get("code"), u.Query().Get("state")
	}
	if strings.Contains(v, "#") {
		parts := strings.SplitN(v, "#", 2)
		return parts[0], parts[1]
	}
	if strings.Contains(v, "code=") {
		if q, err := url.ParseQuery(v); err == nil {
			return q.Get("code"), q.Get("state")
		}
	}
	return v, ""
}

// postOAuthJSON POSTs a JSON body to url and returns the parsed token fields.
func postOAuthJSON(ctx context.Context, client *http.Client, endpoint string, body map[string]string) (oauthToken, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return oauthToken{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return oauthToken{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return oauthToken{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return oauthToken{}, fmt.Errorf("oauth request failed: status=%d body=%s", resp.StatusCode, string(data))
	}
	var parsed struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return oauthToken{}, fmt.Errorf("oauth response invalid JSON: %w (body=%s)", err, string(data))
	}
	if parsed.AccessToken == "" {
		return oauthToken{}, fmt.Errorf("oauth response missing access_token (body=%s)", string(data))
	}
	// expires_in seconds → absolute ms, minus a 5-minute safety margin.
	expiresAt := time.Now().Add(time.Duration(parsed.ExpiresIn)*time.Second - 5*time.Minute).UnixMilli()
	return oauthToken{Access: parsed.AccessToken, Refresh: parsed.RefreshToken, ExpiresAt: expiresAt}, nil
}

// exchangeAnthropicCode swaps an authorization code (+ PKCE verifier) for tokens.
func exchangeAnthropicCode(ctx context.Context, client *http.Client, code, state, verifier string) (oauthToken, error) {
	return postOAuthJSON(ctx, client, anthropicTokenURL, map[string]string{
		"grant_type":    "authorization_code",
		"client_id":     anthropicOAuthClientID,
		"code":          code,
		"state":         state,
		"redirect_uri":  anthropicOAuthRedirectURI,
		"code_verifier": verifier,
	})
}

// refreshAnthropicToken exchanges a refresh token for a fresh access token.
func refreshAnthropicToken(ctx context.Context, client *http.Client, refreshToken string) (oauthToken, error) {
	return postOAuthJSON(ctx, client, anthropicTokenURL, map[string]string{
		"grant_type":    "refresh_token",
		"client_id":     anthropicOAuthClientID,
		"refresh_token": refreshToken,
	})
}
