package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/mattdurham/wllr/modules/agent"
	"github.com/mattdurham/wllr/modules/harness"
)

// savedModelChoices builds the /models list: every model the user actually has,
// across providers. Sources are provider-specific so there is exactly one owner
// per model:
//
//   - local:        wllr.local_models (configured endpoints; discovery fills in windows)
//   - openrouter:   wllr.openrouter_models (pinned from the live catalog)
//   - other:        wllr.saved_models (catalog models added through the add flow)
//
// activeProvider/activeModel mark the row currently in use.
func savedModelChoices(
	ctx context.Context,
	cfg *Config,
	activeProvider, activeModel string,
) []harness.ModelChoice {
	if cfg == nil {
		return nil
	}
	out := make([]harness.ModelChoice, 0, len(cfg.LocalModels)+len(cfg.OpenRouterModels)+len(cfg.SavedModels))

	for _, lm := range cfg.LocalModels {
		if lm.ID == "" {
			continue
		}
		name := lm.Name
		if name == "" {
			name = lm.ID
		}
		cw := contextWindowForSelection(providerLocal, lm.ID, cfg)
		out = append(out, harness.ModelChoice{
			ID:                 lm.ID,
			Name:               name,
			Sublabel:           localModelSublabel(lm, cw),
			Provider:           providerLocal,
			ContextWindow:      cw,
			ContextWindowKnown: cw > 0,
			Tiers:              tiersForModel(providerLocal, lm.ID),
		})
	}

	for _, m := range cfg.OpenRouterModels {
		if m.ID == "" {
			continue
		}
		name := m.Name
		if name == "" {
			name = m.ID
		}
		cw := contextWindowForSelection(providerOpenRouter, m.ID, cfg)
		out = append(out, harness.ModelChoice{
			ID:                 m.ID,
			Name:               name,
			Sublabel:           m.ID,
			Provider:           providerOpenRouter,
			ContextWindow:      cw,
			ContextWindowKnown: cw > 0,
			Tiers:              tiersForModel(providerOpenRouter, m.ID),
		})
	}

	for _, m := range cfg.SavedModels {
		// Provider-specific stores own their providers; saved_models only
		// carries the catalog providers (and any tier target on one).
		switch m.Provider {
		case providerLocal, providerOpenRouter:
			continue
		}
		if m.Provider == "" || m.ID == "" {
			continue
		}
		cw := contextWindowForSelection(m.Provider, m.ID, cfg)
		name := m.Name
		if name == "" {
			name = m.ID
		}
		out = append(out, harness.ModelChoice{
			ID:                 m.ID,
			Name:               name,
			Sublabel:           m.ID,
			Provider:           m.Provider,
			ContextWindow:      cw,
			ContextWindowKnown: cw > 0,
			Tiers:              tiersForModel(m.Provider, m.ID),
		})
	}

	// Mark the active model so the list shows what is live. The active model is
	// always present in a provider's store (it was selected through /models), but
	// guard against it missing so the list never silently omits the live model.
	for i := range out {
		if out[i].ID == activeModel && out[i].Provider == activeProvider {
			out[i].Active = true
			return out
		}
	}
	if activeModel != "" {
		if cw := contextWindowForSelection(activeProvider, activeModel, cfg); cw > 0 || activeProvider != "" {
			out = append(out, harness.ModelChoice{
				ID:                 activeModel,
				Name:               activeModel,
				Sublabel:           "(configured model)",
				Provider:           activeProvider,
				ContextWindow:      cw,
				ContextWindowKnown: cw > 0,
				Active:             true,
				Tiers:              tiersForModel(activeProvider, activeModel),
			})
		}
	}
	return out
}

// localModelSublabel renders a local model row with its endpoint and window.
func localModelSublabel(lm localModelConfig, cw int64) string {
	parts := []string{lm.ID}
	if lm.BaseURL != "" {
		parts = append(parts, lm.BaseURL)
	}
	if cw > 0 {
		parts = append(parts, fmt.Sprintf("%dk ctx", cw/1000))
	}
	return strings.Join(parts, " · ")
}

// addModelProviders lists the providers offered by the add-model flow.
func addModelProviders(cfg *Config) []harness.ProviderChoice {
	return []harness.ProviderChoice{
		{ID: providerAnthropic, Name: "Anthropic", Sublabel: "Claude models (API key or Claude login)"},
		{ID: providerOpenAI, Name: "OpenAI / ChatGPT", Sublabel: "GPT models (API key or ChatGPT login)"},
		{ID: providerOpenRouter, Name: "OpenRouter", Sublabel: "search the live catalog with an API key"},
		{ID: providerLocal, Name: "Local model", Sublabel: localProviderSublabel(cfg)},
	}
}

