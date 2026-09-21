package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"charm.land/fantasy"
	"github.com/mattdurham/wllr/modules/testutil"
)

func TestSubagentLanguageModelUsesConfiguredLocalEndpoint(t *testing.T) {
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("path = %q, want /v1/chat/completions", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer worker-key" {
			t.Errorf("authorization = %q, want configured worker key", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(
			w,
			`{"id":"chatcmpl-test","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`,
		)
	}))
	defer server.Close()

	cfg := &Config{
		Provider: providerLocal,
		Model:    "main-model",
		LocalModels: []localModelConfig{
			{ID: "main-model", BaseURL: "http://127.0.0.1:1/v1", APIKey: "main-key"},
			{ID: "worker-model", BaseURL: server.URL + "/v1", APIKey: "worker-key"},
		},
	}
	provider := testutil.NewFakeProvider()
	for _, endpoint := range []string{"", server.URL + "/v1"} {
		lm, err := subagentLanguageModel(context.Background(), cfg, provider, "worker-model", endpoint)
		if err != nil {
			t.Fatalf("configured model with endpoint %q: %v", endpoint, err)
		}
		if lm.Model() != "worker-model" {
			t.Fatalf("model = %q, want worker-model", lm.Model())
		}
		if _, err := lm.Generate(context.Background(), fantasy.Call{}); err != nil {
			t.Fatalf("generate via configured endpoint: %v", err)
		}
	}
	if requests != 2 {
		t.Fatalf("requests = %d, want 2", requests)
	}
}

func TestSubagentLanguageModelRejectsUnconfiguredLocalSelection(t *testing.T) {
	cfg := &Config{
		Provider:    providerLocal,
		LocalModels: []localModelConfig{{ID: "worker", BaseURL: "http://127.0.0.1:1/v1"}},
	}
	provider := testutil.NewFakeProvider()
	for _, tc := range []struct{ model, endpoint, want string }{
		{"unknown", "", "not configured"},
		{"worker", "http://127.0.0.1:2/v1", "does not match"},
	} {
		_, err := subagentLanguageModel(context.Background(), cfg, provider, tc.model, tc.endpoint)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("model %q endpoint %q: error = %v, want %q", tc.model, tc.endpoint, err, tc.want)
		}
	}
	cfg.Provider = providerOpenAI
	if _, err := subagentLanguageModel(context.Background(), cfg, provider, "worker", "http://127.0.0.1:1/v1"); err == nil {
		t.Error("non-local provider accepted endpoint")
	}
}
