package main

import (
	"os"
	"strings"
	"testing"
)

func TestModelTierRoundTrip(t *testing.T) {
	withConfigPath(t)

	if tiers := loadModelTiers(); len(tiers) != 0 {
		t.Fatalf("loadModelTiers on missing file = %v, want empty", tiers)
	}
	if err := setModelTier("high", providerAnthropic, "claude-opus-4-8"); err != nil {
		t.Fatalf("setModelTier: %v", err)
	}
	tier, ok := savedModelTier("high")
	if !ok {
		t.Fatal("savedModelTier(high) not found")
	}
	if tier.Provider != providerAnthropic || tier.Model != "claude-opus-4-8" {
		t.Fatalf("tier = %+v", tier)
	}
	// Lookup is case-insensitive.
	if _, ok := savedModelTier("HIGH"); !ok {
		t.Error("savedModelTier should be case-insensitive")
	}
	if err := clearModelTier("high"); err != nil {
		t.Fatalf("clearModelTier: %v", err)
	}
	if _, ok := savedModelTier("high"); ok {
		t.Error("tier should be cleared")
	}
	// Clearing an absent tier is a no-op.
	if err := clearModelTier("missing"); err != nil {
		t.Errorf("clearModelTier(absent) = %v, want nil", err)
	}
}

