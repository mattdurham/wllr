package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
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
	case providerAnthropic, providerOpenAI, providerGemini, providerLocal, providerOpenRouter:
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
// tier or skill can declare, e.g. "high") to the mode ID of the given
// model's OWN vocabulary. Resolution is per model because models name their
// thinking differently: the level passes through when the model's set uses
// it as a mode ID ("high"), maps through the label-equivalent row when the
// vocabulary names modes differently ("High" → the "32768" Anthropic
// budget), and otherwise degrades to the nearest ranked mode (ties prefer
// the stronger mode, so the openai-only extremes still degrade low/xhigh→
// low/high on OpenRouter). Returns "" when the level is off or unknown, the
// provider has no reasoning mechanism, or the model's declared set is empty
// (it cannot reason).
func thinkingModeIDForLevel(provider, model, level string) string {
	modes, known := modelThinkingModeVocabulary(provider, model)
	if !known {
		modes = providerDefaultThinkingModes(provider)
	}
	return thinkingModeIDForSet(modes, level)
}

// thinkingModeIDForSet resolves a declared level onto one concrete mode set:
// exact mode-ID match first, then the level's label row, then the nearest
// ranked mode (ties prefer the stronger mode). Unranked rows (custom
// endpoint mode IDs outside the standard labels) are skipped rather than
// guessed.
func thinkingModeIDForSet(modes []thinkingMode, level string) string {
	lvl := thinkingLevel(level)
	if !isValidThinkingLevel(level) || lvl == thinkingOff {
		return ""
	}
	for _, m := range modes {
		if m.ID == string(lvl) {
			return m.ID
		}
	}
	label := thinkingModeLabelForLevel(lvl)
	for _, m := range modes {
		if m.Name == label {
			return m.ID
		}
	}
	want := thinkingLevelRank(lvl)
	bestID, bestDist, bestRank := "", math.MaxFloat64, math.Inf(-1)
	for _, m := range modes {
		rank, ok := thinkingModeRank(m)
		if !ok {
			continue
		}
		dist := math.Abs(rank - want)
		if dist < bestDist || (dist == bestDist && rank > bestRank) {
			bestID, bestDist, bestRank = m.ID, dist, rank
		}
	}
	return bestID
}

// thinkingLevelRank positions a declared level on the cross-vocabulary
// effort ordering shared with thinkingModeRank.
func thinkingLevelRank(lvl thinkingLevel) float64 {
	switch lvl {
	case thinkingMinimal:
		return 1
	case thinkingLow:
		return 2
	case thinkingMedium:
		return 3
	case thinkingHigh:
		return 4
	case thinkingXHigh:
		return 5
	}
	return -1
}

// thinkingModeRank reads a mode's position on that same ordering from its
// picker label ("Low", "Medium-Low", "High", …), so budget-ID vocabularies
// (Anthropic, Gemini) rank alongside effort-ID ones. ok=false for labels
// outside the ordering; those rows are skipped when degrading.
func thinkingModeRank(m thinkingMode) (float64, bool) {
	switch m.Name {
	case "None":
		return 0, true
	case "Minimal":
		return 1, true
	case thinkingLabelLow:
		return 2, true
	case "Medium-Low":
		return 2.5, true
	case thinkingLabelMedium:
		return 3, true
	case thinkingLabelHigh:
		return 4, true
	case thinkingLabelXHigh:
		return 5, true
	case "Max":
		return 6, true
	}
	return 0, false
}

// thinkingModeLabelForLevel is the picker label a level's row carries in any
// catalog vocabulary ("high" → "High", also the "32768" row's label).
func thinkingModeLabelForLevel(lvl thinkingLevel) string {
	switch lvl {
	case thinkingMinimal:
		return "Minimal"
	case thinkingLow:
		return thinkingLabelLow
	case thinkingMedium:
		return thinkingLabelMedium
	case thinkingHigh:
		return thinkingLabelHigh
	case thinkingXHigh:
		return thinkingLabelXHigh
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
