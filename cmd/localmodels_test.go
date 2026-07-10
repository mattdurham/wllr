package main

import (
	"context"
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
				BaseURL:       "http://localhost:8000/v1",
				ContextWindow: 300000,
			},
			{
				ID:            "qwen/qwen3-coder-next",
				Name:          "Qwen3 Coder Next",
				BaseURL:       "http://localhost:1234/v1",
				ContextWindow: 262144,
			},
		},
	}

	models := localModels(context.Background(), cfg)
	if len(models) != 2 {
		t.Fatalf("len(models) = %d, want 2: %+v", len(models), models)
	}
	if models[0].ID != "deepseek-v4-flash" || models[0].LocalBaseURL != "http://localhost:8000/v1" {
		t.Fatalf("configured model not preserved: %+v", models[0])
	}
	if models[1].ID != "qwen/qwen3-coder-next" || models[1].LocalBaseURL != "http://localhost:1234/v1" {
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
