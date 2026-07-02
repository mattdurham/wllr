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

func resolveLocalProviderConfig(ctx context.Context, cfg *Config) {
	if cfg == nil || cfg.Provider != providerLocal {
		return
	}
	if cfg.ContextWindow == 0 && cfg.LocalContextWindow > 0 {
		cfg.ContextWindow = cfg.LocalContextWindow
	}
	models, err := fetchLocalModels(ctx, cfg)
	if err != nil {
		return
	}
	if len(models) > 0 && !modelListContains(models, cfg.Model) {
		cfg.Model = models[0].ID
	}
}

func modelListContains(models []modelInfo, id string) bool {
	for _, m := range models {
		if m.ID == id {
			return true
		}
	}
	return false
}

func localModels(ctx context.Context, cfg *Config) []modelInfo {
	models, err := fetchLocalModels(ctx, cfg)
	if err == nil && len(models) > 0 {
		return models
	}
	return modelsForProvider(providerLocal)
}

func localProviderSublabel(cfg *Config) string {
	if cfg == nil || strings.TrimSpace(cfg.LocalBaseURL) == "" {
		return "configure wllr.local_base_url"
	}
	if cfg.LocalContextWindow > 0 {
		return fmt.Sprintf("%s · %dk ctx", cfg.LocalBaseURL, cfg.LocalContextWindow/1000)
	}
	return cfg.LocalBaseURL
}

func fetchLocalModels(ctx context.Context, cfg *Config) ([]modelInfo, error) {
	if cfg == nil || strings.TrimSpace(cfg.LocalBaseURL) == "" {
		return nil, fmt.Errorf("local_base_url is not configured")
	}
	endpoint := strings.TrimRight(cfg.LocalBaseURL, "/") + "/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	if cfg.LocalAPIKey != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.LocalAPIKey)
	}
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("GET %s: %s", endpoint, resp.Status)
	}
	var payload struct {
		Data []struct {
			ID            string `json:"id"`
			Name          string `json:"name"`
			ContextLength int64  `json:"context_length"`
			TopProvider   struct {
				ContextLength int64 `json:"context_length"`
			} `json:"top_provider"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	out := make([]modelInfo, 0, len(payload.Data))
	seen := make(map[string]struct{}, len(payload.Data))
	for _, item := range payload.Data {
		id := strings.TrimSpace(item.ID)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		name := strings.TrimSpace(item.Name)
		if name == "" {
			name = id
		}
		contextWindow := cfg.LocalContextWindow
		if contextWindow == 0 {
			contextWindow = item.ContextLength
		}
		if contextWindow == 0 {
			contextWindow = item.TopProvider.ContextLength
		}
		out = append(out, modelInfo{ID: id, Name: name, ContextWindow: contextWindow})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}