// providerReady reports whether a provider has enough configuration to list and
// use models. Local needs at least one endpoint; OpenRouter needs a key; the
// catalog providers need a credential (key or OAuth).
func providerReady(cfg *Config, provider string) bool {
	if cfg == nil {
		return false
	}
	switch provider {
	case providerLocal:
		return len(cfg.LocalModels) > 0
	case providerOpenRouter:
		return cfg.OpenRouterAPIKey != ""
	case providerAnthropic:
		return cfg.AnthropicAPIKey != "" || hasOAuthCredential(providerAnthropic)
	case providerOpenAI:
		return cfg.OpenAIAPIKey != "" || hasOAuthCredential(providerOpenAI)
	case providerGemini:
		return cfg.GeminiAPIKey != ""
	default:
		return false
	}
}

// hasOAuthCredential reports whether a stored OAuth credential exists for the
// provider, so a prior /login counts as configured.
func hasOAuthCredential(provider string) bool {
	cred, ok := loadAuthCredential(provider)
	return ok && cred.Type == authTypeOAuth && cred.Access != ""
}

// catalogChoicesFor lists the models a provider can add in the add-model flow.
// Catalog providers use their built-in catalog; OpenRouter uses the pinned list
// already fetched by the searchable browse step.
func catalogChoicesFor(cfg *Config, provider string) []harness.ModelChoice {
	var catalog []modelInfo
	switch provider {
	case providerOpenAI:
		catalog = modelsForOpenAIAuth()
	case providerLocal:
		catalog = configuredLocalModels(cfg)
	case providerAnthropic, providerGemini:
		catalog = modelsForProvider(provider)
	case providerOpenRouter:
		out := make([]harness.ModelChoice, 0, len(cfg.OpenRouterModels))
		for _, m := range cfg.OpenRouterModels {
			name := m.Name
			if name == "" {
				name = m.ID
			}
			out = append(out, harness.ModelChoice{
				ID:                 m.ID,
				Name:               name,
				Sublabel:           m.ID,
				Provider:           providerOpenRouter,
				ContextWindow:      m.ContextWindow,
				ContextWindowKnown: m.ContextWindow > 0,
			})
		}
		return out
	default:
		return nil
	}
	out := make([]harness.ModelChoice, 0, len(catalog))
	for _, mi := range catalog {
		cw := contextWindowForSelection(provider, mi.ID, cfg)
		out = append(out, harness.ModelChoice{
			ID:                 mi.ID,
			Name:               mi.Name,
			Sublabel:           modelChoiceSublabel(mi),
			Provider:           provider,
			ContextWindow:      cw,
			ContextWindowKnown: cw > 0,
		})
	}
	return out
}

// saveCatalogModel persists a catalog model chosen in the add-model flow and
// makes it the active model, rebuilding the pool's provider. Catalog providers
// have no other model store, so this is what makes an added model appear in
// /models next time.
func saveCatalogModel(
	ctx context.Context,
	cfg *Config,
	pool *agent.AgentPool,
	provider string,
	choice harness.ModelChoice,
) (string, error) {
	if provider == "" || choice.ID == "" {
		return "", fmt.Errorf("provider and model id are required")
	}
	cw := choice.ContextWindow
	if cw <= 0 {
		cw = contextWindowForSelection(provider, choice.ID, cfg)
	}
	if err := addSavedModel(savedModelConfig{
		Provider:      provider,
		ID:            choice.ID,
		Name:          choice.Name,
		ContextWindow: cw,
	}); err != nil {
		return "", err
	}
	// Reflect the addition in the in-memory config so the list and provider
	// build see it without a reload.
	if _, ok := savedModelEntry(cfg.SavedModels, provider, choice.ID); !ok {
		cfg.SavedModels = append(cfg.SavedModels, savedModelConfig{
			Provider:      provider,
			ID:            choice.ID,
			Name:          choice.Name,
			ContextWindow: cw,
		})
	}
	if err := activateProviderModel(ctx, cfg, pool, provider, choice.ID); err != nil {
		return "", err
	}
	cfg.Provider = provider
	cfg.Model = choice.ID
	if err := saveProvider(provider); err != nil {
		return "", err
	}
	if err := saveModel(choice.ID); err != nil {
		return "", err
	}
	return choice.ID, nil
}

// dropModelFromConfig removes a model from the in-memory config after a
// successful on-disk removal, so the /models list reflects it immediately.
func dropModelFromConfig(cfg *Config, provider, id string) {
	if cfg == nil {
		return
	}
	switch provider {
	case providerLocal:
		kept := cfg.LocalModels[:0]
		for _, m := range cfg.LocalModels {
			if m.ID != id {
				kept = append(kept, m)
			}
		}
		cfg.LocalModels = kept
	case providerOpenRouter:
		kept := cfg.OpenRouterModels[:0]
		for _, m := range cfg.OpenRouterModels {
			if m.ID != id {
				kept = append(kept, m)
			}
		}
		cfg.OpenRouterModels = kept
	default:
		kept := cfg.SavedModels[:0]
		for _, m := range cfg.SavedModels {
			if m.Provider != provider || m.ID != id {
				kept = append(kept, m)
			}
		}
		cfg.SavedModels = kept
	}
}
