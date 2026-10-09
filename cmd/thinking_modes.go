package main

import (
	"charm.land/fantasy"
	fantasyanthropicprovider "charm.land/fantasy/providers/anthropic"
	fantasygoogleprovider "charm.land/fantasy/providers/google"
	fantasyopenapiprovider "charm.land/fantasy/providers/openai"
	fantasyopenrouterprovider "charm.land/fantasy/providers/openrouter"
)

// openAIUsesResponsesAPI reports whether a model on the openai provider is
// served by the Responses API, which decides the provider-options wire type
// (see providerOptionsForThinkingMode). Two constructions route differently:
// a stored ChatGPT OAuth token builds newCodexProvider, which forces Responses
// for every model ID; a plain API key uses fantasy's default gate.
func openAIUsesResponsesAPI(modelID string) bool {
	if cred, ok := loadAuthCredential(providerOpenAI); ok && cred.Type == authTypeOAuth {
		return true
	}
	return fantasyopenapiprovider.IsResponsesModel(modelID)
}

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
func providerOptionsForThinkingMode(provider, modeID, modelID string) fantasy.ProviderOptions {
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
		// nil branch and omit the field, so they can never 400 a request.
		effort := openAIReasoningEffortForThinkingMode(modeID)
		if effort == nil {
			return nil
		}
		// The option type must match the model's wire format: fantasy's
		// Responses-API model reads *ResponsesProviderOptions and silently
		// ignores any other value under the openai key, while the
		// chat-completions model reads *ProviderOptions. Sending the chat type
		// for a responses model drops reasoning_effort without an error —
		// exactly the silent no-op this branching prevents.
		if openAIUsesResponsesAPI(modelID) {
			return fantasy.ProviderOptions{
				fantasyopenapiprovider.Name: &fantasyopenapiprovider.ResponsesProviderOptions{
					ReasoningEffort: effort,
				},
			}
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
		// the six OpenAI values). Local endpoints speak chat completions, so
		// the chat-completions option type is always correct here.
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
	case providerOpenRouter:
		// OpenRouter normalizes reasoning across upstreams into one object:
		// effort (low/medium/high) plus an enabled flag. "none" maps to
		// enabled:false — the documented way to disable reasoning; an omitted
		// object would leave a thinking model on its own default, which is
		// on. Only the documented effort IDs map; anything else (e.g. a mode
		// saved while another provider was active) returns nil and omits the
		// object entirely, so a stale mode can never 400 a request.
		reasoning := openRouterReasoningForThinkingMode(modeID)
		if reasoning == nil {
			return nil
		}
		return fantasy.ProviderOptions{
			fantasyopenrouterprovider.Name: &fantasyopenrouterprovider.ProviderOptions{
				Reasoning: reasoning,
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

// openRouterReasoningForThinkingMode maps a thinking mode ID to OpenRouter
// reasoning options. "none" becomes enabled:false, the documented way to turn
// reasoning off for models that always reason; the effort IDs pass through
// verbatim. Returns nil when the mode is outside OpenRouter's documented
// vocabulary (which omits the reasoning object entirely).
func openRouterReasoningForThinkingMode(modeID string) *fantasyopenrouterprovider.ReasoningOptions {
	switch modeID {
	case thinkingModeNone:
		enabled := false
		return &fantasyopenrouterprovider.ReasoningOptions{Enabled: &enabled}
	case thinkingModeLow, thinkingModeMedium, thinkingModeHigh:
		effort := fantasyopenrouterprovider.ReasoningEffort(modeID)
		return &fantasyopenrouterprovider.ReasoningOptions{
			Effort: fantasyopenrouterprovider.ReasoningEffortOption(effort),
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
	if effort, ok := openAIReasoningEffortByMode[modeID]; ok {
		val := effort
		return &val
	}
	return nil // unknown or off
}

// thinkingModeKey is the config key under which a model's thinking mode is
// saved: "provider:model". Provider IDs never contain colons, so the key
// round-trips unambiguously even though model IDs may contain slashes.
func thinkingModeKey(provider, model string) string {
	return provider + ":" + model
}

// savedThinkingMode returns the thinking mode persisted for a provider/model.
// Models without their own entry fall back to the legacy single
// "thinking_mode" key: configs written before per-model storage, and models
// never explicitly set, keep the last-saved mode. Empty string if nothing
// applies.
func savedThinkingMode(provider, model string) string {
	settings := loadWllrSettings()
	if mode, ok := settings.ThinkingModes[thinkingModeKey(provider, model)]; ok {
		return mode
	}
	return settings.ThinkingMode
}

// saveThinkingMode persists the thinking mode ID for one provider/model to
// the "thinking_modes" map in the "wllr" config group; an empty modeID clears
// that model's entry. The legacy global "thinking_mode" key is mirrored with
// the saved value so models without their own entry keep inheriting the most
// recently chosen mode.
func saveThinkingMode(provider, model, modeID string) error {
	modes := loadWllrSettings().ThinkingModes
	key := thinkingModeKey(provider, model)
	if modeID == "" {
		delete(modes, key)
	} else {
		if modes == nil {
			modes = map[string]string{}
		}
		modes[key] = modeID
	}
	if len(modes) == 0 {
		if err := removeWllrField("thinking_modes"); err != nil {
			return err
		}
	} else if err := saveWllrRawField("thinking_modes", modes); err != nil {
		return err
	}
	return saveWllrField("thinking_mode", modeID)
}
