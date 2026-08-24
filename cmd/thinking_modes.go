package main

import (
	"charm.land/fantasy"
	fantasyanthropicprovider "charm.land/fantasy/providers/anthropic"
	fantasygoogleprovider "charm.land/fantasy/providers/google"
	fantasyopenapiprovider "charm.land/fantasy/providers/openai"
)

// providerOptionsForThinkingMode builds the fantasy provider options for the
// given provider and model-specific thinking mode ID. Returns nil when the
// provider has no reasoning mechanism (or the mode clears reasoning on a
// provider that signals it by omission), which clears any previously-set
// thinking options.
//
// openai and local share the OpenAI reasoning_effort wire vocabulary, but the
// emission policy differs: openai omits the field for "none"/unknown (native
// default applies), local always emits it (the server would otherwise keep
// its own default — LM Studio's is "on" for loaded thinking models).
func providerOptionsForThinkingMode(provider, modeID string) fantasy.ProviderOptions {
	switch provider {
	case providerAnthropic:
		budget := anthropicBudgetForThinkingMode(modeID)
		if budget <= 0 {
			return nil
		}
		return fantasy.ProviderOptions{
			fantasyanthropicprovider.Name: &fantasyanthropicprovider.ProviderOptions{
				Thinking: &fantasyanthropicprovider.ThinkingProviderOption{
					BudgetTokens: budget,
				},
			},
		}
	case providerOpenAI:
		// Native OpenAI (incl. Codex): the standard effort IDs map through
		// verbatim, including "none" (which maps to an explicit
		// ReasoningEffort=none — a documented value that turns reasoning off).
		// Only unknown/stale IDs (e.g. a saved mode from another model) hit the
		// nil branch and omit the field, so they can never 400 a request. This
		// is the pre-existing behavior; the codex/openai path is unchanged.
		effort := openAIReasoningEffortForThinkingMode(modeID)
		if effort == nil {
			return nil
		}
		return fantasy.ProviderOptions{
			fantasyopenapiprovider.Name: &fantasyopenapiprovider.ProviderOptions{
				ReasoningEffort: effort,
			},
		}
	case providerLocal:
		// Local (OpenAI-compatible) endpoints always get the effort, including
		// an explicit "none": an omitted field leaves the server on its own
		// default (LM Studio defaults loaded thinking models to "on"), so an
		// explicit "none" is the only way to actually disable. Unlike openai,
		// unknown/stale IDs (e.g. the boolean "on") are sent as "none" —
		// disabling rather than 400-ing (the endpoint rejects anything outside
		// the six OpenAI values).
		effort := openAIReasoningEffortForThinkingMode(modeID)
		if effort == nil {
			none := fantasyopenapiprovider.ReasoningEffortNone
			effort = &none
		}
		return fantasy.ProviderOptions{
			fantasyopenapiprovider.Name: &fantasyopenapiprovider.ProviderOptions{
				ReasoningEffort: effort,
			},
		}
	case providerGemini:
		budget := geminiBudgetForThinkingMode(modeID)
		if budget == nil || *budget <= 0 {
			return nil
		}
		return fantasy.ProviderOptions{
			fantasygoogleprovider.Name: &fantasygoogleprovider.ProviderOptions{
				ThinkingConfig: &fantasygoogleprovider.ThinkingConfig{
					ThinkingBudget: budget,
				},
			},
		}
	default:
		return nil
	}
}

// anthropicBudgetForThinkingMode maps a thinking mode ID to an Anthropic
// budget token value. Returns 0 if the mode is unknown or off.
func anthropicBudgetForThinkingMode(modeID string) int64 {
	// Anthropic uses numeric token budgets as mode IDs
	switch modeID {
	case "2048":
		return 2_048
	case "4096":
		return 4_096
	case "16384":
		return 16_384
	case "32768":
		return 32_768
	case "65536":
		return 65_536
	default:
		return 0 // unknown or off
	}
}

// geminiBudgetForThinkingMode maps a thinking mode ID to a Gemini budget
// token value. Returns nil if the mode is unknown or off.
func geminiBudgetForThinkingMode(modeID string) *int64 {
	// Gemini uses numeric token budgets as mode IDs
	switch modeID {
	case "512":
		val := int64(512)
		return &val
	case "4096":
		val := int64(4_096)
		return &val
	case "16384":
		val := int64(16_384)
		return &val
	case "32768":
		val := int64(32_768)
		return &val
	case "65536":
		val := int64(65_536)
		return &val
	default:
		return nil // unknown or off
	}
}

// openAIReasoningEffortForThinkingMode maps a thinking mode ID to an OpenAI
// reasoning effort value. Returns nil if the mode is unknown or off.
func openAIReasoningEffortForThinkingMode(modeID string) *fantasyopenapiprovider.ReasoningEffort {
	// OpenAI uses named effort levels as mode IDs
	switch modeID {
	case "none":
		val := fantasyopenapiprovider.ReasoningEffortNone
		return &val
	case "minimal":
		val := fantasyopenapiprovider.ReasoningEffortMinimal
		return &val
	case "low":
		val := fantasyopenapiprovider.ReasoningEffortLow
		return &val
	case "medium":
		val := fantasyopenapiprovider.ReasoningEffortMedium
		return &val
	case "high":
		val := fantasyopenapiprovider.ReasoningEffortHigh
		return &val
	case "xhigh":
		val := fantasyopenapiprovider.ReasoningEffortXHigh
		return &val
	default:
		return nil // unknown or off
	}
}

// savedThinkingMode returns the persisted thinking mode ID, or empty string if
// none is stored or the stored value is unknown.
func savedThinkingMode() string {
	return savedWllrField("thinking_mode")
}

// saveThinkingMode persists the thinking mode ID to the "wllr" config group.
func saveThinkingMode(modeID string) error {
	return saveWllrField("thinking_mode", modeID)
}
