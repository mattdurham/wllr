package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestResolveLocalProviderConfigUsesConfiguredLocalModel(t *testing.T) {
	cfg := &Config{
		Provider: providerLocal,
		LocalModels: []localModelConfig{
			{
				ID:            "deepseek-v4-flash",
				Name:          "Dwarfstar 4 Flash",
				BaseURL:       "http://localhost:8000/v1",
				ContextWindow: 300000,
			},
		},
	}

	resolveLocalProviderConfig(context.Background(), cfg)

	if cfg.Model != "deepseek-v4-flash" {
		t.Fatalf("Model = %q, want deepseek-v4-flash", cfg.Model)
	}
	if cfg.LocalBaseURL != "http://localhost:8000/v1" {
		t.Fatalf("LocalBaseURL = %q", cfg.LocalBaseURL)
	}
	if cfg.ContextWindow != 300000 {
		t.Fatalf("ContextWindow = %d, want 300000", cfg.ContextWindow)
	}
}

func TestLocalModelsUsesConfiguredModels(t *testing.T) {
	cfg := &Config{
		LocalModels: []localModelConfig{
			{
				ID:            "deepseek-v4-flash",
				Name:          "Dwarfstar 4 Flash",
				BaseURL:       "http://127.0.0.1:1/v1",
				ContextWindow: 300000,
			},
			{
				ID:            "qwen/qwen3-coder-next",
				Name:          "Qwen3 Coder Next",
				BaseURL:       "http://127.0.0.1:1/v1",
				ContextWindow: 262144,
			},
		},
	}

	models := localModels(context.Background(), cfg)
	if len(models) != 2 {
		t.Fatalf("len(models) = %d, want 2: %+v", len(models), models)
	}
	if models[0].ID != "deepseek-v4-flash" || models[0].LocalBaseURL != "http://127.0.0.1:1/v1" {
		t.Fatalf("configured model not preserved: %+v", models[0])
	}
	if models[1].ID != "qwen/qwen3-coder-next" || models[1].LocalBaseURL != "http://127.0.0.1:1/v1" {
		t.Fatalf("second configured model not annotated: %+v", models[1])
	}
}

func TestLocalProviderSublabel(t *testing.T) {
	got := localProviderSublabel(&Config{LocalBaseURL: "http://localhost:8000/v1", LocalContextWindow: 100000})
	want := "http://localhost:8000/v1 · 100k ctx"
	if got != want {
		t.Fatalf("localProviderSublabel = %q, want %q", got, want)
	}
}

func TestFirstAvailableModelKeepsPreferredWhenPresent(t *testing.T) {
	models := []modelInfo{{ID: "a"}, {ID: "b"}}
	if got := firstAvailableModel("b", models); got != "b" {
		t.Fatalf("firstAvailableModel = %q, want b", got)
	}
	if got := firstAvailableModel("missing", models); got != "a" {
		t.Fatalf("firstAvailableModel missing = %q, want a", got)
	}
}

func TestLocalModelsDiscoversModelsFromEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Errorf("request path = %q, want /v1/models", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("authorization = %q, want bearer token", got)
		}
		_, _ = fmt.Fprint(
			w,
			`{"object":"list","data":[{"id":"model-a","name":"Model A","context_length":262144},{"id":"model-b"}]}`,
		)
	}))
	defer server.Close()

	cfg := &Config{LocalModels: []localModelConfig{{
		ID: "configured-model", BaseURL: server.URL + "/v1", APIKey: "test-key",
	}}}
	models := localModels(context.Background(), cfg)
	if len(models) != 2 {
		t.Fatalf("len(models) = %d, want 2: %+v", len(models), models)
	}
	if models[0].ID != "model-a" || models[0].Name != "Model A" {
		t.Fatalf("first discovered model = %+v", models[0])
	}
	if models[0].ContextWindow != 262144 {
		t.Fatalf("discovered context window = %d, want 262144", models[0].ContextWindow)
	}
	if models[1].Name != "model-b" || models[1].LocalBaseURL != server.URL+"/v1" {
		t.Fatalf("second discovered model = %+v", models[1])
	}
}

func TestApplyLocalModelChoiceUsesDiscoveredModel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `{"data":[{"id":"remote-model"}]}`)
	}))
	defer server.Close()

	cfg := &Config{Provider: providerLocal, LocalModels: []localModelConfig{{
		ID: "configured-model", BaseURL: server.URL + "/v1",
	}}}
	if !applyLocalModelChoice(context.Background(), cfg, "remote-model") {
		t.Fatal("applyLocalModelChoice returned false")
	}
	if cfg.Model != "remote-model" || cfg.LocalBaseURL != server.URL+"/v1" {
		t.Fatalf("selected config = %+v", cfg)
	}
	if _, ok := cfg.localModelByID("remote-model"); !ok {
		t.Fatal("discovered model was not registered for provider construction")
	}
}
