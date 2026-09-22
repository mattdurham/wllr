package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"charm.land/fantasy"
	"github.com/mattdurham/wllr/modules/agent"
)

// Model tiers let the user tag provider/model pairs as a named cost or
// thinking tier (for example "high" for an expensive planning model and "low"
// for a cheap working model) and then reference the tier instead of the model.
// Skills declare a tier in frontmatter, sub-agents default to the working
// tier, and /model <tier> applies one interactively.
//
// Tiers are stored in the "wllr" config group under "model_tiers" as a map of
// tier name to {provider, model, thinking}. A tier may name a provider
// different from the active one, so a plan/research tier can be Anthropic
// while the working tier is a local model.
const (
	// tierHigh is the reserved tier name for the expensive planning model.
	tierHigh = "high"
	// tierLow is the reserved tier name for the cheaper working model used by
	// sub-agents when they do not name a model.
	tierLow = "low"
)

// reservedTierNames are the tier names with built-in behavior. Other names may
// be configured, but only these two are wired to picker keys and sub-agent
// defaults.
var reservedTierNames = []string{tierHigh, tierLow}

// modelTier is one named tier target. Provider may be empty, in which case the
// tier resolves against whatever provider is active when it is applied.
type modelTier struct {
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model"`
	Thinking string `json:"thinking,omitempty"`
}

// UnmarshalJSON accepts both the object form
// {"provider": "anthropic", "model": "claude-opus-4-8", "thinking": "high"} and
// the string shorthand "claude-opus-4-8", which sets only the model.
func (t *modelTier) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" || trimmed == "null" {
		return nil
	}
	if strings.HasPrefix(trimmed, "\"") {
		var model string
		if err := json.Unmarshal(data, &model); err != nil {
			return err
		}
		t.Model = strings.TrimSpace(model)
		return nil
	}
	type wire struct {
		Provider string `json:"provider"`
		Model    string `json:"model"`
		Thinking string `json:"thinking"`
	}
	var w wire
	if err := json.Unmarshal(data, &w); err != nil {
		return err
	}
	t.Provider = strings.TrimSpace(w.Provider)
	t.Model = strings.TrimSpace(w.Model)
	t.Thinking = strings.TrimSpace(w.Thinking)
	return nil
}

// normalizeTierName lowercases and trims a tier name for lookup.
func normalizeTierName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

// loadModelTiers returns the persisted tier map, or an empty map when none are
// configured or the config is unreadable.
func loadModelTiers() map[string]modelTier {
	tiers := loadWllrSettings().ModelTiers
	if tiers == nil {
		return map[string]modelTier{}
	}
	return tiers
}

// saveModelTiers persists the tier map to the "wllr" config group. An empty
// map removes the key so a config with no tiers stays clean.
func saveModelTiers(tiers map[string]modelTier) error {
	if len(tiers) == 0 {
		return removeWllrField("model_tiers")
	}
	return saveWllrRawField("model_tiers", tiers)
}

// savedModelTier returns one tier by name. Lookup is case-insensitive.
func savedModelTier(name string) (modelTier, bool) {
	tier, ok := loadModelTiers()[normalizeTierName(name)]
	return tier, ok
}

// setModelTier records provider/model for a tier and persists it.
func setModelTier(name, provider, model string) error {
	name = normalizeTierName(name)
	model = strings.TrimSpace(model)
	if name == "" {
		return fmt.Errorf("model tier: name is required")
	}
	if model == "" {
		return fmt.Errorf("model tier %q: model is required", name)
	}
	tiers := loadModelTiers()
	existing := tiers[name]
	tiers[name] = modelTier{Provider: strings.TrimSpace(provider), Model: model, Thinking: existing.Thinking}
	return saveModelTiers(tiers)
}

// clearModelTier removes a tier and persists the change. Removing an absent
// tier is a no-op.
func clearModelTier(name string) error {
	name = normalizeTierName(name)
	tiers := loadModelTiers()
	if _, ok := tiers[name]; !ok {
		return nil
	}
	delete(tiers, name)
	return saveModelTiers(tiers)
}

