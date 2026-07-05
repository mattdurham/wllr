package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestLoadConfig_APIKey(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-key-123")
	t.Setenv("WLLR_EXTENSIONS_DIR", "")
	t.Setenv("WLLR_MODEL", "")
	t.Setenv("WLLR_PROVIDER", "")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}
	if cfg.AnthropicAPIKey != "test-key-123" {
		t.Errorf("AnthropicAPIKey = %q, want %q", cfg.AnthropicAPIKey, "test-key-123")
	}
}

func TestLoadConfig_ExtensionsDir(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "key")
	t.Setenv("WLLR_EXTENSIONS_DIR", "/tmp/extensions")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}
	if cfg.ExtensionsDir != "/tmp/extensions" {
		t.Errorf("ExtensionsDir = %q, want %q", cfg.ExtensionsDir, "/tmp/extensions")
	}
}

func TestLoadConfig_ModelDefault(t *testing.T) {
	withConfigPath(t) // isolate from any real ~/.config/wllr/config.json (persisted model)
	t.Setenv("ANTHROPIC_API_KEY", "key")
	t.Setenv("WLLR_MODEL", "")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}
	if cfg.Model != "claude-sonnet-4-6" {
		t.Errorf("Model = %q, want %q", cfg.Model, "claude-sonnet-4-6")
	}
}

func TestLoadConfig_ModelOverride(t *testing.T) {
	withConfigPath(t)
	withAuthPath(t)
	t.Setenv("ANTHROPIC_API_KEY", "key")
	t.Setenv("WLLR_MODEL", "claude-haiku-3-5")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}
	if cfg.Model != "claude-haiku-3-5" {
		t.Errorf("Model = %q, want %q", cfg.Model, "claude-haiku-3-5")
	}
}

func TestLoadConfig_ProviderDefault(t *testing.T) {
	withConfigPath(t)
	withAuthPath(t)
	t.Setenv("ANTHROPIC_API_KEY", "key")
	t.Setenv("WLLR_PROVIDER", "")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}
	if cfg.Provider != "anthropic" {
		t.Errorf("Provider = %q, want %q", cfg.Provider, "anthropic")
	}
}

func TestLoadConfig_ProviderOverride(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "key")
	t.Setenv("WLLR_PROVIDER", "custom")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}
	if cfg.Provider != "custom" {
		t.Errorf("Provider = %q, want %q", cfg.Provider, "custom")
	}
}

func TestLoadConfig_SavedLocalProviderDefault(t *testing.T) {
	withConfigPath(t)
	if err := saveProvider("local"); err != nil {
		t.Fatalf("saveProvider: %v", err)
	}
	t.Setenv("WLLR_PROVIDER", "")
	t.Setenv("WLLR_MODEL", "")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}
	if cfg.Provider != "local" {
		t.Errorf("Provider = %q, want local", cfg.Provider)
	}
	if cfg.Model != "" {
		t.Errorf("Model = %q, want empty before local model discovery", cfg.Model)
	}
	if cfg.ProviderConfigured != true {
		t.Error("ProviderConfigured should be true for saved provider")
	}
	if _, missing := missingProviderAuth(cfg); missing {
		t.Error("local provider should not require auth")
	}
}

func TestLoadConfig_LocalSettingsFromConfig(t *testing.T) {
	path := withConfigPath(t)
	seed := map[string]any{
		"wllr": map[string]any{
			"provider":             "local",
			"model":                "deepseek-v4-flash",
			"local_base_url":       "http://localhost:8000/v1",
			"local_api_key":        "local-key",
			"local_context_window": 300000,
		},
	}
	data, _ := json.MarshalIndent(seed, "", "  ")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("seed write: %v", err)
	}
	t.Setenv("WLLR_PROVIDER", "")
	t.Setenv("WLLR_MODEL", "")
	t.Setenv("WLLR_LOCAL_BASE_URL", "")
	t.Setenv("WLLR_LOCAL_API_KEY", "")
	t.Setenv("WLLR_LOCAL_CONTEXT_WINDOW", "")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}
	if cfg.Provider != providerLocal {
		t.Fatalf("Provider = %q, want local", cfg.Provider)
	}
	if cfg.Model != "deepseek-v4-flash" {
		t.Errorf("Model = %q, want deepseek-v4-flash", cfg.Model)
	}
	if !cfg.ProviderConfigured {
		t.Error("ProviderConfigured should be true for saved provider")
	}
	if !cfg.ModelConfigured {
		t.Error("ModelConfigured should be true for saved model")
	}
	if cfg.LocalBaseURL != "http://localhost:8000/v1" {
		t.Errorf("LocalBaseURL = %q", cfg.LocalBaseURL)
	}
	if cfg.LocalAPIKey != "local-key" {
		t.Errorf("LocalAPIKey = %q", cfg.LocalAPIKey)
	}
	if cfg.LocalContextWindow != 300000 {
		t.Errorf("LocalContextWindow = %d, want 300000", cfg.LocalContextWindow)
	}
}

