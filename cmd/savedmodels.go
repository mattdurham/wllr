package main

import (
	"fmt"
	"strings"
)

// savedModelConfig is one catalog-provider model the user added to /models.
// Providers with a richer model store keep using it (local_models carries
// endpoints and keys; openrouter_models carries the live catalog pin), so this
// covers anthropic/openai/gemini, whose model list was previously a static
// catalog with nothing to pin.
type savedModelConfig struct {
	Provider      string `json:"provider"`
	ID            string `json:"id"`
	Name          string `json:"name,omitempty"`
	ContextWindow int64  `json:"context_window,omitempty"`
}

// savedModelKey identifies a saved model within its provider.
func savedModelKey(provider, id string) string {
	return strings.ToLower(strings.TrimSpace(provider)) + ":" + strings.ToLower(strings.TrimSpace(id))
}

// savedModelLookup returns the saved models indexed by provider:id.
func savedModelLookup(models []savedModelConfig) map[string]savedModelConfig {
	out := make(map[string]savedModelConfig, len(models))
	for _, m := range models {
		if m.Provider == "" || m.ID == "" {
			continue
		}
		out[savedModelKey(m.Provider, m.ID)] = m
	}
	return out
}

// addSavedModel records a catalog-provider model so it appears in /models.
// Adding a model that is already saved updates its name/window in place rather
// than duplicating it, and preserves its original position.
func addSavedModel(model savedModelConfig) error {
	model.Provider = strings.TrimSpace(model.Provider)
	model.ID = strings.TrimSpace(model.ID)
	if model.Provider == "" || model.ID == "" {
		return fmt.Errorf("saved model requires a provider and id")
	}
	models := append([]savedModelConfig(nil), loadWllrSettings().SavedModels...)
	key := savedModelKey(model.Provider, model.ID)
	replaced := false
	for i := range models {
		if savedModelKey(models[i].Provider, models[i].ID) == key {
			// Keep the existing provider/id case but refresh the display data.
			model.Provider, model.ID = models[i].Provider, models[i].ID
			models[i] = model
			replaced = true
			break
		}
	}
	if !replaced {
		models = append(models, model)
	}
	return saveWllrRawField("saved_models", models)
}

// removeSavedModel drops a model from the saved list. Removing an absent model
// is a no-op.
func removeSavedModel(provider, id string) error {
	models := loadWllrSettings().SavedModels
	key := savedModelKey(provider, id)
	out := models[:0]
	changed := false
	for _, m := range models {
		if savedModelKey(m.Provider, m.ID) == key {
			changed = true
			continue
		}
		out = append(out, m)
	}
	if !changed {
		return nil
	}
	if len(out) == 0 {
		return removeWllrField("saved_models")
	}
	return saveWllrRawField("saved_models", out)
}

// savedModelEntry resolves a provider/id against the saved list, falling back
// to nil when the model is not saved.
func savedModelEntry(models []savedModelConfig, provider, id string) (savedModelConfig, bool) {
	m, ok := savedModelLookup(models)[savedModelKey(provider, id)]
	return m, ok
}

// removeModel routes removal to the store that owns the provider: local models
// live in local_models, OpenRouter pins in openrouter_models, and catalog
// providers in saved_models. Returns an error for a provider with no model
// store so a removal can never silently no-op against the wrong list.
func removeModel(provider, id string) error {
	if provider == "" || id == "" {
		return fmt.Errorf("removing a model requires a provider and id")
	}
	switch provider {
	case providerLocal:
		return removeLocalModel(id)
	case providerOpenRouter:
		return removeOpenRouterModel(id)
	default:
		return removeSavedModel(provider, id)
	}
}

// removeLocalModel drops a configured local model entry. Removing the last
// entry clears the field so an empty list does not linger in the config.
func removeLocalModel(id string) error {
	models := loadWllrSettings().LocalModels
	out := make([]localModelConfig, 0, len(models))
	changed := false
	for _, m := range models {
		if m.ID == id {
			changed = true
			continue
		}
		out = append(out, m)
	}
	if !changed {
		return nil
	}
	if len(out) == 0 {
		return removeWllrField("local_models")
	}
	return saveLocalModels(out)
}

// removeOpenRouterModel drops a pinned OpenRouter model.
func removeOpenRouterModel(id string) error {
	models := loadWllrSettings().OpenRouterModels
	out := make([]openRouterModelConfig, 0, len(models))
	changed := false
	for _, m := range models {
		if m.ID == id {
			changed = true
			continue
		}
		out = append(out, m)
	}
	if !changed {
		return nil
	}
	if len(out) == 0 {
		return removeWllrField("openrouter_models")
	}
	return saveWllrRawField("openrouter_models", out)
}
