package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mattdurham/wllr/modules/agent"
)

func TestOAuthLogin_BeginUnsupportedProvider(t *testing.T) {
	s := &oauthLoginState{}
	if _, err := s.beginAnthropicOAuth("openai"); err == nil {
		t.Error("expected error for non-anthropic provider")
	}
}

func TestOAuthLogin_BeginStoresVerifier(t *testing.T) {
	s := &oauthLoginState{}
	u, err := s.beginAnthropicOAuth(providerAnthropic)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if s.verifier == "" {
		t.Error("verifier should be stored after begin")
	}
	if u == "" {
		t.Error("authorize URL should be returned")
	}
}

func TestOAuthLogin_CompleteRequiresInProgress(t *testing.T) {
	s := &oauthLoginState{}
	pool := agent.NewPool()
	err := s.completeAnthropicOAuth(context.Background(), pool, "claude-sonnet-4-6", providerAnthropic, "code#state")
	if err == nil {
		t.Error("expected error when no login in progress")
	}
}

func TestOAuthLogin_CompleteRoundTrip(t *testing.T) {
	withAuthPath(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"access_token":"sk-ant-oat-live","refresh_token":"r1","expires_in":3600}`))
	}))
	defer srv.Close()
	orig := anthropicTokenURL
	anthropicTokenURL = srv.URL
	defer func() { anthropicTokenURL = orig }()

	// Pool with an anthropic provider + a spawned main agent (so SetModel path runs).
	pool := agent.NewPool()
	prov, err := newAnthropicProvider("sk-ant-initial")
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	pool.SetProvider(prov)
	pool.SetDefaultModelName("claude-sonnet-4-6")
	lm, err := pool.LanguageModelForModel(context.Background(), "claude-sonnet-4-6")
	if err != nil {
		t.Fatalf("model: %v", err)
	}
	if _, err := pool.Spawn(agent.MainAgentID, lm, agent.SpawnOpts{TurnTimeout: -1}); err != nil {
		t.Fatalf("spawn: %v", err)
	}

	s := &oauthLoginState{}
	if _, err := s.beginAnthropicOAuth(providerAnthropic); err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := s.completeAnthropicOAuth(context.Background(), pool, "claude-sonnet-4-6", providerAnthropic, "the-code#the-state"); err != nil {
		t.Fatalf("complete: %v", err)
	}

	// Credential persisted with the OAuth token.
	cred, ok := loadAuthCredential(providerAnthropic)
	if !ok || cred.Type != authTypeOAuth || cred.Access != "sk-ant-oat-live" || cred.Refresh != "r1" {
		t.Errorf("stored credential = %+v ok=%v", cred, ok)
	}
	// Verifier cleared after completion.
	if s.verifier != "" {
		t.Error("verifier should be cleared after completion")
	}
}

func TestResolveStartupAnthropicOAuth_NoCredential(t *testing.T) {
	withAuthPath(t)
	pool := agent.NewPool()
	cfg := &Config{Provider: providerAnthropic, Model: "claude-sonnet-4-6"}
	if resolveStartupAnthropicOAuth(context.Background(), pool, cfg) {
		t.Error("should return false when no oauth credential is stored")
	}
}

func TestResolveStartupAnthropicOAuth_AppliesStored(t *testing.T) {
	withAuthPath(t)
	if err := saveAuthCredential(providerAnthropic, authCredential{
		Type:   authTypeOAuth,
		Access: "sk-ant-oat-stored",
		// Far-future expiry so no refresh is attempted.
		Expires: 1<<62 - 1,
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	pool := agent.NewPool()
	prov, _ := newAnthropicProvider("sk-ant-initial")
	pool.SetProvider(prov)
	pool.SetDefaultModelName("claude-sonnet-4-6")
	lm, _ := pool.LanguageModelForModel(context.Background(), "claude-sonnet-4-6")
	_, _ = pool.Spawn(agent.MainAgentID, lm, agent.SpawnOpts{TurnTimeout: -1})

	cfg := &Config{Provider: providerAnthropic, Model: "claude-sonnet-4-6"}
	if !resolveStartupAnthropicOAuth(context.Background(), pool, cfg) {
		t.Error("should return true when a valid oauth credential is applied")
	}
}