func TestLoadConfig_NormalizesUnsupportedCodexOAuthModel(t *testing.T) {
	withConfigPath(t)
	withAuthPath(t)
	if err := saveProvider("openai"); err != nil {
		t.Fatalf("saveProvider: %v", err)
	}
	if err := saveModel("gpt-5.3-codex"); err != nil {
		t.Fatalf("saveModel: %v", err)
	}
	if err := saveAuthCredential("openai", authCredential{Type: authTypeOAuth, Access: "tok", AccountID: "acct"}); err != nil {
		t.Fatalf("saveAuthCredential: %v", err)
	}
	t.Setenv("WLLR_PROVIDER", "")
	t.Setenv("WLLR_MODEL", "")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}
	if cfg.Model != "gpt-5.5" {
		t.Errorf("Model = %q, want gpt-5.5", cfg.Model)
	}
}

func TestLoadConfig_MissingAPIKeyError(t *testing.T) {
	withAuthPath(t)
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("WLLR_PROVIDER", "anthropic")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}
	envVar, missing := missingProviderAuth(cfg)
	if !missing || envVar != "ANTHROPIC_API_KEY" {
		t.Fatalf("missingProviderAuth = (%q,%v), want (ANTHROPIC_API_KEY,true)", envVar, missing)
	}
	if msg := missingAuthError(cfg.Provider, envVar).Error(); !strings.Contains(
		msg,
		"wllr login --provider anthropic",
	) {
		t.Fatalf("missing key error = %q, want standalone login guidance", msg)
	}
}

func TestLoadConfig_MissingAPIKeyNonAnthropicOK(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("WLLR_PROVIDER", "custom")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig with non-anthropic provider should not error on missing key: %v", err)
	}
	if cfg.AnthropicAPIKey != "" {
		t.Errorf("AnthropicAPIKey should be empty for custom provider: got %q", cfg.AnthropicAPIKey)
	}
}

func TestLoadConfig_AllEnvVars(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "mykey")
	t.Setenv("OPENAI_API_KEY", "oai-key")
	t.Setenv("GEMINI_API_KEY", "gem-key")
	t.Setenv("WLLR_EXTENSIONS_DIR", "/ext")
	t.Setenv("WLLR_MODEL", "claude-opus-4-5")
	t.Setenv("WLLR_PROVIDER", "anthropic")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}
	if cfg.AnthropicAPIKey != "mykey" {
		t.Errorf("AnthropicAPIKey = %q", cfg.AnthropicAPIKey)
	}
	if cfg.OpenAIAPIKey != "oai-key" {
		t.Errorf("OpenAIAPIKey = %q", cfg.OpenAIAPIKey)
	}
	if cfg.GeminiAPIKey != "gem-key" {
		t.Errorf("GeminiAPIKey = %q", cfg.GeminiAPIKey)
	}
	if cfg.ExtensionsDir != "/ext" {
		t.Errorf("ExtensionsDir = %q", cfg.ExtensionsDir)
	}
	if cfg.Model != "claude-opus-4-5" {
		t.Errorf("Model = %q", cfg.Model)
	}
	if cfg.Provider != "anthropic" {
		t.Errorf("Provider = %q", cfg.Provider)
	}
}

func TestLoadConfig_ExpandsHomeTilde(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot determine home dir")
	}
	t.Setenv("ANTHROPIC_API_KEY", "key")
	t.Setenv("WLLR_EXTENSIONS_DIR", "~/myext")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}
	expected := home + "/myext"
	if cfg.ExtensionsDir != expected {
		t.Errorf("ExtensionsDir = %q, want %q", cfg.ExtensionsDir, expected)
	}
}

func TestLoadConfig_OpenAIAPIKey(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "oai-key-456")
	t.Setenv("WLLR_PROVIDER", "openai")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}
	if cfg.OpenAIAPIKey != "oai-key-456" {
		t.Errorf("OpenAIAPIKey = %q, want %q", cfg.OpenAIAPIKey, "oai-key-456")
	}
}

func TestLoadConfig_GeminiAPIKey(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("GEMINI_API_KEY", "gem-key-789")
	t.Setenv("WLLR_PROVIDER", "gemini")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}
	if cfg.GeminiAPIKey != "gem-key-789" {
		t.Errorf("GeminiAPIKey = %q, want %q", cfg.GeminiAPIKey, "gem-key-789")
	}
}

func TestLoadConfig_MissingOpenAIKeyError(t *testing.T) {
	withAuthPath(t)
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("WLLR_PROVIDER", "openai")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}
	envVar, missing := missingProviderAuth(cfg)
	if !missing || envVar != "OPENAI_API_KEY" {
		t.Fatalf("missingProviderAuth = (%q,%v), want (OPENAI_API_KEY,true)", envVar, missing)
	}
	if msg := missingAuthError(cfg.Provider, envVar).Error(); !strings.Contains(msg, "wllr login --provider openai") {
		t.Fatalf("missing key error = %q, want standalone login guidance", msg)
	}
}

func TestLoadConfig_MissingGeminiKeyError(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "")
	t.Setenv("WLLR_PROVIDER", "gemini")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}
	envVar, missing := missingProviderAuth(cfg)
	if !missing || envVar != "GEMINI_API_KEY" {
		t.Fatalf("missingProviderAuth = (%q,%v), want (GEMINI_API_KEY,true)", envVar, missing)
	}
}
