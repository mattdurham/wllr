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

func TestSavedWllrField_MixedValueTypes(t *testing.T) {
	path := withConfigPath(t)
	seed := map[string]any{
		"wllr": map[string]any{
			"provider":       "local",
			"model":          "deepseek-v4-flash",
			"context_window": 300000,
		},
	}
	data, _ := json.MarshalIndent(seed, "", "  ")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("seed write: %v", err)
	}

	if got := savedProvider(); got != "local" {
		t.Fatalf("savedProvider = %q, want local", got)
	}
	if got := savedModel(); got != "deepseek-v4-flash" {
		t.Fatalf("savedModel = %q, want deepseek-v4-flash", got)
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
	for _, p := range []string{"anthropic", "openai", "gemini"} {
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

// TestSupportedThinkingModesForModel tests that each model has the correct thinking modes
func TestSupportedThinkingModesForModel(t *testing.T) {
	tests := []struct {
		name         string
		provider     string
		model        string
		wantModes    int // expected number of thinking modes
		wantHasNone  bool // whether "none" mode is included
	}{
		{
			name:         "claude-opus-4-8",
			provider:     "anthropic",
			model:        "claude-opus-4-8",
			wantModes:    5,
			wantHasNone:  false,
		},
		{
			name:         "claude-sonnet-4-6",
			provider:     "anthropic",
			model:        "claude-sonnet-4-6",
			wantModes:    5,
			wantHasNone:  false,
		},
		{
			name:         "claude-haiku-4-5-20251001",
			provider:     "anthropic",
			model:        "claude-haiku-4-5-20251001",
			wantModes:    1,
			wantHasNone:  true,
		},
		{
			name:         "gpt-5.5",
			provider:     "openai",
			model:        "gpt-5.5",
			wantModes:    6,
			wantHasNone:  true,
		},
		{
			name:         "gemini-3.5-flash",
			provider:     "gemini",
			model:        "gemini-3.5-flash",
			wantModes:    5,
			wantHasNone:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			modes := supportedThinkingModesForModel(tt.provider, tt.model)
			if len(modes) != tt.wantModes {
				t.Errorf("supportedThinkingModesForModel(%q, %q) = %d modes, want %d", tt.provider, tt.model, len(modes), tt.wantModes)
			}

			hasNone := false
			for _, mode := range modes {
				if mode.ID == "none" || mode.Name == "None" {
					hasNone = true
				}
			}
			if hasNone != tt.wantHasNone {
				t.Errorf("has none mode = %v, want %v", hasNone, tt.wantHasNone)
			}
		})
	}
}

// TestCurrentThinkingModeForModel tests the current thinking mode lookup
func TestCurrentThinkingModeForModel(t *testing.T) {
	tests := []struct {
		name       string
		provider   string
		model      string
		setupLevel thinkingLevel
		wantMode   string
	}{
		{
			name:       "anthropic default",
			provider:   "anthropic",
			model:      "claude-sonnet-4-6",
			setupLevel: thinkingOff,
			wantMode:   "none", // default thinking budget when level is "off"
		},
		{
			name:       "openai default",
			provider:   "openai",
			model:      "gpt-5.5",
			setupLevel: thinkingOff,
			wantMode:   "none", // default reasoning effort
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setupLevel != "" {
				if err := saveThinkingLevel(tt.setupLevel); err != nil {
					t.Fatalf("failed to setup thinking level %q: %v", tt.setupLevel, err)
				}
			}

			mode := currentThinkingModeForModel(tt.provider, tt.model)
			if mode != tt.wantMode {
				t.Errorf("currentThinkingModeForModel(%q, %q) = %q, want %q", tt.provider, tt.model, mode, tt.wantMode)
			}

			if err := saveThinkingLevel(thinkingOff); err != nil {
				t.Logf("failed to cleanup thinking level: %v", err)
			}
		})
	}
}

// TestThinkingModesForUnknownModel tests behavior for unknown models
func TestThinkingModesForUnknownModel(t *testing.T) {
	// Unknown provider should return empty
	modes := supportedThinkingModesForModel("unknown-provider", "some-model")
	if len(modes) != 0 {
		t.Errorf("unsupported provider returned %d modes, want 0", len(modes))
	}

	// Unknown model with known provider should return empty
	modes = supportedThinkingModesForModel("anthropic", "unknown-model")
	if len(modes) != 0 {
		t.Errorf("unknown model returned %d modes, want 0", len(modes))
	}

	// Current mode returns "none" for unknown models (safe default)
	mode := currentThinkingModeForModel("anthropic", "unknown-model")
	if mode != "none" {
		t.Errorf("current thinking mode for unknown model = %q, want none (safe default)", mode)
	}
}

// TestThinkingModeHasCorrectMetadata tests that thinking modes have proper metadata
func TestThinkingModeHasCorrectMetadata(t *testing.T) {
	modes := supportedThinkingModesForModel("anthropic", "claude-opus-4-8")
	if len(modes) == 0 {
		t.Fatal("claude-opus should have thinking modes")
	}

	// Check that each mode has ID and Name
	for i, mode := range modes {
		if mode.ID == "" {
			t.Errorf("mode %d missing ID", i)
		}
		if mode.Name == "" {
			t.Errorf("mode %d missing Name", i)
		}
	}

	// Verify specific modes exist
	modeIDs := make(map[string]bool)
	for _, mode := range modes {
		modeIDs[mode.ID] = true
	}

	// Claude Opus should have thinking budgets
	expectedBudgets := []string{"2048", "4096", "16384", "32768", "65536"}
	for _, budget := range expectedBudgets {
		if !modeIDs[budget] {
			t.Errorf("missing thinking budget %q from claude-opus modes", budget)
		}
	}
}

