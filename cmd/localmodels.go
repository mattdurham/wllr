package main

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

func resolveLocalProviderConfig(ctx context.Context, cfg *Config) {
	if cfg == nil || cfg.Provider != providerLocal {
		return
	}
	configured := configuredLocalModels(cfg)
	if cfg.Model == "" && len(configured) > 0 {
		cfg.Model = configured[0].ID
	}
	cfg.applyLocalModelSelection(cfg.Model)
}

func localModels(ctx context.Context, cfg *Config) []modelInfo {
	_ = ctx
	configured := configuredLocalModels(cfg)
	if len(configured) > 0 {
		return configured
	}
	return modelsForProvider(providerLocal)
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
