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

	// LocalAPIKey is the optional API key for the selected configured local model.
	LocalAPIKey string

	// LocalBaseURL is the OpenAI-compatible endpoint for the selected configured
	// local model.
	LocalBaseURL string

	// ExtensionsDir is the directory scanned for .wasm extension files (BOB_EXTENSIONS_DIR).
	ExtensionsDir string

	// Model is the default LLM model to use (BOB_MODEL, default: claude-sonnet-4-6).
	Model string

	// Provider is the LLM provider name (BOB_PROVIDER, default: anthropic).
	Provider string

	// LocalModels are optional explicit OpenAI-compatible local models. They let
	// one "local" provider expose multiple endpoint/model pairs in the picker.
	LocalModels []localModelConfig

	// LocalContextWindow is the context window for the selected configured local
	// model.
	LocalContextWindow int64

	// ContextWindow overrides the model's context window in tokens (WLLR_CONTEXT_WINDOW).
	// When 0, wllr queries the provider at startup to determine the window.
	ContextWindow int64

	// ContextWindowConfigured reports whether ContextWindow came from explicit
	// env/config override instead of selected model metadata.
	ContextWindowConfigured bool

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
	contextWindow := firstPositive(parseContextWindow(os.Getenv("WLLR_CONTEXT_WINDOW")), fileCfg.ContextWindow)
	cfg := &Config{
		AnthropicAPIKey:         os.Getenv("ANTHROPIC_API_KEY"),
		OpenAIAPIKey:            os.Getenv("OPENAI_API_KEY"),
		GeminiAPIKey:            os.Getenv("GEMINI_API_KEY"),
		ExtensionsDir:           expandTilde(os.Getenv("WLLR_EXTENSIONS_DIR")),
		Model:                   os.Getenv("WLLR_MODEL"),
		Provider:                os.Getenv("WLLR_PROVIDER"),
		ContextWindow:           contextWindow,
		ContextWindowConfigured: contextWindow > 0,
		LocalModels:             fileCfg.LocalModels,
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
			cfg.Model = defaultAnthropicModel
		}
	}
	cfg.Model = normalizeModelForProvider(cfg.Provider, cfg.Model)
	if cfg.Provider == providerLocal {
		cfg.applyLocalModelSelection(cfg.Model)
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
	if cfg.Provider == providerOpenAI && cfg.OpenAIAPIKey == "" {
		if cred, ok := loadAuthCredential(providerOpenAI); ok && cred.Type == authTypeOAuth && cred.Access != "" {
			cfg.OpenAIAPIKey = cred.Access
		}
	}

	return cfg, nil
}

type wllrSettings struct {
	Provider         string             `json:"provider"`
	Model            string             `json:"model"`
	LocalModels      []localModelConfig `json:"local_models"`
	RawContextWindow json.RawMessage    `json:"context_window"`
	ContextWindow    int64              `json:"-"`
}

type localModelConfig struct {
	ID               string          `json:"id"`
	Name             string          `json:"name"`
	BaseURL          string          `json:"base_url"`
	APIKey           string          `json:"api_key"`
	RawContextWindow json.RawMessage `json:"context_window"`
	ContextWindow    int64           `json:"-"`

	// ThinkingModes is an explicit per-model list of reasoning effort mode IDs
	// (e.g. ["none","low","medium","high"]). When set, it overrides whatever
	// the model's endpoint declares (LM Studio app API) and the standard
	// OpenAI fallback set for the /thinking picker.
	ThinkingModes []string `json:"thinking_modes,omitempty"`
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
	for i := range settings.LocalModels {
		settings.LocalModels[i].ContextWindow = parseContextWindowJSON(settings.LocalModels[i].RawContextWindow)
	}
	return settings
}

func (cfg *Config) localModelByID(id string) (localModelConfig, bool) {
	if cfg == nil || id == "" {
		return localModelConfig{}, false
	}
	for _, lm := range cfg.LocalModels {
		if lm.ID == id {
			return lm, true
		}
	}
	return localModelConfig{}, false
}

// localModelEntry returns the local model entry with the given ID from the
// persisted settings, for callers that only hold the model ID.
func (s wllrSettings) localModelEntry(id string) (localModelConfig, bool) {
	for _, lm := range s.LocalModels {
		if lm.ID == id {
			return lm, true
		}
	}
	return localModelConfig{}, false
}

func (cfg *Config) applyLocalModelSelection(id string) bool {
	lm, ok := cfg.localModelByID(id)
	if !ok {
		return false
	}
	cfg.Model = lm.ID
	cfg.LocalBaseURL = lm.BaseURL
	cfg.LocalAPIKey = lm.APIKey
	// Window precedence mirrors rememberLocalModel: an explicit local_models
	// context_window is authoritative; a window-less entry clears the pool-
	// facing window (unless a user override is set) so the next model in use
	// is not represented by the previous model's window.
	if lm.ContextWindow > 0 {
		cfg.LocalContextWindow = lm.ContextWindow
		if !cfg.ContextWindowConfigured {
			cfg.ContextWindow = lm.ContextWindow
		}
	} else if !cfg.ContextWindowConfigured {
		cfg.LocalContextWindow = 0
		cfg.ContextWindow = 0
	}
	return true
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
	case providerOpenAI:
		return "OPENAI_API_KEY", cfg.OpenAIAPIKey == ""
	case providerGemini:
		return "GEMINI_API_KEY", cfg.GeminiAPIKey == ""
	default:
		return "", false
	}
}

func missingAuthError(provider, envVar string) error {
	msg := fmt.Sprintf("%s is required when WLLR_PROVIDER=%s", envVar, provider)
	switch provider {
	case providerAnthropic, providerOpenAI:
		return fmt.Errorf(
			"%s. Set %s, or run `wllr login --provider %s` to authenticate with OAuth before starting the TUI",
			msg,
			envVar,
			provider,
		)
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
