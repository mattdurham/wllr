package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"charm.land/fantasy"
	"github.com/mattdurham/wllr/modules/agent"
	"github.com/mattdurham/wllr/modules/extension"
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
		lm, _, err := subagentLanguageModelWithModelFetcher(
			context.Background(), cfg, provider, "worker-model", endpoint, nil,
		)
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
		_, _, err := subagentLanguageModelWithModelFetcher(
			context.Background(), cfg, provider, tc.model, tc.endpoint, nil,
		)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("model %q endpoint %q: error = %v, want %q", tc.model, tc.endpoint, err, tc.want)
		}
	}
	cfg.Provider = providerOpenAI
	if _, _, err := subagentLanguageModelWithModelFetcher(
		context.Background(), cfg, provider, "worker", "http://127.0.0.1:1/v1", nil,
	); err == nil {
		t.Error("non-local provider accepted endpoint")
	}
}

type subagentRecordingProvider struct {
	lm        fantasy.LanguageModel
	requested []string
}

func (p *subagentRecordingProvider) Name() string { return "recording" }

func (p *subagentRecordingProvider) LanguageModel(
	_ context.Context,
	model string,
) (fantasy.LanguageModel, error) {
	p.requested = append(p.requested, model)
	return p.lm, nil
}

func newSubagentRecordingProvider() *subagentRecordingProvider {
	return &subagentRecordingProvider{lm: testutil.NewFakeProvider().LM()}
}

func TestSubagentLanguageModelRejectsUnavailableOpenRouterModel(t *testing.T) {
	cfg := &Config{Provider: providerOpenRouter, OpenRouterAPIKey: "test-key"}
	provider := newSubagentRecordingProvider()
	fetchModels := func(
		_ context.Context,
		_ *http.Client,
		endpoint, key string,
	) ([]openRouterModelConfig, error) {
		if endpoint != openRouterModelsURL || key != "test-key" {
			t.Errorf("catalog request endpoint=%q key=%q", endpoint, key)
		}
		return []openRouterModelConfig{{ID: "deepseek/deepseek-chat", ContextWindow: 128000}}, nil
	}

	pool := agent.NewPool()
	pool.SetProvider(provider)
	pool.SetModelFactory(newSubagentModelFactory(cfg, pool, fetchModels))
	spawner := agent.NewSpawner(pool, nil, nil)
	err := spawner.Spawn(context.Background(), extension.SpawnRequest{
		ID:        "main/worker",
		Name:      "worker",
		ModelName: "deepseek-flash",
	})
	if err == nil || !strings.Contains(
		err.Error(),
		`OpenRouter model "deepseek-flash" is not available to this account`,
	) {
		t.Fatalf("error = %v, want unavailable-model error", err)
	}
	if got := pool.Get("main/worker"); got != nil {
		t.Fatalf("invalid model registered agent %q", got.ID())
	}
	if len(provider.requested) != 0 {
		t.Fatalf("provider constructed models %v after validation failed", provider.requested)
	}
}

func TestSubagentLanguageModelAcceptsAvailableOpenRouterModel(t *testing.T) {
	cfg := &Config{Provider: providerOpenRouter, OpenRouterAPIKey: "test-key"}
	provider := newSubagentRecordingProvider()
	fetchModels := func(
		_ context.Context,
		_ *http.Client,
		_, _ string,
	) ([]openRouterModelConfig, error) {
		return []openRouterModelConfig{{ID: "deepseek/deepseek-chat", ContextWindow: 128000}}, nil
	}

	pool := agent.NewPool()
	pool.SetProvider(provider)
	pool.SetModelFactory(newSubagentModelFactory(cfg, pool, fetchModels))
	spawner := agent.NewSpawner(pool, nil, nil)
	err := spawner.Spawn(context.Background(), extension.SpawnRequest{
		ID:        "main/worker",
		Name:      "worker",
		ModelName: "deepseek/deepseek-chat",
	})
	if err != nil {
		t.Fatalf("available model: %v", err)
	}
	if got := pool.Get("main/worker"); got == nil {
		t.Fatal("available model did not register its agent")
	}
	if window := pool.ContextWindowForModel("deepseek/deepseek-chat"); window != 128000 {
		t.Fatalf("context window = %d, want catalog window 128000", window)
	}
	if len(provider.requested) != 1 || provider.requested[0] != "deepseek/deepseek-chat" {
		t.Fatalf("provider requests = %v, want the validated model", provider.requested)
	}
}

