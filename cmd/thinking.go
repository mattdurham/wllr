package main

import (
	"charm.land/fantasy"
	fantasyanthropicprovider "charm.land/fantasy/providers/anthropic"
	fantasygoogleprovider "charm.land/fantasy/providers/google"
	fantasyopenapiprovider "charm.land/fantasy/providers/openai"
	fantasyopenrouterprovider "charm.land/fantasy/providers/openrouter"
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

// openAIReasoningEffortByMode maps standard OpenAI reasoning-effort mode IDs
// (shared by the openai and local providers) to their wire values. The map is
// the single source of truth for the standard vocabulary; mode IDs outside it
// (e.g. LM Studio's boolean "on") map to nil, which omits the field and lets
// the server apply its own default.
var openAIReasoningEffortByMode = map[string]fantasyopenapiprovider.ReasoningEffort{
	thinkingModeNone:    fantasyopenapiprovider.ReasoningEffortNone,
	thinkingModeMinimal: fantasyopenapiprovider.ReasoningEffortMinimal,
	thinkingModeLow:     fantasyopenapiprovider.ReasoningEffortLow,
	thinkingModeMedium:  fantasyopenapiprovider.ReasoningEffortMedium,
	thinkingModeHigh:    fantasyopenapiprovider.ReasoningEffortHigh,
	thinkingModeXHigh:   fantasyopenapiprovider.ReasoningEffortXHigh,
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
	case providerOpenAI, providerLocal:
		if level == thinkingOff {
			return nil
		}
		effort := openAIReasoningEffort[level]
		return fantasy.ProviderOptions{
			fantasyopenapiprovider.Name: &fantasyopenapiprovider.ProviderOptions{
				ReasoningEffort: &effort,
			},
		}
	case providerGemini:
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

// OpenRouter provider-routing options.
//
// OpenRouter routes one model across several upstream providers and lets the
// request express a preference. Fantasy exposes this as ProviderOptions.Provider
// .Sort, whose value is one of "price", "throughput", or "latency"; OpenRouter
// also documents the ":floor" and ":nitro" shortcuts, which force the
// cheapest/least-expensive and the fastest provider respectively.
//
// This is separate from the model choice: the same OpenRouter model can be
// served by different upstreams at different prices and speeds.
const (
	// openRouterSpeedDefault lets OpenRouter route with its own default
	// (currently a balanced price/uptime preference). No sort is sent.
	openRouterSpeedDefault = "default"
	// openRouterSpeedFloor prefers the lowest price.
	openRouterSpeedFloor = "floor"
	// openRouterSpeedNitro prefers the highest throughput.
	openRouterSpeedNitro = "nitro"
	// openRouterSpeedPrice and the two below are the explicit sort keys.
	openRouterSpeedPrice      = "price"
	openRouterSpeedThroughput = "throughput"
	openRouterSpeedLatency    = "latency"
)

// openRouterSpeedOptions is the selectable set, in picker order, for the
// active provider's routing preference. ID is stored in config; Label is shown
// in the picker; Sort is the OpenRouter value sent ("" sends nothing).
type openRouterSpeedOption struct {
	ID          string
	Label       string
	Description string
	Sort        string
}

// openRouterSpeedOptions lists the routing preferences offered by
// /openrouter-speed. "default" is first so a user can return to OpenRouter's
// own routing after picking a preference.
var openRouterSpeedOptions = []openRouterSpeedOption{
	{ID: openRouterSpeedDefault, Label: "Default", Description: "OpenRouter's own balanced routing"},
	{ID: openRouterSpeedFloor, Label: "Floor", Description: "cheapest available provider (price)", Sort: openRouterSpeedPrice},
	{ID: openRouterSpeedNitro, Label: "Nitro", Description: "fastest provider (throughput)", Sort: openRouterSpeedThroughput},
	{ID: openRouterSpeedPrice, Label: "Price", Description: "sort by price", Sort: openRouterSpeedPrice},
	{ID: openRouterSpeedThroughput, Label: "Throughput", Description: "sort by tokens/sec", Sort: openRouterSpeedThroughput},
	{ID: openRouterSpeedLatency, Label: "Latency", Description: "sort by time to first token", Sort: openRouterSpeedLatency},
}

// openRouterSpeedSort maps a stored option ID to the OpenRouter sort value.
// An unknown or "default" ID yields "" (send no sort).
func openRouterSpeedSort(id string) string {
	for _, o := range openRouterSpeedOptions {
		if o.ID == id {
			return o.Sort
		}
	}
	return ""
}

// isValidOpenRouterSpeed reports whether id is a known routing option.
func isValidOpenRouterSpeed(id string) bool {
	if id == "" {
		return true
	}
	for _, o := range openRouterSpeedOptions {
		if o.ID == id {
			return true
		}
	}
	return false
}

// savedOpenRouterSpeed returns the persisted OpenRouter routing preference, or
// "" when none is stored (meaning OpenRouter's default routing).
func savedOpenRouterSpeed() string {
	id := savedWllrField("openrouter_speed")
	if !isValidOpenRouterSpeed(id) {
		return ""
	}
	return id
}

// saveOpenRouterSpeed persists the OpenRouter routing preference. The default
// clears the field so an unset preference stays unset in the config.
func saveOpenRouterSpeed(id string) error {
	if id == "" || id == openRouterSpeedDefault {
		return removeWllrField("openrouter_speed")
	}
	return saveWllrField("openrouter_speed", id)
}

// openRouterProviderOptions builds the OpenRouter provider options for the
// stored routing preference. Returns nil when there is nothing to apply, so a
// default preference clears any previously-set routing.
func openRouterProviderOptions() fantasy.ProviderOptions {
	sortKey := openRouterSpeedSort(savedOpenRouterSpeed())
	if sortKey == "" {
		return nil
	}
	return fantasy.ProviderOptions{
		fantasyopenrouterprovider.Name: &fantasyopenrouterprovider.ProviderOptions{
			Provider: &fantasyopenrouterprovider.Provider{Sort: &sortKey},
		},
	}
}

// providerOptionsForRuntime returns the provider options applied to the main
// agent: the reasoning selection plus any provider-routing preference. These
// occupy distinct fantasy keys (the reasoning struct vs the OpenRouter routing
// struct), so they merge instead of replacing one another — otherwise setting a
// speed would silently clear the reasoning mode and vice versa.
func providerOptionsForRuntime(provider, modeID string) fantasy.ProviderOptions {
	out := fantasy.ProviderOptions{}
	for k, v := range providerOptionsForThinkingMode(provider, modeID) {
		out[k] = v
	}
	if provider == providerOpenRouter {
		for k, v := range openRouterProviderOptions() {
			out[k] = v
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// speedDisplayFor returns the routing preference to show in the status bar for
// a provider: the stored value for OpenRouter, empty for everyone else (no
// other provider has a routing preference).
func speedDisplayFor(provider string) string {
	if provider != providerOpenRouter {
		return ""
	}
	return savedOpenRouterSpeed()
}
