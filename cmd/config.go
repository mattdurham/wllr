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

	// ExtensionsDir is the directory scanned for .wasm extension files (BOB_EXTENSIONS_DIR).
	ExtensionsDir string

	// Model is the default LLM model to use (BOB_MODEL, default: claude-sonnet-4-6).
	Model string

	// Provider is the LLM provider name (BOB_PROVIDER, default: anthropic).
	Provider string

	// ContextWindow overrides the model's context window in tokens (WLLR_CONTEXT_WINDOW).
	// When 0, wllr queries the provider at startup to determine the window.
	ContextWindow int64
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
		ExtensionsDir:   expandTilde(os.Getenv("WLLR_EXTENSIONS_DIR")),
		Model:           os.Getenv("WLLR_MODEL"),
		Provider:        os.Getenv("WLLR_PROVIDER"),
		ContextWindow:   parseContextWindow(os.Getenv("WLLR_CONTEXT_WINDOW")),
	}

	// Model precedence: env WLLR_MODEL > persisted selection (config.json wllr.model)
	// > built-in default. The persisted value is written by the /model picker.
	if cfg.Model == "" {
		cfg.Model = savedModel()
	}
	if cfg.Model == "" {
		cfg.Model = "claude-sonnet-4-6"
	}
	if cfg.Provider == "" {
		cfg.Provider = providerAnthropic
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

	switch cfg.Provider {
	case providerAnthropic:
		if cfg.AnthropicAPIKey == "" {
			return nil, fmt.Errorf("ANTHROPIC_API_KEY is required when WLLR_PROVIDER=anthropic (or run /login)")
		}
	case "openai":
		if cfg.OpenAIAPIKey == "" {
			return nil, fmt.Errorf("OPENAI_API_KEY is required when WLLR_PROVIDER=openai (or run /login)")
		}
	case "gemini":
		if cfg.GeminiAPIKey == "" {
			return nil, fmt.Errorf("GEMINI_API_KEY is required when BOB_PROVIDER=gemini")
		}
	default:
		// Unknown/custom providers: no built-in key requirement.
	}

	return cfg, nil
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
