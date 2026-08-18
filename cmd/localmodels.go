package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
)

// resolveLocalProviderConfig applies the selected local model when available.
// It returns true when a non-empty stale selection was replaced so the UI can
// ask the user to confirm the new model at startup.
func resolveLocalProviderConfig(ctx context.Context, cfg *Config) bool {
	if cfg == nil || cfg.Provider != providerLocal {
		return false
	}
	models := localModels(ctx, cfg)
	// A persisted model may disappear from a local server after a model
	// replacement or endpoint change. Prefer the configured model when it is
	// still available, otherwise fall back to the first discovered/configured
	// model so startup does not fail with a misleading "matching model" error.
	for _, model := range models {
		if cfg.Model != "" && model.ID != cfg.Model {
			continue
		}
		if rememberLocalModel(cfg, model) {
			return false
		}
	}
	if cfg.Model != "" {
		for _, model := range models {
			if rememberLocalModel(cfg, model) {
				return true
			}
		}
	}
	return false
}

func localModels(ctx context.Context, cfg *Config) []modelInfo {
	configured := configuredLocalModels(cfg)
	if discovered := discoverLocalModels(ctx, cfg); len(discovered) > 0 {
		return discovered
	}
	if len(configured) > 0 {
		return configured
	}
	return modelsForProvider(providerLocal)
}

type openAIModelsResponse struct {
	Data []openAIModel `json:"data"`
}

type openAIModel struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	ContextLength    int64  `json:"context_length"`
	MaxContextLength int64  `json:"max_context_length"`
	MaxModelLength   int64  `json:"max_model_len"`
	NumContext       int64  `json:"num_ctx"`
}

// discoverLocalModels queries each configured OpenAI-compatible endpoint. The
// standard endpoint is <base_url>/models, where base_url normally ends in /v1.
// Context metadata is optional and vendor-specific; explicit local_models
// context_window values take precedence over anything returned by the endpoint.
func discoverLocalModels(ctx context.Context, cfg *Config) []modelInfo {
	if cfg == nil || len(cfg.LocalModels) == 0 {
		return nil
	}

	configuredByID := make(map[string]modelInfo)
	for _, model := range configuredLocalModels(cfg) {
		configuredByID[model.ID] = model
	}

	seen := make(map[string]bool)
	var discovered []modelInfo
	for _, local := range cfg.LocalModels {
		baseURL := strings.TrimRight(strings.TrimSpace(local.BaseURL), "/")
		if baseURL == "" {
			continue
		}
		models, result := queryLocalModels(ctx, baseURL+"/models", local.APIKey)
		if result != queryLocalModelsOK {
			continue
		}
		for _, remote := range models {
			id := strings.TrimSpace(remote.ID)
			if id == "" || seen[id] {
				continue
			}
			seen[id] = true
			model := configuredByID[id]
			model.ID = id
			if model.Name == "" {
				model.Name = strings.TrimSpace(remote.Name)
			}
			if model.Name == "" {
				model.Name = id
			}
			if model.ContextWindow == 0 {
				model.ContextWindow = contextWindowFromOpenAIModel(remote)
			}
			if model.LocalBaseURL == "" {
				model.LocalBaseURL = baseURL
				model.LocalAPIKey = local.APIKey
			}
			discovered = append(discovered, model)
		}
	}
	sort.Slice(discovered, func(i, j int) bool { return discovered[i].ID < discovered[j].ID })
	return discovered
}

// contextWindowFromOpenAIModel resolves a context-window token count from an
// OpenAI-compatible /models entry, trying each vendor-specific field in turn.
// Returns 0 if none are populated.
func contextWindowFromOpenAIModel(m openAIModel) int64 {
	switch {
	case m.ContextLength > 0:
		return m.ContextLength
	case m.MaxContextLength > 0:
		return m.MaxContextLength
	case m.MaxModelLength > 0:
		return m.MaxModelLength
	case m.NumContext > 0:
		return m.NumContext
	default:
		return 0
	}
}

// queryLocalModelsResult classifies why a probe did not yield models, so
// callers can distinguish an unreachable endpoint (bad host/port, refused,
// timed out) from one that responded but had nothing usable.
type queryLocalModelsResult int

