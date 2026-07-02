package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchLocalModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Fatalf("path = %q, want /v1/models", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("Authorization = %q, want bearer key", got)
		}
		_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"zeta"},{"id":"alpha"},{"id":"alpha"}]}`))
	}))
	defer srv.Close()

	models, err := fetchLocalModels(context.Background(), &Config{
		LocalBaseURL:       srv.URL + "/v1",
		LocalAPIKey:        "test-key",
		LocalContextWindow: 100000,
	})
	if err != nil {
		t.Fatalf("fetchLocalModels: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("len(models) = %d, want 2", len(models))
	}
	if models[0].ID != "alpha" || models[1].ID != "zeta" {
		t.Fatalf("models sorted/deduped = %+v", models)
	}
	for _, m := range models {
		if m.ContextWindow != 100000 {
			t.Fatalf("model context = %d, want 100000", m.ContextWindow)
		}
	}
}

func TestResolveLocalProviderConfigUsesModelsEndpoint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"local-large"}]}`))
	}))
	defer srv.Close()

	cfg := &Config{
		Provider:           providerLocal,
		LocalBaseURL:       srv.URL,
		LocalContextWindow: 100000,
	}
	resolveLocalProviderConfig(context.Background(), cfg)

	if cfg.Model != "local-large" {
		t.Fatalf("Model = %q, want local-large", cfg.Model)
	}
	if cfg.ContextWindow != 100000 {
		t.Fatalf("ContextWindow = %d, want 100000", cfg.ContextWindow)
	}
}

func TestResolveLocalProviderConfigAppliesContextWhenModelsEndpointFails(t *testing.T) {
	cfg := &Config{
		Provider:           providerLocal,
		LocalBaseURL:       "http://127.0.0.1:1/v1",
		LocalContextWindow: 300000,
	}

	resolveLocalProviderConfig(context.Background(), cfg)

	if cfg.ContextWindow != 300000 {
		t.Fatalf("ContextWindow = %d, want 300000", cfg.ContextWindow)
	}
}

func TestLocalProviderSublabel(t *testing.T) {
	got := localProviderSublabel(&Config{LocalBaseURL: "http://localhost:8000/v1", LocalContextWindow: 100000})
	want := "http://localhost:8000/v1 · 100k ctx"
	if got != want {
		t.Fatalf("localProviderSublabel = %q, want %q", got, want)
	}
}
