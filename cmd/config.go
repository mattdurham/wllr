package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Config holds runtime configuration for the bob binary.
type Config struct {
	// AnthropicAPIKey is the Anthropic API key (ANTHROPIC_API_KEY).
	AnthropicAPIKey string

	// OpenAIAPIKey is the OpenAI API key (OPENAI_API_KEY).
	OpenAIAPIKey string

	// GeminiAPIKey is the Google Gemini API key (GEMINI_API_KEY).
	GeminiAPIKey string

	// LocalAPIKey is the optional API key sent to an OpenAI-compatible local
	// endpoint (WLLR_LOCAL_API_KEY or wllr.local_api_key).
	LocalAPIKey string

	// LocalBaseURL is the OpenAI-compatible local endpoint base URL
	// (WLLR_LOCAL_BASE_URL or wllr.local_base_url).
	LocalBaseURL string

	// LocalContextWindow is the default context window for models discovered
	// from the configured local endpoint (WLLR_LOCAL_CONTEXT_WINDOW or
	// wllr.local_context_window).
	LocalContextWindow int64

	// ExtensionsDir is the directory scanned for .wasm extension files (BOB_EXTENSIONS_DIR).
	ExtensionsDir string

	// Model is the default LLM model to use (BOB_MODEL, default: claude-sonnet-4-6).
	Model string

	// Provider is the LLM provider name (BOB_PROVIDER, default: anthropic).
	Provider string

	// ContextWindow overrides the model's context window in tokens (WLLR_CONTEXT_WINDOW).
	// When 0, wllr queries the provider at startup to determine the window.
	ContextWindow int64

	// ModelConfigured reports whether a model came from env or persisted config,
	// rather than the built-in default.
	ModelConfigured bool

	// ProviderConfigured reports whether a provider was explicitly set.
	ProviderConfigured bool
}

// LoadConfig reads configuration from environment variables.
//
// Variable precedence (highest to lowest):
//  1. Environment variables
//
// Defaults:
//   - WLLR_MODEL: claude-sonnet-4-6
//   - WLLR_PROVIDER: anthropic
//
// Returns an error if the active provider's API key is empty.
func LoadConfig() (*Config, error) {
	fileCfg := loadWllrSettings()
	cfg := &Config{
		AnthropicAPIKey: os.Getenv("ANTHROPIC_API_KEY"),
		OpenAIAPIKey:    os.Getenv("OPENAI_API_KEY"),
		GeminiAPIKey:    os.Getenv("GEMINI_API_KEY"),
		LocalAPIKey:     firstNonEmpty(os.Getenv("WLLR_LOCAL_API_KEY"), fileCfg.LocalAPIKey),
		LocalBaseURL:    firstNonEmpty(os.Getenv("WLLR_LOCAL_BASE_URL"), fileCfg.LocalBaseURL),
		ExtensionsDir:   expandTilde(os.Getenv("WLLR_EXTENSIONS_DIR")),
		Model:           os.Getenv("WLLR_MODEL"),
		Provider:        os.Getenv("WLLR_PROVIDER"),
		ContextWindow:   firstPositive(parseContextWindow(os.Getenv("WLLR_CONTEXT_WINDOW")), fileCfg.ContextWindow),
		LocalContextWindow: firstPositive(
			parseContextWindow(os.Getenv("WLLR_LOCAL_CONTEXT_WINDOW")),
			fileCfg.LocalContextWindow,
		),
	}

	// Provider precedence: env WLLR_PROVIDER > persisted selection
	// (config.json wllr.provider) > built-in default.
	if cfg.Provider == "" {
		cfg.Provider = savedProvider()
	}
	cfg.ProviderConfigured = cfg.Provider != ""
	if cfg.Provider == "" {
		cfg.Provider = providerAnthropic
	}

	// Model precedence: env WLLR_MODEL > persisted selection (config.json wllr.model)
	// > provider-specific built-in default. The persisted value is written by the
	// /model picker and setup wizard.
	if cfg.Model == "" {
		cfg.Model = savedModel()
	}
	cfg.ModelConfigured = cfg.Model != ""
	if cfg.Model == "" {
		cfg.Model = defaultModelForProvider(cfg.Provider)
		if cfg.Model == "" && cfg.Provider != providerLocal {
			cfg.Model = "claude-sonnet-4-6"
		}
	}
	cfg.Model = normalizeModelForProvider(cfg.Provider, cfg.Model)
	// Auth-file OAuth credentials satisfy the provider key requirement: if no env
	// key is set but a stored OAuth access token exists, seed the key from it so
	// the initial provider build works. Refresh-on-expiry happens at startup
	// (resolveStartupOAuth). This lets a prior /login persist without an env var.
	if cfg.Provider == providerAnthropic && cfg.AnthropicAPIKey == "" {
		if cred, ok := loadAuthCredential(providerAnthropic); ok && cred.Type == authTypeOAuth && cred.Access != "" {
			cfg.AnthropicAPIKey = cred.Access
		}
	}
	if cfg.Provider == "openai" && cfg.OpenAIAPIKey == "" {
		if cred, ok := loadAuthCredential("openai"); ok && cred.Type == authTypeOAuth && cred.Access != "" {
			cfg.OpenAIAPIKey = cred.Access
		}
	}

	return cfg, nil
}