const (
	queryLocalModelsOK queryLocalModelsResult = iota
	// queryLocalModelsUnreachable means the request itself failed to complete
	// (DNS failure, connection refused, timeout) — the URL is likely wrong.
	queryLocalModelsUnreachable
	// queryLocalModelsBadResponse means the endpoint was reached but returned a
	// non-2xx status or a body that isn't a valid /models listing.
	queryLocalModelsBadResponse
)

func queryLocalModels(parent context.Context, endpoint, apiKey string) ([]openAIModel, queryLocalModelsResult) {
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, queryLocalModelsUnreachable
	}
	if strings.TrimSpace(apiKey) != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, queryLocalModelsUnreachable
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, queryLocalModelsBadResponse
	}
	var payload openAIModelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, queryLocalModelsBadResponse
	}
	return payload.Data, queryLocalModelsOK
}

func applyLocalModelChoice(ctx context.Context, cfg *Config, id string) bool {
	if cfg.applyLocalModelSelection(id) {
		return true
	}
	for _, model := range localModels(ctx, cfg) {
		if model.ID != id {
			continue
		}
		return rememberLocalModel(cfg, model)
	}
	return false
}

// rememberLocalModel makes a discovered model available to buildProvider for
// the rest of the session. It is intentionally kept in memory; persistent
// configuration remains the user's explicit endpoint configuration.
func rememberLocalModel(cfg *Config, model modelInfo) bool {
	if cfg == nil || model.ID == "" || model.LocalBaseURL == "" {
		return false
	}
	if _, ok := cfg.localModelByID(model.ID); !ok {
		cfg.LocalModels = append(cfg.LocalModels, localModelConfig{
			ID:            model.ID,
			Name:          model.Name,
			BaseURL:       model.LocalBaseURL,
			APIKey:        model.LocalAPIKey,
			ContextWindow: model.ContextWindow,
		})
	}
	cfg.Model = model.ID
	cfg.LocalBaseURL = model.LocalBaseURL
	cfg.LocalAPIKey = model.LocalAPIKey
	if model.ContextWindow > 0 {
		cfg.LocalContextWindow = model.ContextWindow
		if !cfg.ContextWindowConfigured {
			cfg.ContextWindow = model.ContextWindow
		}
	}
	return true
}

func localProviderSublabel(cfg *Config) string {
	if cfg == nil || strings.TrimSpace(cfg.LocalBaseURL) == "" {
		return "configure wllr.local_models"
	}
	if cfg.LocalContextWindow > 0 {
		return fmt.Sprintf("%s · %dk ctx", cfg.LocalBaseURL, cfg.LocalContextWindow/1000)
	}
	return cfg.LocalBaseURL
}

func modelChoiceSublabel(mi modelInfo) string {
	parts := []string{mi.ID}
	if mi.LocalBaseURL != "" {
		parts = append(parts, mi.LocalBaseURL)
	}
	if mi.ContextWindow > 0 {
		parts = append(parts, fmt.Sprintf("%dk ctx", mi.ContextWindow/1000))
	}
	return strings.Join(parts, " · ")
}

func firstAvailableModel(preferred string, models []modelInfo) string {
	if preferred != "" {
		for _, m := range models {
			if m.ID == preferred {
				return preferred
			}
		}
	}
	if len(models) == 0 {
		return ""
	}
	return models[0].ID
}

func configuredLocalModels(cfg *Config) []modelInfo {
	if cfg == nil || len(cfg.LocalModels) == 0 {
		return nil
	}
	out := make([]modelInfo, 0, len(cfg.LocalModels))
	for _, lm := range cfg.LocalModels {
		id := strings.TrimSpace(lm.ID)
		baseURL := strings.TrimSpace(lm.BaseURL)
		if id == "" || baseURL == "" {
			continue
		}
		name := strings.TrimSpace(lm.Name)
		if name == "" {
			name = id
		}
		out = append(out, modelInfo{
			ID:            id,
			Name:          name,
			ContextWindow: lm.ContextWindow,
			LocalBaseURL:  baseURL,
			LocalAPIKey:   lm.APIKey,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