func TestModelTierStringShorthand(t *testing.T) {
	path := withConfigPath(t)
	if err := os.WriteFile(path, []byte(`{"wllr":{"model_tiers":{"low":"qwen3"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	tier, ok := savedModelTier("low")
	if !ok {
		t.Fatal("low tier not parsed")
	}
	if tier.Model != "qwen3" || tier.Provider != "" {
		t.Fatalf("shorthand tier = %+v, want model qwen3 and empty provider", tier)
	}
}

func TestModelTierYAMLConfigPreserved(t *testing.T) {
	path := withConfigPath(t)
	yaml := `wllr:
  provider: anthropic
  custom_key: keepme
  model_tiers:
    high:
      provider: anthropic
      model: claude-opus-4-8
      thinking: high
`
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	// The documented config format is YAML; it must load even though the file
	// is not JSON.
	tier, ok := savedModelTier("high")
	if !ok {
		t.Fatal("high tier not parsed from YAML config")
	}
	if tier.Thinking != "high" {
		t.Fatalf("thinking = %q, want high", tier.Thinking)
	}
	if got := savedWllrField("provider"); got != providerAnthropic {
		t.Fatalf("provider from YAML = %q", got)
	}
	// Writing must not clobber the surrounding YAML keys or other groups.
	if err := setModelTier("low", providerLocal, "qwen3"); err != nil {
		t.Fatal(err)
	}
	if got := savedWllrField("custom_key"); got != "keepme" {
		t.Errorf("custom_key after write = %q, want keepme", got)
	}
	if _, ok := savedModelTier("high"); !ok {
		t.Error("high tier lost after writing low")
	}
}

func TestResolveModelTierCrossProvider(t *testing.T) {
	path := withConfigPath(t)
	if err := os.WriteFile(path, []byte(`{"wllr":{"model_tiers":{
		"high":{"provider":"anthropic","model":"claude-opus-4-8","thinking":"high"},
		"low":{"provider":"local","model":"worker"}
	},"local_models":[{"id":"worker","base_url":"http://127.0.0.1:9/v1"}]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := &Config{
		Provider:    providerAnthropic,
		Model:       "claude-sonnet-4-6",
		LocalModels: []localModelConfig{{ID: "worker", BaseURL: "http://127.0.0.1:9/v1"}},
	}

	provider, model, thinking, err := resolveModelTier("high", cfg.Provider, cfg)
	if err != nil {
		t.Fatalf("resolve high: %v", err)
	}
	if provider != providerAnthropic || model != "claude-opus-4-8" || thinking != "high" {
		t.Fatalf("high = %s/%s/%s", provider, model, thinking)
	}

	provider, model, _, err = resolveModelTier("low", cfg.Provider, cfg)
	if err != nil {
		t.Fatalf("resolve low: %v", err)
	}
	if provider != providerLocal || model != "worker" {
		t.Fatalf("low = %s/%s, want local/worker", provider, model)
	}
}

func TestResolveModelTierErrors(t *testing.T) {
	path := withConfigPath(t)
	if err := os.WriteFile(path, []byte(`{"wllr":{"model_tiers":{
		"low":{"provider":"local","model":"missing"},
		"empty":{"provider":"local","model":""},
		"bad":{"provider":"nope","model":"x"}
	},"local_models":[{"id":"worker","base_url":"http://127.0.0.1:9/v1"}]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := &Config{
		Provider:    providerAnthropic,
		LocalModels: []localModelConfig{{ID: "worker", BaseURL: "http://127.0.0.1:9/v1"}},
	}
	if _, _, _, err := resolveModelTier("absent", cfg.Provider, cfg); err == nil {
		t.Error("unknown tier should error")
	}
	// A local model absent from local_models is not itself an error: /models
	// also lists endpoint-discovered models, and tagging one must stay usable.
	// Whether it is reachable is settled when the tier is applied.
	if _, model, _, err := resolveModelTier("low", cfg.Provider, cfg); err != nil {
		t.Errorf("discovered local tier should resolve, got %v", err)
	} else if model != "missing" {
		t.Errorf("model = %q, want missing", model)
	}
	if _, _, _, err := resolveModelTier("bad", cfg.Provider, cfg); err == nil {
		t.Error("unknown provider tier should error")
	}
	if _, _, _, err := resolveModelTier("empty", cfg.Provider, cfg); err == nil {
		t.Error("tier with no model should error")
	}
}

func TestTiersForModel(t *testing.T) {
	path := withConfigPath(t)
	if err := os.WriteFile(path, []byte(`{"wllr":{"model_tiers":{
		"high":{"provider":"anthropic","model":"claude-opus-4-8"},
		"low":"claude-opus-4-8"
	}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	// The shorthand "low" entry has no provider and matches on any provider.
	got := tiersForModel(providerAnthropic, "claude-opus-4-8")
	if strings.Join(got, ",") != "high,low" {
		t.Fatalf("tiersForModel = %v, want [high low]", got)
	}
	if other := tiersForModel(providerOpenAI, "gpt-5.4"); len(other) != 0 {
		t.Fatalf("tiersForModel(other) = %v, want empty", other)
	}
}

func TestThinkingModeIDForLevel(t *testing.T) {
	cases := []struct {
		provider, level, want string
	}{
		{providerAnthropic, "high", "32768"},
		{providerAnthropic, "minimal", "2048"},
		{providerOpenAI, "high", "high"},
		{providerLocal, "low", "low"},
		{providerGemini, "medium", "16384"},
		{providerAnthropic, "off", ""},
		{providerAnthropic, "bogus", ""},
		{"unknown-provider", "high", ""},
	}
	for _, tc := range cases {
		if got := thinkingModeIDForLevel(tc.provider, "m", tc.level); got != tc.want {
			t.Errorf("thinkingModeIDForLevel(%s, %s) = %q, want %q", tc.provider, tc.level, got, tc.want)
		}
	}
}

func TestValidateTierThinking(t *testing.T) {
	if err := validateTierThinking(providerAnthropic, ""); err != nil {
		t.Errorf("empty level should be valid, got %v", err)
	}
	if err := validateTierThinking(providerAnthropic, "high"); err != nil {
		t.Errorf("high on anthropic should be valid, got %v", err)
	}
	if err := validateTierThinking(providerAnthropic, "nonsense"); err == nil {
		t.Error("unknown level should error")
	}
}

func TestModelTierLabelOmitsOffThinking(t *testing.T) {
	if got := modelTierLabel(modelTier{Provider: providerLocal, Model: "m", Thinking: "off"}); got != "local · m" {
		t.Errorf("label = %q, want thinking:off omitted", got)
	}
	if got := modelTierLabel(modelTier{Model: "m", Thinking: "high"}); got != "m · thinking:high" {
		t.Errorf("label = %q, want thinking:high", got)
	}
}
