package main

// NOTE: model catalog. Source: charmbracelet Catwalk (https://catwalk.charm.sh/v2/providers),
// the same model-metadata service used by crush. Regenerate by fetching that
// endpoint and updating the per-provider slices below. Context windows are the
// model's input window in tokens.
//
// This is intentionally core Go code (not a WASM extension): the model list is
// tied directly to the provider integrations in cmd/provider.go.

// modelInfo describes one selectable model for the /model picker.
type modelInfo struct {
	ID            string
	Name          string
	ContextWindow int64
}

// modelCatalog maps a provider name (cfg.Provider) to its selectable models,
// most-capable first. Keys match the provider names accepted by buildProvider.
var modelCatalog = map[string][]modelInfo{
	"anthropic": {
		{ID: "claude-opus-4-8", Name: "Claude Opus 4.8", ContextWindow: 1000000},
		{ID: "claude-opus-4-7", Name: "Claude Opus 4.7", ContextWindow: 1000000},
		{ID: "claude-opus-4-6", Name: "Claude Opus 4.6", ContextWindow: 200000},
		{ID: "claude-opus-4-5-20251101", Name: "Claude Opus 4.5", ContextWindow: 200000},
		{ID: "claude-sonnet-5", Name: "Claude Sonnet 5", ContextWindow: 1000000},
		{ID: "claude-sonnet-4-6", Name: "Claude Sonnet 4.6", ContextWindow: 200000},
		{ID: "claude-sonnet-4-5-20250929", Name: "Claude Sonnet 4.5", ContextWindow: 200000},
		{ID: "claude-haiku-4-5-20251001", Name: "Claude Haiku 4.5", ContextWindow: 200000},
		{ID: "claude-opus-4-1-20250805", Name: "Claude Opus 4.1", ContextWindow: 200000},
		{ID: "claude-opus-4-20250514", Name: "Claude Opus 4", ContextWindow: 200000},
		{ID: "claude-sonnet-4-20250514", Name: "Claude Sonnet 4", ContextWindow: 200000},
	},
	"openai": {
		{ID: "gpt-5.5", Name: "GPT-5.5", ContextWindow: 1050000},
		{ID: "gpt-5.5-pro", Name: "GPT-5.5 Pro", ContextWindow: 1050000},
		{ID: "gpt-5.4", Name: "GPT-5.4", ContextWindow: 1050000},
		{ID: "gpt-5.4-pro", Name: "GPT-5.4 Pro", ContextWindow: 1050000},
		{ID: "gpt-5.4-mini", Name: "GPT-5.4 Mini", ContextWindow: 400000},
		{ID: "gpt-5.4-nano", Name: "GPT-5.4 Nano", ContextWindow: 400000},
		{ID: "gpt-5.3-codex", Name: "GPT-5.3 Codex", ContextWindow: 400000},
		{ID: "gpt-5.2", Name: "GPT-5.2", ContextWindow: 400000},
		{ID: "gpt-5.2-codex", Name: "GPT-5.2 Codex", ContextWindow: 400000},
		{ID: "gpt-5.1", Name: "GPT-5.1", ContextWindow: 400000},
		{ID: "gpt-5.1-codex", Name: "GPT-5.1 Codex", ContextWindow: 400000},
		{ID: "gpt-5.1-codex-max", Name: "GPT-5.1 Codex Max", ContextWindow: 400000},
		{ID: "gpt-5.1-codex-mini", Name: "GPT-5.1 Codex Mini", ContextWindow: 400000},
		{ID: "gpt-5-codex", Name: "GPT-5 Codex", ContextWindow: 400000},
		{ID: "gpt-5", Name: "GPT-5", ContextWindow: 400000},
		{ID: "gpt-5-mini", Name: "GPT-5 Mini", ContextWindow: 400000},
		{ID: "gpt-5-nano", Name: "GPT-5 Nano", ContextWindow: 400000},
		{ID: "o4-mini", Name: "o4 Mini", ContextWindow: 200000},
		{ID: "o3", Name: "o3", ContextWindow: 200000},
		{ID: "gpt-4.1", Name: "GPT-4.1", ContextWindow: 1047576},
		{ID: "gpt-4.1-mini", Name: "GPT-4.1 Mini", ContextWindow: 1047576},
		{ID: "gpt-4.1-nano", Name: "GPT-4.1 Nano", ContextWindow: 1047576},
		{ID: "o3-mini", Name: "o3 Mini", ContextWindow: 200000},
		{ID: "gpt-4o", Name: "GPT-4o", ContextWindow: 128000},
		{ID: "gpt-4o-mini", Name: "GPT-4o-mini", ContextWindow: 128000},
	},
	"gemini": {
		{ID: "gemini-3.5-flash", Name: "Gemini 3.5 Flash", ContextWindow: 1048576},
		{ID: "gemini-3.1-pro-preview-customtools", Name: "Gemini 3.1 Pro", ContextWindow: 1048576},
		{ID: "gemini-3-pro-preview", Name: "Gemini 3 Pro", ContextWindow: 1048576},
		{ID: "gemini-3-flash-preview", Name: "Gemini 3 Flash", ContextWindow: 1048576},
		{ID: "gemini-2.5-pro", Name: "Gemini 2.5 Pro", ContextWindow: 1048576},
		{ID: "gemini-2.5-flash", Name: "Gemini 2.5 Flash", ContextWindow: 1048576},
	},
}

// modelsForProvider returns the catalog for a provider, or nil if unknown.
func modelsForProvider(provider string) []modelInfo {
	return modelCatalog[provider]
}

// contextWindowFromCatalog returns the context window for a model ID within a
// provider, or 0 if not found.
func contextWindowFromCatalog(provider, id string) int64 {
	for _, m := range modelCatalog[provider] {
		if m.ID == id {
			return m.ContextWindow
		}
	}
	return 0
}