type wllrSettings struct {
	Provider              string          `json:"provider"`
	Model                 string          `json:"model"`
	LocalAPIKey           string          `json:"local_api_key"`
	LocalBaseURL          string          `json:"local_base_url"`
	ContextWindow         int64           `json:"-"`
	LocalContextWindow    int64           `json:"-"`
	RawContextWindow      json.RawMessage `json:"context_window"`
	RawLocalContextWindow json.RawMessage `json:"local_context_window"`
}

func loadWllrSettings() wllrSettings {
	raw, err := loadConfigGroup(wllrConfigGroup)
	if err != nil {
		return wllrSettings{}
	}
	var settings wllrSettings
	if err := json.Unmarshal(raw, &settings); err != nil {
		return wllrSettings{}
	}
	settings.ContextWindow = parseContextWindowJSON(settings.RawContextWindow)
	settings.LocalContextWindow = parseContextWindowJSON(settings.RawLocalContextWindow)
	return settings
}

func parseContextWindowJSON(raw json.RawMessage) int64 {
	if len(raw) == 0 || string(raw) == "null" {
		return 0
	}
	var n int64
	if err := json.Unmarshal(raw, &n); err == nil {
		if n > 0 {
			return n
		}
		return 0
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return parseContextWindow(s)
	}
	return 0
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func firstPositive(values ...int64) int64 {
	for _, v := range values {
		if v > 0 {
			return v
		}
	}
	return 0
}

func missingProviderAuth(cfg *Config) (string, bool) {
	if cfg == nil {
		return "", false
	}
	switch cfg.Provider {
	case providerAnthropic:
		return "ANTHROPIC_API_KEY", cfg.AnthropicAPIKey == ""
	case "openai":
		return "OPENAI_API_KEY", cfg.OpenAIAPIKey == ""
	case "gemini":
		return "GEMINI_API_KEY", cfg.GeminiAPIKey == ""
	default:
		return "", false
	}
}

func missingAuthError(provider, envVar string) error {
	msg := fmt.Sprintf("%s is required when WLLR_PROVIDER=%s", envVar, provider)
	switch provider {
	case providerAnthropic, "openai":
		return fmt.Errorf("%s. Set %s, or run `wllr login --provider %s` to authenticate with OAuth before starting the TUI", msg, envVar, provider)
	default:
		return fmt.Errorf("%s. Set %s before starting wllr", msg, envVar)
	}
}

// parseContextWindow converts a string like "1000000" or "1m" to int64 tokens.
// Returns 0 if empty or unparseable (caller uses provider-derived value).
func parseContextWindow(s string) int64 {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return 0
	}
	multiplier := int64(1)
	if strings.HasSuffix(s, "k") {
		multiplier = 1_000
		s = s[:len(s)-1]
	} else if strings.HasSuffix(s, "m") {
		multiplier = 1_000_000
		s = s[:len(s)-1]
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n <= 0 {
		return 0
	}
	return n * multiplier
}

// expandTilde replaces a leading "~" with the user's home directory.
// Returns the path unchanged if it doesn't start with "~" or if the home
// directory cannot be determined.
func expandTilde(path string) string {
	if !strings.HasPrefix(path, "~") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return home + path[1:]
}
