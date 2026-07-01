package main

import (
	"charm.land/fantasy"
	fantasyanthropicprovider "charm.land/fantasy/providers/anthropic"
	fantasygoogleprovider "charm.land/fantasy/providers/google"
	fantasyopenapiprovider "charm.land/fantasy/providers/openai"
)

// Thinking levels are a provider-agnostic tier that maps to each provider's
// native reasoning mechanism:
//   - Anthropic → thinking.budget_tokens (int)
//   - OpenAI    → reasoning_effort (none/minimal/low/medium/high/xhigh)
//   - Gemini    → ThinkingConfig.thinking_budget (int)
//
// The level names mirror pi's set so behavior is familiar across harnesses.

// thinkingLevel is a named reasoning tier selectable via /thinking.
type thinkingLevel string

const (
	thinkingOff     thinkingLevel = "off"
	thinkingMinimal thinkingLevel = "minimal"
	thinkingLow     thinkingLevel = "low"
	thinkingMedium  thinkingLevel = "medium"
	thinkingHigh    thinkingLevel = "high"
	thinkingXHigh   thinkingLevel = "xhigh"
)

// thinkingLevels is the ordered list of selectable levels (least → most).
var thinkingLevels = []thinkingLevel{
	thinkingOff,
	thinkingMinimal,
	thinkingLow,
	thinkingMedium,
	thinkingHigh,
	thinkingXHigh,
}

// thinkingLevelLabels gives a short human description per level for the picker.
var thinkingLevelLabels = map[thinkingLevel]string{
	thinkingOff:     "Off — no extended thinking",
	thinkingMinimal: "Minimal — a little reasoning",
	thinkingLow:     "Low",
	thinkingMedium:  "Medium",
	thinkingHigh:    "High",
	thinkingXHigh:   "X-High — maximum reasoning",
}

// anthropicThinkingBudget maps a level to Anthropic extended-thinking token
// budgets. 0 means thinking is disabled (no ThinkingProviderOption emitted).
var anthropicThinkingBudget = map[thinkingLevel]int64{
	thinkingOff:     0,
	thinkingMinimal: 2_048,
	thinkingLow:     4_096,
	thinkingMedium:  16_384,
	thinkingHigh:    32_768,
	thinkingXHigh:   65_536,
}

// geminiThinkingBudget maps a level to Gemini thinking-budget tokens. 0 disables.
var geminiThinkingBudget = map[thinkingLevel]int64{
	thinkingOff:     0,
	thinkingMinimal: 512,
	thinkingLow:     4_096,
	thinkingMedium:  16_384,
	thinkingHigh:    32_768,
	thinkingXHigh:   65_536,
}

// openAIReasoningEffort maps a level to OpenAI reasoning_effort values. The
// bool is false for thinkingOff (no reasoning-effort option emitted).
var openAIReasoningEffort = map[thinkingLevel]fantasyopenapiprovider.ReasoningEffort{
	thinkingOff:     fantasyopenapiprovider.ReasoningEffortNone,
	thinkingMinimal: fantasyopenapiprovider.ReasoningEffortMinimal,
	thinkingLow:     fantasyopenapiprovider.ReasoningEffortLow,
	thinkingMedium:  fantasyopenapiprovider.ReasoningEffortMedium,
	thinkingHigh:    fantasyopenapiprovider.ReasoningEffortHigh,
	thinkingXHigh:   fantasyopenapiprovider.ReasoningEffortXHigh,
}

// isValidThinkingLevel reports whether s names a known level.
func isValidThinkingLevel(s string) bool {
	_, ok := thinkingLevelLabels[thinkingLevel(s)]
	return ok
}

// savedThinkingLevel returns the persisted thinking level, or thinkingOff if
// none is stored or the stored value is unknown.
func savedThinkingLevel() thinkingLevel {
	if s := savedWllrField("thinking"); isValidThinkingLevel(s) {
		return thinkingLevel(s)
	}
	return thinkingOff
}

// saveThinkingLevel persists the thinking level to the "wllr" config group.
func saveThinkingLevel(level thinkingLevel) error {
	return saveWllrField("thinking", string(level))
}

// providerOptionsForThinking builds the fantasy provider options for the given
// provider and level. Returns nil when the level is Off or the provider has no
// reasoning mechanism, which clears any previously-set thinking options.
func providerOptionsForThinking(provider string, level thinkingLevel) fantasy.ProviderOptions {
	switch provider {
	case providerAnthropic:
		budget := anthropicThinkingBudget[level]
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
	case "openai":
		if level == thinkingOff {
			return nil
		}
		effort := openAIReasoningEffort[level]
		return fantasy.ProviderOptions{
			fantasyopenapiprovider.Name: &fantasyopenapiprovider.ProviderOptions{
				ReasoningEffort: fantasyopenapiprovider.ReasoningEffortOption(effort),
			},
		}
	case "gemini":
		budget := geminiThinkingBudget[level]
		if budget <= 0 {
			return nil
		}
		return fantasy.ProviderOptions{
			fantasygoogleprovider.Name: &fantasygoogleprovider.ProviderOptions{
				ThinkingConfig: &fantasygoogleprovider.ThinkingConfig{
					ThinkingBudget: &budget,
				},
			},
		}
	default:
		return nil
	}
}