// TestThinkingModeDefaultClaudeHaiku tests that Haiku has no extended thinking
func TestThinkingModeDefaultClaudeHaiku(t *testing.T) {
	modes := supportedThinkingModesForModel("anthropic", "claude-haiku-4-5-20251001")
	if len(modes) != 1 {
		t.Fatalf("claude-haiku should have 1 mode, got %d", len(modes))
	}

	if modes[0].ID != "none" {
		t.Errorf("first mode ID = %q, want none", modes[0].ID)
	}

	if modes[0].Name != "None" {
		t.Errorf("first mode Name = %q, want None", modes[0].Name)
	}

	if modes[0].Description != "No extended thinking" {
		t.Errorf("first mode Description = %q, want No extended thinking", modes[0].Description)
	}
}

// TestThinkingModeDefaultGemini tests that Gemini models have thinking budgets
func TestThinkingModeDefaultGemini(t *testing.T) {
	modes := supportedThinkingModesForModel("gemini", "gemini-3.5-flash")
	if len(modes) != 5 {
		t.Fatalf("gemini should have 5 modes, got %d", len(modes))
	}

	expectedBudgets := []string{"512", "4096", "16384", "32768", "65536"}
	actualBudgets := make([]string, 0, len(modes))
	for _, m := range modes {
		actualBudgets = append(actualBudgets, m.ID)
	}

	for _, expected := range expectedBudgets {
		found := false
		for _, actual := range actualBudgets {
			if actual == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing budget %q from gemini modes", expected)
		}
	}
}

// TestThinkingModeDefaultOpenAI tests that OpenAI models have reasoning efforts
func TestThinkingModeDefaultOpenAI(t *testing.T) {
	modes := supportedThinkingModesForModel("openai", "gpt-5.5")
	if len(modes) != 6 {
		t.Fatalf("openai should have 6 modes, got %d", len(modes))
	}

	expectedEfforts := []string{"none", "minimal", "low", "medium", "high", "xhigh"}
	actualEfforts := make([]string, 0, len(modes))
	for _, m := range modes {
		actualEfforts = append(actualEfforts, m.ID)
	}

	for _, expected := range expectedEfforts {
		found := false
		for _, actual := range actualEfforts {
			if actual == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing effort %q from openai modes", expected)
		}
	}
}


// TestCurrentThinkingModeForModel_Better tests the current thinking mode lookup with better coverage
func TestCurrentThinkingModeForModel_Better(t *testing.T) {
	tests := []struct {
		name       string
		provider   string
		model      string
		setupLevel thinkingLevel // thinking level to set before test
		wantMode   string        // expected mode after setup
	}{
		{
			name:       "anthropic minimal level",
			provider:   "anthropic",
			model:      "claude-opus-4-8",
			setupLevel: thinkingMinimal,
			wantMode:   "2048", // minimal budget for Anthropic
		},
		{
			name:       "anthropic low level",
			provider:   "anthropic",
			model:      "claude-sonnet-4-6",
			setupLevel: thinkingLow,
			wantMode:   "4096", // low budget for Anthropic
		},
		{
			name:       "anthropic high level",
			provider:   "anthropic",
			model:      "claude-sonnet-4-6",
			setupLevel: thinkingHigh,
			wantMode:   "32768", // high budget for Anthropic
		},
		{
			name:       "openai minimal level",
			provider:   "openai",
			model:      "gpt-5.5",
			setupLevel: thinkingMinimal,
			wantMode:   "minimal", // minimal effort for OpenAI
		},
		{
			name:       "openai low level",
			provider:   "openai",
			model:      "gpt-5.5",
			setupLevel: thinkingLow,
			wantMode:   "low", // low effort for OpenAI
		},
		{
			name:       "openai high level",
			provider:   "openai",
			model:      "gpt-5.5",
			setupLevel: thinkingHigh,
			wantMode:   "high", // high effort for OpenAI
		},
		{
			name:       "gemini minimal level",
			provider:   "gemini",
			model:      "gemini-3.5-flash",
			setupLevel: thinkingMinimal,
			wantMode:   "512", // minimal budget for Gemini
		},
		{
			name:       "gemini low level",
			provider:   "gemini",
			model:      "gemini-3.5-flash",
			setupLevel: thinkingLow,
			wantMode:   "4096", // low budget for Gemini
		},
		{
			name:       "gemini high level",
			provider:   "gemini",
			model:      "gemini-3.5-flash",
			setupLevel: thinkingHigh,
			wantMode:   "32768", // high budget for Gemini
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save the thinking level first
			if tt.setupLevel != "" {
				if err := saveThinkingLevel(tt.setupLevel); err != nil {
					t.Fatalf("failed to setup thinking level %q: %v", tt.setupLevel, err)
				}
			}

			mode := currentThinkingModeForModel(tt.provider, tt.model)
			if mode != tt.wantMode {
				t.Errorf("currentThinkingModeForModel(%q, %q) = %q, want %q", tt.provider, tt.model, mode, tt.wantMode)
			}

			// Cleanup: reset to default
			if err := saveThinkingLevel(thinkingOff); err != nil {
				t.Logf("failed to cleanup thinking level: %v", err)
			}
		})
	}
}

// TestCurrentThinkingModeForUnknownModel tests behavior for unknown models
func TestCurrentThinkingModeForUnknownModel(t *testing.T) {
	tests := []struct {
		name     string
		provider string
	}{
		{"unknown provider", "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset thinking level to default
			if err := saveThinkingLevel(thinkingOff); err != nil {
				t.Fatalf("failed to reset thinking level: %v", err)
			}

			mode := currentThinkingModeForModel(tt.provider, "unknown-model")
			if mode != "none" {
				t.Errorf("current thinking mode for unknown model = %q, want none (safe default)", mode)
			}
		})
	}
}
