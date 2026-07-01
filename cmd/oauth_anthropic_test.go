package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestGeneratePKCE_ChallengeMatchesVerifier(t *testing.T) {
	p, err := generatePKCE()
	if err != nil {
		t.Fatalf("generatePKCE: %v", err)
	}
	if p.Verifier == "" || p.Challenge == "" {
		t.Fatal("empty verifier or challenge")
	}
	sum := sha256.Sum256([]byte(p.Verifier))
	want := base64.RawURLEncoding.EncodeToString(sum[:])
	if p.Challenge != want {
		t.Errorf("challenge = %q, want %q", p.Challenge, want)
	}
	// base64url: no padding or +/ characters.
	if strings.ContainsAny(p.Verifier+p.Challenge, "+/=") {
		t.Error("verifier/challenge must be base64url (no +/=)")
	}
}

func TestAnthropicAuthorizeURLFor(t *testing.T) {
	u := anthropicAuthorizeURLFor("chal", "state123")
	parsed, err := url.Parse(u)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	q := parsed.Query()
	checks := map[string]string{
		"client_id":             anthropicOAuthClientID,
		"response_type":         "code",
		"redirect_uri":          anthropicOAuthRedirectURI,
		"scope":                 anthropicOAuthScopes,
		"code_challenge":        "chal",
		"code_challenge_method": "S256",
		"state":                 "state123",
		"code":                  "true",
	}
	for k, want := range checks {
		if got := q.Get(k); got != want {
			t.Errorf("query %q = %q, want %q", k, got, want)
		}
	}
	if !strings.HasPrefix(u, anthropicAuthorizeURL+"?") {
		t.Errorf("URL should start with authorize endpoint, got %q", u)
	}
}

func TestParseAuthorizationInput(t *testing.T) {
	cases := []struct {
		name, in, code, state string
	}{
		{"full redirect URL", "http://localhost:53692/callback?code=abc&state=xyz", "abc", "xyz"},
		{"code#state", "abc#xyz", "abc", "xyz"},
		{"query fragment", "code=abc&state=xyz", "abc", "xyz"},
		{"bare code", "just-a-code", "just-a-code", ""},
		{"empty", "  ", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			code, state := parseAuthorizationInput(c.in)
			if code != c.code || state != c.state {
				t.Errorf("parse(%q) = (%q,%q), want (%q,%q)", c.in, code, state, c.code, c.state)
			}
		})
	}
}

func TestExchangeAnthropicCode(t *testing.T) {
	var gotBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"sk-ant-oat-new","refresh_token":"refresh-xyz","expires_in":3600}`))
	}))
	defer srv.Close()

	orig := anthropicTokenURL
	anthropicTokenURL = srv.URL
	defer func() { anthropicTokenURL = orig }()

	tok, err := exchangeAnthropicCode(context.Background(), srv.Client(), "the-code", "the-state", "the-verifier")
	if err != nil {
		t.Fatalf("exchangeAnthropicCode: %v", err)
	}
	if tok.Access != "sk-ant-oat-new" || tok.Refresh != "refresh-xyz" {
		t.Errorf("tokens = %+v", tok)
	}
	if tok.ExpiresAt == 0 {
		t.Error("ExpiresAt should be set")
	}
	// Request body carries the PKCE + grant fields.
	for k, want := range map[string]string{
		"grant_type":    "authorization_code",
		"client_id":     anthropicOAuthClientID,
		"code":          "the-code",
		"code_verifier": "the-verifier",
	} {
		if gotBody[k] != want {
			t.Errorf("request body %q = %q, want %q", k, gotBody[k], want)
		}
	}
}

func TestRefreshAnthropicToken(t *testing.T) {
	var gotBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_, _ = w.Write([]byte(`{"access_token":"sk-ant-oat-refreshed","refresh_token":"refresh-2","expires_in":3600}`))
	}))
	defer srv.Close()

	orig := anthropicTokenURL
	anthropicTokenURL = srv.URL
	defer func() { anthropicTokenURL = orig }()

	tok, err := refreshAnthropicToken(context.Background(), srv.Client(), "old-refresh")
	if err != nil {
		t.Fatalf("refreshAnthropicToken: %v", err)
	}
	if tok.Access != "sk-ant-oat-refreshed" {
		t.Errorf("access = %q", tok.Access)
	}
	if gotBody["grant_type"] != "refresh_token" || gotBody["refresh_token"] != "old-refresh" {
		t.Errorf("request body = %+v", gotBody)
	}
}

func TestExchangeAnthropicCode_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
	}))
	defer srv.Close()

	orig := anthropicTokenURL
	anthropicTokenURL = srv.URL
	defer func() { anthropicTokenURL = orig }()

	if _, err := exchangeAnthropicCode(context.Background(), srv.Client(), "c", "s", "v"); err == nil {
		t.Error("expected error on non-2xx response")
	}
}
