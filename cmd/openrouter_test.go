package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFetchOpenRouterModels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/models" || r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("request path=%q auth=%q", r.URL.Path, r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(
			w,
			`{"data":[{"id":"anthropic/claude-sonnet-4.6","name":"Claude Sonnet","context_length":200000},{"id":"openai/gpt","context_length":128000},{"name":"missing id"}]}`,
		)
	}))
	defer server.Close()
	models, err := fetchOpenRouterModels(context.Background(), server.Client(), server.URL+"/api/v1/models", "test-key")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(models) != 2 || models[0].ID != "anthropic/claude-sonnet-4.6" || models[0].ContextWindow != 200000 ||
		models[1].Name != "openai/gpt" {
		t.Fatalf("models = %+v", models)
	}
}

func TestFetchOpenRouterModelsRejectsHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()
	_, err := fetchOpenRouterModels(context.Background(), server.Client(), server.URL, "bad-key")
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("error = %v, want HTTP 401", err)
	}
}

func TestOpenRouterModelAndKeyPersist(t *testing.T) {
	withConfigPath(t)
	if err := saveAuthCredential(providerOpenRouter, authCredential{Type: authTypeAPIKey, Key: "saved-key"}); err != nil {
		t.Fatalf("save key: %v", err)
	}
	cfg := &Config{}
	for _, model := range []openRouterModelConfig{
		{ID: "one/model", Name: "One", ContextWindow: 100000},
		{ID: "two/model", Name: "Two", ContextWindow: 200000},
	} {
		if err := cfg.pinOpenRouterModel(model); err != nil {
			t.Fatalf("pin: %v", err)
		}
	}
	if err := saveProvider(providerOpenRouter); err != nil {
		t.Fatalf("save provider: %v", err)
	}
	if err := saveModel("two/model"); err != nil {
		t.Fatalf("save model: %v", err)
	}
	t.Setenv("OPENROUTER_API_KEY", "")
	loaded, err := LoadConfig()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if loaded.Provider != providerOpenRouter || loaded.Model != "two/model" || loaded.OpenRouterAPIKey != "saved-key" {
		t.Fatalf("loaded selection provider=%q model=%q key=%q", loaded.Provider, loaded.Model, loaded.OpenRouterAPIKey)
	}
	if len(loaded.OpenRouterModels) != 2 || loaded.OpenRouterModels[0].ID != "two/model" {
		t.Fatalf("pinned models = %+v", loaded.OpenRouterModels)
	}
	if got := contextWindowForSelection(providerOpenRouter, "two/model", loaded); got != 200000 {
		t.Fatalf("context window = %d", got)
	}
	if err := saveProvider(providerAnthropic); err != nil {
		t.Fatalf("save other provider: %v", err)
	}
	other, err := LoadConfig()
	if err != nil || other.OpenRouterAPIKey != "saved-key" {
		t.Fatalf("OpenRouter key lost after switching providers: key=%q err=%v", other.OpenRouterAPIKey, err)
	}
}

func TestBuildOpenRouterProvider(t *testing.T) {
	provider, lm, err := buildProvider(context.Background(), &Config{
		Provider: providerOpenRouter, Model: "openai/gpt-4o", OpenRouterAPIKey: "test-key",
	})
	if err != nil {
		t.Fatalf("build provider: %v", err)
	}
	if provider.Name() != providerOpenRouter || lm.Provider() != providerOpenRouter || lm.Model() != "openai/gpt-4o" {
		t.Fatalf("provider=%q language model provider=%q model=%q", provider.Name(), lm.Provider(), lm.Model())
	}
}
