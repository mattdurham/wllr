package main

import (
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

	// LocalAPIKey is the API key sent to an OpenAI-compatible local endpoint
	// (WLLR_LOCAL_API_KEY, default: "ollama").
	LocalAPIKey string

	// LocalBaseURL is the OpenAI-compatible local endpoint base URL
	// (WLLR_LOCAL_BASE_URL, default: http://localhost:11434/v1).
	LocalBaseURL string

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
	cfg := &Config{
		AnthropicAPIKey: os.Getenv("ANTHROPIC_API_KEY"),
		OpenAIAPIKey:    os.Getenv("OPENAI_API_KEY"),
		GeminiAPIKey:    os.Getenv("GEMINI_API_KEY"),
		LocalAPIKey:     os.Getenv("WLLR_LOCAL_API_KEY"),
		LocalBaseURL:    os.Getenv("WLLR_LOCAL_BASE_URL"),
		ExtensionsDir:   expandTilde(os.Getenv("WLLR_EXTENSIONS_DIR")),
		Model:           os.Getenv("WLLR_MODEL"),
		Provider:        os.Getenv("WLLR_PROVIDER"),
		ContextWindow:   parseContextWindow(os.Getenv("WLLR_CONTEXT_WINDOW")),
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
		if cfg.Model == "" {
			cfg.Model = "claude-sonnet-4-6"
		}
	}
	cfg.Model = normalizeModelForProvider(cfg.Provider, cfg.Model)
	if cfg.LocalBaseURL == "" {
		cfg.LocalBaseURL = "http://localhost:11434/v1"
	}
	if cfg.LocalAPIKey == "" {
		cfg.LocalAPIKey = "ollama"
	}

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