// tierNames returns the configured tier names, reserved names first, then any
// others alphabetically.
func tierNames() []string {
	tiers := loadModelTiers()
	seen := make(map[string]bool, len(tiers))
	out := make([]string, 0, len(tiers))
	for _, name := range reservedTierNames {
		if _, ok := tiers[name]; ok {
			out = append(out, name)
			seen[name] = true
		}
	}
	extra := make([]string, 0, len(tiers))
	for name := range tiers {
		if !seen[name] {
			extra = append(extra, name)
		}
	}
	sort.Strings(extra)
	return append(out, extra...)
}

// modelTierLabel renders a tier for picker sublabels and listings.
func modelTierLabel(t modelTier) string {
	parts := make([]string, 0, 3)
	if t.Provider != "" {
		parts = append(parts, t.Provider)
	}
	parts = append(parts, t.Model)
	if t.Thinking != "" && t.Thinking != thinkingOffLevel() {
		parts = append(parts, "thinking:"+t.Thinking)
	}
	return strings.Join(parts, " · ")
}

// thinkingOffLevel reports the level ID meaning "no reasoning", kept in one
// place so tier rendering and validation agree.
func thinkingOffLevel() string { return string(thinkingOff) }

// resolveModelTier maps a tier name to a concrete provider/model/thinking
// triple. An empty tier provider means "use activeProvider". The model is
// validated the same way an interactive selection is: local models must be
// configured, pinned OpenRouter models must exist, and catalog providers accept
// any non-empty ID so unpinned models stay usable.
func resolveModelTier(name, activeProvider string, cfg *Config) (provider, model, thinking string, err error) {
	tier, ok := savedModelTier(name)
	if !ok {
		return "", "", "", fmt.Errorf("no model tier named %q; tag one with /models", normalizeTierName(name))
	}
	provider = tier.Provider
	if provider == "" {
		provider = activeProvider
	}
	model = normalizeModelForProvider(provider, tier.Model)
	if model == "" {
		return "", "", "", fmt.Errorf("model tier %q has no model", normalizeTierName(name))
	}
	switch provider {
	case providerLocal:
		if cfg == nil {
			return "", "", "", fmt.Errorf(
				"model tier %q: local provider requires configuration",
				normalizeTierName(name),
			)
		}
		// A local tier model need not be pre-declared in local_models: /models
		// also lists models discovered from the endpoint, and tagging one must
		// stay usable. Whether the model is actually reachable (and its window)
		// is settled when the tier is applied, which can remember a discovered
		// model for the session — see ApplyModelTierFn.
	case providerOpenRouter:
		if cfg != nil {
			if _, ok := cfg.openRouterModel(model); !ok {
				return "", "", "", fmt.Errorf(
					"model tier %q: OpenRouter model %q is not in your saved models",
					normalizeTierName(name), model,
				)
			}
		}
	default:
		if modelsForProvider(provider) == nil {
			return "", "", "", fmt.Errorf("model tier %q: unknown provider %q", normalizeTierName(name), provider)
		}
	}
	return provider, model, tier.Thinking, nil
}

// validateTierThinking checks that a tier's thinking level is a known level the
// provider supports. An empty level is always valid: the tier then applies no
// thinking override and leaves the current reasoning selection in place.
func validateTierThinking(provider, level string) error {
	if level == "" {
		return nil
	}
	if !isValidThinkingLevel(level) {
		return fmt.Errorf("unknown thinking level %q", level)
	}
	if !providerSupportsThinking(provider) {
		return fmt.Errorf("provider %q does not support thinking levels", provider)
	}
	return nil
}

// providerSupportsThinking reports whether a provider has a reasoning mechanism
// that thinking levels map onto.
func providerSupportsThinking(provider string) bool {
	switch provider {
	case providerAnthropic, providerOpenAI, providerGemini, providerLocal:
		return true
	default:
		return false
	}
}

// tiersForModel returns the tier names currently tagged with provider/model, in
// reserved-then-alphabetical order. Used to render tags in the model picker.
func tiersForModel(provider, model string) []string {
	tiers := loadModelTiers()
	out := make([]string, 0, len(tiers))
	for _, name := range tierNames() {
		if tierMatches(tiers[name], provider, model) {
			out = append(out, name)
		}
	}
	return out
}

