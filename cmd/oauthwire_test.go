package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mattdurham/wllr/modules/agent"
)

// useEphemeralCallbackPort points the callback server at an OS-assigned port for
// the duration of a test, so tests never fight over the fixed 53692 and never
// leak a listener across tests.
func useEphemeralCallbackPort(t *testing.T) {
	t.Helper()
	orig := oauthCallbackAddr
	oauthCallbackAddr = "127.0.0.1:0"
	t.Cleanup(func() { oauthCallbackAddr = orig })
}

func newTestOAuthState(t *testing.T, pool *agent.AgentPool) *oauthLoginState {
	t.Helper()
	s := newOAuthLoginState(context.Background(), pool, "claude-sonnet-4-6")
	t.Cleanup(func() { s.mu.Lock(); s.stopLocked(); s.mu.Unlock() })
	return s
}

func TestOAuthLogin_BeginUnsupportedProvider(t *testing.T) {
	s := newTestOAuthState(t, agent.NewPool())
	if _, _, err := s.begin("gemini"); err == nil {
		t.Error("expected error for unsupported provider")
	}
}

func TestOAuthLogin_BeginAnthropicStoresVerifier(t *testing.T) {
	useEphemeralCallbackPort(t)
	s := newTestOAuthState(t, agent.NewPool())
	body, clip, err := s.begin(providerAnthropic)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if s.verifier == "" {
		t.Error("verifier should be stored after begin")
	}
	if body == "" || clip == "" {
		t.Errorf("begin should return modal body + clipboard URL; got body=%q clip=%q", body, clip)
	}
}

func TestOAuthLogin_CompleteAnthropicRequiresInProgress(t *testing.T) {
	s := newTestOAuthState(t, agent.NewPool())
	if err := s.complete(providerAnthropic, "code#state"); err == nil {
		t.Error("expected error when no login in progress")
	}
}