func TestSubagentLanguageModelReportsOpenRouterCatalogFailure(t *testing.T) {
	cfg := &Config{Provider: providerOpenRouter, OpenRouterAPIKey: "test-key"}
	provider := newSubagentRecordingProvider()
	catalogErr := errors.New("catalog offline")
	fetchModels := func(
		_ context.Context,
		_ *http.Client,
		_, _ string,
	) ([]openRouterModelConfig, error) {
		return nil, catalogErr
	}

	_, _, err := subagentLanguageModelWithModelFetcher(
		context.Background(),
		cfg,
		provider,
		"deepseek/deepseek-chat",
		"",
		fetchModels,
	)
	if !errors.Is(err, catalogErr) || !strings.Contains(
		err.Error(),
		`validate OpenRouter model "deepseek/deepseek-chat"`,
	) {
		t.Fatalf("error = %v, want wrapped catalog failure", err)
	}
	if len(provider.requested) != 0 {
		t.Fatalf("provider constructed models %v after catalog failure", provider.requested)
	}
}

func TestSessionLanguageModelBypassesSubagentFactory(t *testing.T) {
	provider := newSubagentRecordingProvider()
	pool := agent.NewPool()
	pool.SetProvider(provider)
	pool.SetDefaultModelName("session-model")
	pool.SetModelFactory(func(
		_ context.Context,
		_ fantasy.Provider,
		_, _ string,
	) (fantasy.LanguageModel, error) {
		return nil, errors.New("sub-agent factory must not be called")
	})

	lm, err := pool.SessionLanguageModel(context.Background(), "")
	if err != nil {
		t.Fatalf("session model resolution: %v", err)
	}
	if lm == nil {
		t.Fatal("session model resolution returned no language model")
	}
	if len(provider.requested) != 1 || provider.requested[0] != "session-model" {
		t.Fatalf("provider requests = %v, want the session default", provider.requested)
	}
}

func TestSessionModelDoesNotUseOpenRouterSubagentValidation(t *testing.T) {
	cfg := &Config{Provider: providerOpenRouter, OpenRouterAPIKey: "test-key"}
	provider := newSubagentRecordingProvider()
	catalogRequests := 0
	fetchModels := func(
		_ context.Context,
		_ *http.Client,
		_, _ string,
	) ([]openRouterModelConfig, error) {
		catalogRequests++
		return nil, errors.New("catalog offline")
	}

	pool := agent.NewPool()
	pool.SetProvider(provider)
	pool.SetDefaultModelName("session-model")
	pool.SetModelFactory(newSubagentModelFactory(cfg, pool, fetchModels))

	lm, model, err := sessionModel(context.Background(), pool)
	if err != nil {
		t.Fatalf("session fallback during catalog outage: %v", err)
	}
	if lm == nil || model != "session-model" {
		t.Fatalf("session fallback = (%v, %q), want session model", lm, model)
	}
	if catalogRequests != 0 {
		t.Fatalf("session fallback made %d catalog requests, want none", catalogRequests)
	}
}

func TestSubagentOpenRouterTierValidatesModel(t *testing.T) {
	cfg := &Config{OpenRouterAPIKey: "test-key"}
	fetchModels := func(
		_ context.Context,
		_ *http.Client,
		_, _ string,
	) ([]openRouterModelConfig, error) {
		return []openRouterModelConfig{{ID: "deepseek/deepseek-chat", ContextWindow: 128000}}, nil
	}

	lm, err := modelForProviderTierWithModelFetcher(
		context.Background(), cfg, providerOpenRouter, "deepseek-flash", fetchModels,
	)
	if err == nil || !strings.Contains(err.Error(), "is not available to this account") {
		t.Fatalf("unavailable tier model error = %v", err)
	}
	if lm != nil {
		t.Fatal("unavailable tier model was constructed")
	}

	lm, err = modelForProviderTierWithModelFetcher(
		context.Background(), cfg, providerOpenRouter, "deepseek/deepseek-chat", fetchModels,
	)
	if err != nil {
		t.Fatalf("available tier model: %v", err)
	}
	if lm == nil || lm.Model() != "deepseek/deepseek-chat" {
		t.Fatalf("available tier model = %v", lm)
	}
}