// tierMatches reports whether tier targets provider/model. A tier with no
// provider matches the model on any provider.
func tierMatches(tier modelTier, provider, model string) bool {
	if tier.Model != model {
		return false
	}
	if tier.Provider == "" {
		return true
	}
	return tier.Provider == provider
}

// tierLabels renders the configured tiers as "name: target" lines for /model
// tiers output.
func tierLabels() []string {
	tiers := loadModelTiers()
	names := tierNames()
	out := make([]string, 0, len(names))
	for _, name := range names {
		out = append(out, name+": "+modelTierLabel(tiers[name]))
	}
	return out
}

// activateProviderModel switches the live provider and main agent to
// provider/model without going through the interactive provider picker. It is
// the tier-application path: a tier may name a provider other than the active
// one, so the pool's provider is rebuilt from a copy of cfg rather than reusing
// whatever provider is already installed.
//
// Persisting the selection and updating m.activeProvider/activeModel is the
// caller's responsibility (the harness does it through
// setActiveProviderModel).
func activateProviderModel(
	ctx context.Context,
	cfg *Config,
	pool *agent.AgentPool,
	provider, modelID string,
) error {
	candidate := *cfg
	candidate.Provider = provider
	candidate.Model = modelID
	if provider == providerLocal && !candidate.applyLocalModelSelection(modelID) {
		return fmt.Errorf("local model %q is not configured in wllr.local_models", modelID)
	}
	prov, lm, err := buildProvider(ctx, &candidate)
	if err != nil {
		return err
	}
	pool.SetProvider(prov)
	pool.SetProviderName(provider)
	pool.SetDefaultModelName(modelID)
	if cw := contextWindowForSelection(provider, modelID, cfg); cw > 0 {
		pool.SetModelContextWindow(modelID, cw)
	}
	if main := pool.Get(agent.MainAgentID); main != nil {
		main.SetModel(lm, modelID, contextWindowForSelection(provider, modelID, cfg))
	}
	return nil
}

// thinkingModeIDForLevel maps a provider-agnostic thinking level (the names a
// tier can declare, e.g. "high") to the provider-specific mode ID the /thinking
// picker uses (Anthropic "32768", OpenAI "high"). Returns "" when the level is
// off or the provider has no reasoning mechanism, meaning no mode is applied.
func thinkingModeIDForLevel(provider, model, level string) string {
	lvl := thinkingLevel(level)
	if !isValidThinkingLevel(level) || lvl == thinkingOff {
		return ""
	}
	switch provider {
	case providerAnthropic:
		if budget := anthropicThinkingBudget[lvl]; budget > 0 {
			return fmt.Sprint(budget)
		}
	case providerOpenAI, providerLocal:
		// The OpenAI/local reasoning-effort vocabulary uses the level name as
		// the mode ID ("low", "medium", "high", …), so the level passes through.
		if _, ok := openAIReasoningEffort[lvl]; ok {
			return string(lvl)
		}
	case providerGemini:
		if budget := geminiThinkingBudget[lvl]; budget > 0 {
			return fmt.Sprint(budget)
		}
	}
	return ""
}

// modelForProviderTier builds a language model for provider/model without
// touching the live pool. It is the sub-agent tier path: a working tier may
// name a provider other than the session's, and spawning a sub-agent must not
// switch the session's provider.
func modelForProviderTier(ctx context.Context, cfg *Config, provider, modelID string) (fantasy.LanguageModel, error) {
	candidate := *cfg
	candidate.Provider = provider
	candidate.Model = modelID
	if provider == providerLocal && !candidate.applyLocalModelSelection(modelID) {
		return nil, fmt.Errorf("local model %q is not configured in wllr.local_models", modelID)
	}
	_, lm, err := buildProvider(ctx, &candidate)
	if err != nil {
		return nil, err
	}
	return lm, nil
}

// sessionModel resolves the pool's default (session) language model. It is the
// fallback for sub-agent spawning when no usable working tier is configured, so
// a missing or broken tier degrades to the previous behavior instead of
// failing the spawn.
func sessionModel(ctx context.Context, pool *agent.AgentPool) (fantasy.LanguageModel, string, error) {
	lm, err := pool.LanguageModelForModel(ctx, "")
	return lm, pool.DefaultModelName(), err
}
