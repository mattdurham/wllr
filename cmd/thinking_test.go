package main

import (
	"testing"

	fantasyanthropicprovider "charm.land/fantasy/providers/anthropic"
	fantasygoogleprovider "charm.land/fantasy/providers/google"
	fantasyopenapiprovider "charm.land/fantasy/providers/openai"
)

func TestSaveThinkingLevel_RoundTrip(t *testing.T) {
	withConfigPath(t)

	if got := savedThinkingLevel(); got != thinkingOff {
		t.Errorf("savedThinkingLevel on missing file = %q, want off", got)
	}
	if err := saveThinkingLevel(thinkingHigh); err != nil {
		t.Fatalf("saveThinkingLevel: %v", err)
	}
	if got := savedThinkingLevel(); got != thinkingHigh {
		t.Errorf("savedThinkingLevel = %q, want high", got)
	}
	// An unknown persisted value falls back to off.
	if err := saveWllrField("thinking", "bogus"); err != nil {
		t.Fatalf("saveWllrField: %v", err)
	}
	if got := savedThinkingLevel(); got != thinkingOff {
		t.Errorf("savedThinkingLevel with bogus value = %q, want off", got)
	}
}

func TestSaveThinkingLevel_PreservesModel(t *testing.T) {
	withConfigPath(t)

	if err := saveModel("claude-opus-4-8"); err != nil {
		t.Fatalf("saveModel: %v", err)
	}
	if err := saveThinkingLevel(thinkingMedium); err != nil {
		t.Fatalf("saveThinkingLevel: %v", err)
	}
	if got := savedModel(); got != "claude-opus-4-8" {
		t.Errorf("model lost after saving thinking level: got %q", got)
	}
	if got := savedThinkingLevel(); got != thinkingMedium {
		t.Errorf("thinking level = %q, want medium", got)
	}
}

func TestIsValidThinkingLevel(t *testing.T) {
	for _, lvl := range thinkingLevels {
		if !isValidThinkingLevel(string(lvl)) {
			t.Errorf("%q should be valid", lvl)
		}
	}
	if isValidThinkingLevel("gigathink") {
		t.Error("gigathink should be invalid")
	}
}

func TestProviderOptionsForThinking_Anthropic(t *testing.T) {
	// Off → nil (clears options).
	if po := providerOptionsForThinkingMode(providerAnthropic, "", "claude-x"); po != nil {
		t.Errorf("anthropic off = %v, want nil", po)
	}
	// High → budget tokens set.
	po := providerOptionsForThinkingMode(providerAnthropic, "32768", "claude-x")
	data, ok := po[fantasyanthropicprovider.Name]
	if !ok {
		t.Fatalf("anthropic high: no anthropic options, got %v", po)
	}
	opts, ok := data.(*fantasyanthropicprovider.ProviderOptions)
	if !ok || opts.Thinking == nil {
		t.Fatalf("anthropic high: unexpected options %T", data)
	}
	if opts.Thinking.BudgetTokens != anthropicBudgetForThinkingMode("32768") {
		t.Errorf("budget = %d, want %d", opts.Thinking.BudgetTokens, anthropicBudgetForThinkingMode("32768"))
	}
}

func TestProviderOptionsForThinking_OpenAI(t *testing.T) {
	withAuthPath(t) // hermetic: no stored OAuth credential
	// Unknown/off mode → nil (clears options).
	if po := providerOptionsForThinkingMode(providerOpenAI, "", "gpt-4o"); po != nil {
		t.Errorf("openai empty mode = %v, want nil", po)
	}
	// The "none" mode is explicit for openai: ReasoningEffort=none, in the
	// responses option type (gpt-4o is in fantasy's responses ID list).
	po := providerOptionsForThinkingMode(providerOpenAI, thinkingModeNone, "gpt-4o")
	data, ok := po[fantasyopenapiprovider.Name]
	if !ok {
		t.Fatalf("openai none: no openai options, got %v", po)
	}
	noneOpts, ok := data.(*fantasyopenapiprovider.ResponsesProviderOptions)
	if !ok || noneOpts.ReasoningEffort == nil ||
		*noneOpts.ReasoningEffort != fantasyopenapiprovider.ReasoningEffortNone {
		t.Fatalf("openai none: unexpected options %T", data)
	}
	// A standard effort maps through, in the responses option type.
	po = providerOptionsForThinkingMode(providerOpenAI, thinkingModeMedium, "gpt-4o")
	data, ok = po[fantasyopenapiprovider.Name]
	if !ok {
		t.Fatalf("openai medium: no openai options, got %v", po)
	}
	opts, ok := data.(*fantasyopenapiprovider.ResponsesProviderOptions)
	if !ok || opts.ReasoningEffort == nil {
		t.Fatalf("openai medium: unexpected options %T", data)
	}
	if *opts.ReasoningEffort != fantasyopenapiprovider.ReasoningEffortMedium {
		t.Errorf("effort = %q, want medium", *opts.ReasoningEffort)
	}
}

func TestProviderOptionsForThinking_Gemini(t *testing.T) {
	if po := providerOptionsForThinkingMode(providerGemini, "", "gemini-x"); po != nil {
		t.Errorf("gemini off = %v, want nil", po)
	}
	po := providerOptionsForThinkingMode(providerGemini, "16384", "gemini-x")
	data, ok := po[fantasygoogleprovider.Name]
	if !ok {
		t.Fatalf("gemini medium: no google options, got %v", po)
	}
	opts, ok := data.(*fantasygoogleprovider.ProviderOptions)
	if !ok || opts.ThinkingConfig == nil || opts.ThinkingConfig.ThinkingBudget == nil {
		t.Fatalf("gemini low: unexpected options %T", data)
	}
	if *opts.ThinkingConfig.ThinkingBudget != geminiThinkingBudget[thinkingMedium] {
		t.Errorf("budget = %d, want %d", *opts.ThinkingConfig.ThinkingBudget, geminiThinkingBudget[thinkingMedium])
	}
}

func TestProviderOptionsForThinking_UnknownProvider(t *testing.T) {
	if po := providerOptionsForThinkingMode("mystery", "32768", ""); po != nil {
		t.Errorf("unknown provider = %v, want nil", po)
	}
}

func TestCurrentThinkingModeForModel_OpenRouter(t *testing.T) {
	withConfigPath(t)
	// Levels map to OpenRouter mode IDs; the openai-only extremes degrade to
	// their nearest supported effort (same mapping tiers use).
	for _, tc := range []struct {
		level, want string
	}{
		{string(thinkingOff), thinkingModeNone},
		{string(thinkingLow), "low"},
		{string(thinkingMedium), "medium"},
		{string(thinkingHigh), "high"},
		{string(thinkingMinimal), "low"},
		{string(thinkingXHigh), "high"},
		{"bogus", thinkingModeNone},
	} {
		if err := saveThinkingLevel(thinkingLevel(tc.level)); err != nil {
			t.Fatalf("saveThinkingLevel(%q): %v", tc.level, err)
		}
		if got := currentThinkingModeForModel(providerOpenRouter, "m"); got != tc.want {
			t.Errorf("currentThinkingModeForModel(openrouter) for level %q = %q, want %q", tc.level, got, tc.want)
		}
	}
}
