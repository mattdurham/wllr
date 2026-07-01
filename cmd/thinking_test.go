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
	if po := providerOptionsForThinking(providerAnthropic, thinkingOff); po != nil {
		t.Errorf("anthropic off = %v, want nil", po)
	}
	// High → budget tokens set.
	po := providerOptionsForThinking(providerAnthropic, thinkingHigh)
	data, ok := po[fantasyanthropicprovider.Name]
	if !ok {
		t.Fatalf("anthropic high: no anthropic options, got %v", po)
	}
	opts, ok := data.(*fantasyanthropicprovider.ProviderOptions)
	if !ok || opts.Thinking == nil {
		t.Fatalf("anthropic high: unexpected options %T", data)
	}
	if opts.Thinking.BudgetTokens != anthropicThinkingBudget[thinkingHigh] {
		t.Errorf("budget = %d, want %d", opts.Thinking.BudgetTokens, anthropicThinkingBudget[thinkingHigh])
	}
}

func TestProviderOptionsForThinking_OpenAI(t *testing.T) {
	if po := providerOptionsForThinking("openai", thinkingOff); po != nil {
		t.Errorf("openai off = %v, want nil", po)
	}
	po := providerOptionsForThinking("openai", thinkingMedium)
	data, ok := po[fantasyopenapiprovider.Name]
	if !ok {
		t.Fatalf("openai medium: no openai options, got %v", po)
	}
	opts, ok := data.(*fantasyopenapiprovider.ProviderOptions)
	if !ok || opts.ReasoningEffort == nil {
		t.Fatalf("openai medium: unexpected options %T", data)
	}
	if *opts.ReasoningEffort != fantasyopenapiprovider.ReasoningEffortMedium {
		t.Errorf("effort = %q, want medium", *opts.ReasoningEffort)
	}
}

func TestProviderOptionsForThinking_Gemini(t *testing.T) {
	if po := providerOptionsForThinking("gemini", thinkingOff); po != nil {
		t.Errorf("gemini off = %v, want nil", po)
	}
	po := providerOptionsForThinking("gemini", thinkingLow)
	data, ok := po[fantasygoogleprovider.Name]
	if !ok {
		t.Fatalf("gemini low: no google options, got %v", po)
	}
	opts, ok := data.(*fantasygoogleprovider.ProviderOptions)
	if !ok || opts.ThinkingConfig == nil || opts.ThinkingConfig.ThinkingBudget == nil {
		t.Fatalf("gemini low: unexpected options %T", data)
	}
	if *opts.ThinkingConfig.ThinkingBudget != geminiThinkingBudget[thinkingLow] {
		t.Errorf("budget = %d, want %d", *opts.ThinkingConfig.ThinkingBudget, geminiThinkingBudget[thinkingLow])
	}
}

func TestProviderOptionsForThinking_UnknownProvider(t *testing.T) {
	if po := providerOptionsForThinking("mystery", thinkingHigh); po != nil {
		t.Errorf("unknown provider = %v, want nil", po)
	}
}
