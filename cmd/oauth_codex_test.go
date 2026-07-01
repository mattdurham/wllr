package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	fantasy "charm.land/fantasy"
	fantasyopenapiprovider "charm.land/fantasy/providers/openai"
)

// jwtWithAccountID builds a minimal unsigned JWT whose payload carries the
// ChatGPT account-id claim, so chatGPTAccountID can extract it.
func jwtWithAccountID(id string) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`))
	payload, _ := json.Marshal(map[string]any{
		"https://api.openai.com/auth": map[string]string{"chatgpt_account_id": id},
	})
	return header + "." + base64.RawURLEncoding.EncodeToString(payload) + ".sig"
}

// swapCodexURLs points the Codex endpoints at test servers and returns a
// restore func.
func swapCodexURLs(userCode, tokenPoll, exchange string) func() {
	ou, ot, oe := codexDeviceUserCodeURL, codexDeviceTokenURL, codexTokenURL
	codexDeviceUserCodeURL = userCode
	codexDeviceTokenURL = tokenPoll
	codexTokenURL = exchange
	return func() {
		codexDeviceUserCodeURL, codexDeviceTokenURL, codexTokenURL = ou, ot, oe
	}
}

// fantasyNewOpenAIForTest builds a plain OpenAI provider for tests that only
// need the pool/model plumbing (no live calls).
func fantasyNewOpenAIForTest() (fantasy.Provider, error) {
	return fantasyopenapiprovider.New(fantasyopenapiprovider.WithAPIKey("test-key"))
}

func TestChatGPTAccountID(t *testing.T) {
	if got := chatGPTAccountID(jwtWithAccountID("acct-42")); got != "acct-42" {
		t.Errorf("chatGPTAccountID = %q, want acct-42", got)
	}
	if got := chatGPTAccountID("not-a-jwt"); got != "" {
		t.Errorf("chatGPTAccountID(non-jwt) = %q, want empty", got)
	}
}

func TestParseFlexibleInt(t *testing.T) {
	cases := map[string]int{`5`: 5, `"7"`: 7, `" 3 "`: 3, `"x"`: 0, ``: 0}
	for in, want := range cases {
		if got := parseFlexibleInt(json.RawMessage(in)); got != want {
			t.Errorf("parseFlexibleInt(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestExchangeCodexCode_ExtractsAccountID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"access_token":"` + jwtWithAccountID("acct-7") + `","refresh_token":"r","expires_in":3600}`))
	}))
	defer srv.Close()
	restore := swapCodexURLs("", "", srv.URL)
	defer restore()

	tok, err := exchangeCodexCode(context.Background(), srv.Client(), "code", "verifier")
	if err != nil {
		t.Fatalf("exchangeCodexCode: %v", err)
	}
	if tok.AccountID != "acct-7" {
		t.Errorf("AccountID = %q, want acct-7", tok.AccountID)
	}
	if tok.Refresh != "r" || tok.ExpiresAt == 0 {
		t.Errorf("unexpected token %+v", tok)
	}
}

func TestExchangeCodexCode_MissingAccountID(t *testing.T) {
	// A token with no account-id claim must be rejected.
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"x"}`))
	noAcct := header + "." + payload + ".sig"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"access_token":"` + noAcct + `","refresh_token":"r","expires_in":3600}`))
	}))
	defer srv.Close()
	restore := swapCodexURLs("", "", srv.URL)
	defer restore()

	if _, err := exchangeCodexCode(context.Background(), srv.Client(), "code", "verifier"); err == nil {
		t.Error("expected error when the token lacks a chatgpt_account_id claim")
	}
}
