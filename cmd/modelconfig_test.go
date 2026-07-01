package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// withConfigPath points WLLR_CONFIG at a temp file for the duration of a test.
func withConfigPath(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	t.Setenv("WLLR_CONFIG", path)
	return path
}

func TestSaveModel_RoundTrip(t *testing.T) {
	withConfigPath(t)

	if got := savedModel(); got != "" {
		t.Errorf("savedModel on missing file = %q, want empty", got)
	}
	if err := saveModel("claude-opus-4-8"); err != nil {
		t.Fatalf("saveModel: %v", err)
	}
	if got := savedModel(); got != "claude-opus-4-8" {
		t.Errorf("savedModel = %q, want claude-opus-4-8", got)
	}
	// Overwrite.
	if err := saveModel("gpt-5.4"); err != nil {
		t.Fatalf("saveModel overwrite: %v", err)
	}
	if got := savedModel(); got != "gpt-5.4" {
		t.Errorf("savedModel after overwrite = %q, want gpt-5.4", got)
	}
}

func TestSaveProvider_RoundTrip(t *testing.T) {
	withConfigPath(t)

	if got := savedProvider(); got != "" {
		t.Errorf("savedProvider on missing file = %q, want empty", got)
	}
	if err := saveProvider("local"); err != nil {
		t.Fatalf("saveProvider: %v", err)
	}
	if got := savedProvider(); got != "local" {
		t.Errorf("savedProvider = %q, want local", got)
	}
}

func TestSaveModel_PreservesOtherGroups(t *testing.T) {
	path := withConfigPath(t)

	// Seed the config with another group and another wllr key.
	seed := map[string]any{
		"permissions": map[string]any{"read": map[string]any{"allow": []string{"*"}}},
		"wllr":        map[string]any{"context_window": 200000},
	}
	data, _ := json.MarshalIndent(seed, "", "  ")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("seed write: %v", err)
	}

	if err := saveModel("gemini-3-pro-preview"); err != nil {
		t.Fatalf("saveModel: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var all map[string]json.RawMessage
	if err := json.Unmarshal(raw, &all); err != nil {
		t.Fatalf("parse: %v", err)
	}
	// Other group preserved.
	if _, ok := all["permissions"]; !ok {
		t.Error("permissions group was dropped")
	}
	// wllr group keeps context_window AND gains model.
	var wllr map[string]json.RawMessage
	if err := json.Unmarshal(all["wllr"], &wllr); err != nil {
		t.Fatalf("parse wllr group: %v", err)
	}
	if _, ok := wllr["context_window"]; !ok {
		t.Error("wllr.context_window was dropped")
	}
	if got := string(wllr["model"]); got != `"gemini-3-pro-preview"` {
		t.Errorf("wllr.model = %s, want quoted gemini-3-pro-preview", got)
	}
}

func TestModelCatalog_KnownProviders(t *testing.T) {
	for _, p := range []string{"anthropic", "openai", "gemini", "local"} {
		models := modelsForProvider(p)
		if len(models) == 0 {
			t.Errorf("provider %q has no models in catalog", p)
		}
		for _, m := range models {
			if m.ID == "" || m.Name == "" || m.ContextWindow <= 0 {
				t.Errorf("provider %q has an invalid model entry: %+v", p, m)
			}
		}
	}
	if modelsForProvider("nonexistent") != nil {
		t.Error("unknown provider should return nil catalog")
	}
}

func TestDefaultModelForProvider(t *testing.T) {
	tests := map[string]string{
		"anthropic": "claude-sonnet-4-6",
		"openai":    "gpt-5.5",
		"local":     "llama3.2",
	}
	for provider, want := range tests {
		if got := defaultModelForProvider(provider); got != want {
			t.Errorf("defaultModelForProvider(%q) = %q, want %q", provider, got, want)
		}
	}
}

func TestModelsForOpenAIAuth_ChatGPTOAuthSubset(t *testing.T) {
	withAuthPath(t)
	if err := saveAuthCredential("openai", authCredential{Type: authTypeOAuth, Access: "tok", AccountID: "acct"}); err != nil {
		t.Fatalf("saveAuthCredential: %v", err)
	}
	models := modelsForOpenAIAuth()
	if len(models) == 0 {
		t.Fatal("modelsForOpenAIAuth returned no models")
	}
	if models[0].ID != "gpt-5.5" {
		t.Fatalf("first ChatGPT OAuth model = %q, want gpt-5.5", models[0].ID)
	}
	for _, m := range models {
		if m.ID == "gpt-5.3-codex" || m.ID == "gpt-5.2-codex" {
			t.Fatalf("ChatGPT OAuth model list should not include unsupported Codex-suffixed model %q", m.ID)
		}
	}
}

func TestContextWindowFromCatalog(t *testing.T) {
	if got := contextWindowFromCatalog("anthropic", "claude-sonnet-4-6"); got != 200000 {
		t.Errorf("claude-sonnet-4-6 ctx = %d, want 200000", got)
	}
	if got := contextWindowFromCatalog("anthropic", "no-such-model"); got != 0 {
		t.Errorf("unknown model ctx = %d, want 0", got)
	}
	if got := contextWindowFromCatalog("gemini", "gemini-3-pro-preview"); got != 1048576 {
		t.Errorf("gemini-3-pro-preview ctx = %d, want 1048576", got)
	}
}