func TestOAuthLogin_CompleteAnthropicRoundTrip(t *testing.T) {
	withAuthPath(t)
	useEphemeralCallbackPort(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"access_token":"sk-ant-oat-live","refresh_token":"r1","expires_in":3600}`))
	}))
	defer srv.Close()
	orig := anthropicTokenURL
	anthropicTokenURL = srv.URL
	defer func() { anthropicTokenURL = orig }()

	pool := agent.NewPool()
	prov, err := newAnthropicProvider("sk-ant-initial")
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	pool.SetProvider(prov)
	pool.SetDefaultModelName("claude-sonnet-4-6")
	lm, _ := pool.LanguageModelForModel(context.Background(), "claude-sonnet-4-6")
	_, _ = pool.Spawn(agent.MainAgentID, lm, agent.SpawnOpts{TurnTimeout: -1})

	s := newTestOAuthState(t, pool)
	if _, _, err := s.begin(providerAnthropic); err != nil {
		t.Fatalf("begin: %v", err)
	}
	input := "the-code#" + s.verifier
	if err := s.complete(providerAnthropic, input); err != nil {
		t.Fatalf("complete: %v", err)
	}

	cred, ok := loadAuthCredential(providerAnthropic)
	if !ok || cred.Type != authTypeOAuth || cred.Access != "sk-ant-oat-live" || cred.Refresh != "r1" {
		t.Errorf("stored credential = %+v ok=%v", cred, ok)
	}
	if s.verifier != "" {
		t.Error("verifier should be cleared after completion")
	}
}

func TestOAuthLogin_CallbackServerCapturesCode(t *testing.T) {
	useEphemeralCallbackPort(t)
	s := newTestOAuthState(t, agent.NewPool())
	if _, _, err := s.begin(providerAnthropic); err != nil {
		t.Fatalf("begin: %v", err)
	}

	s.mu.Lock()
	addr := s.boundAddr
	s.mu.Unlock()
	if addr == "" {
		t.Fatal("callback server did not bind")
	}

	type result struct {
		input string
		err   error
	}
	res := make(chan result, 1)
	go func() {
		in, err := s.await()
		res <- result{in, err}
	}()

	url := "http://" + addr + "/callback?code=the-code&state=" + s.verifier
	resp, err := http.Get(url) //nolint:noctx // test
	if err != nil {
		t.Fatalf("callback GET: %v", err)
	}
	_ = resp.Body.Close()

	select {
	case r := <-res:
		if r.err != nil {
			t.Fatalf("await error: %v", r.err)
		}
		code, state := parseAuthorizationInput(r.input)
		if code != "the-code" || state != s.verifier {
			t.Errorf("captured (code,state)=(%q,%q)", code, state)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for callback")
	}
}

func TestOAuthLogin_AnthropicStateMismatchRejected(t *testing.T) {
	useEphemeralCallbackPort(t)
	s := newTestOAuthState(t, agent.NewPool())
	if _, _, err := s.begin(providerAnthropic); err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := s.complete(providerAnthropic, "the-code#wrong-state"); err == nil {
		t.Fatal("expected state-mismatch error")
	}
	if s.verifier == "" {
		t.Error("verifier should be restored after a failed completion")
	}
}

// ─── Codex device-code ───────────────────────────────────────────────────────

func TestOAuthLogin_CodexDeviceFlow(t *testing.T) {
	withAuthPath(t)

	// Device user-code endpoint.
	userCodeSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"device_auth_id":"dev-1","user_code":"WLLR-1234","interval":1}`))
	}))
	defer userCodeSrv.Close()
	// Device token endpoint: one pending, then complete.
	var polls int
	tokenPollSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		polls++
		if polls < 2 {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		_, _ = w.Write([]byte(`{"authorization_code":"auth-code","code_verifier":"the-verifier"}`))
	}))
	defer tokenPollSrv.Close()
	// Final code→token exchange returns a JWT carrying the account id.
	exchangeSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"access_token":"` + jwtWithAccountID("acct-9") + `","refresh_token":"r2","expires_in":3600}`))
	}))
	defer exchangeSrv.Close()

	restore := swapCodexURLs(userCodeSrv.URL, tokenPollSrv.URL, exchangeSrv.URL)
	defer restore()

	pool := agent.NewPool()
	prov, _ := fantasyNewOpenAIForTest()
	pool.SetProvider(prov)
	pool.SetDefaultModelName("gpt-5.2-codex")
	lm, _ := pool.LanguageModelForModel(context.Background(), "gpt-5.2-codex")
	_, _ = pool.Spawn(agent.MainAgentID, lm, agent.SpawnOpts{TurnTimeout: -1})

	s := newOAuthLoginState(context.Background(), pool, "gpt-5.2-codex")
	body, _, err := s.begin("openai")
	if err != nil {
		t.Fatalf("begin codex: %v", err)
	}
	if body == "" {
		t.Error("begin should return a modal body with the user code")
	}
	input, err := s.await()
	if err != nil {
		t.Fatalf("await codex: %v", err)
	}
	if err := s.complete("openai", input); err != nil {
		t.Fatalf("complete codex: %v", err)
	}

	cred, ok := loadAuthCredential("openai")
	if !ok || cred.Type != authTypeOAuth || cred.Access == "" || cred.AccountID != "acct-9" {
		t.Errorf("stored codex credential = %+v ok=%v", cred, ok)
	}
}

func TestResolveStartupOAuth_NoCredential(t *testing.T) {
	withAuthPath(t)
	pool := agent.NewPool()
	if resolveStartupOAuth(context.Background(), pool, &Config{Provider: providerAnthropic, Model: "claude-sonnet-4-6"}) {
		t.Error("anthropic: should be false with no credential")
	}
	if resolveStartupOAuth(context.Background(), pool, &Config{Provider: "openai", Model: "gpt-5.2-codex"}) {
		t.Error("openai: should be false with no credential")
	}
}

func TestResolveStartupOAuth_AppliesStoredAnthropic(t *testing.T) {
	withAuthPath(t)
	if err := saveAuthCredential(providerAnthropic, authCredential{
		Type: authTypeOAuth, Access: "sk-ant-oat-stored", Expires: 1<<62 - 1,
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	pool := agent.NewPool()
	prov, _ := newAnthropicProvider("sk-ant-initial")
	pool.SetProvider(prov)
	pool.SetDefaultModelName("claude-sonnet-4-6")
	lm, _ := pool.LanguageModelForModel(context.Background(), "claude-sonnet-4-6")
	_, _ = pool.Spawn(agent.MainAgentID, lm, agent.SpawnOpts{TurnTimeout: -1})

	if !resolveStartupOAuth(context.Background(), pool, &Config{Provider: providerAnthropic, Model: "claude-sonnet-4-6"}) {
		t.Error("should apply a stored anthropic oauth token")
	}
}
