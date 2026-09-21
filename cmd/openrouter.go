package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type openRouterModelConfig struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	ContextWindow int64  `json:"context_window"`
}

func (cfg *Config) openRouterModel(id string) (openRouterModelConfig, bool) {
	if cfg != nil {
		for _, model := range cfg.OpenRouterModels {
			if model.ID == id {
				return model, true
			}
		}
	}
	return openRouterModelConfig{}, false
}

func (cfg *Config) pinOpenRouterModel(model openRouterModelConfig) error {
	if model.ID == "" {
		return fmt.Errorf("openrouter model ID is required")
	}
	models := []openRouterModelConfig{model}
	for _, saved := range cfg.OpenRouterModels {
		if saved.ID != model.ID {
			models = append(models, saved)
		}
	}
	if err := saveWllrRawField("openrouter_models", models); err != nil {
		return err
	}
	cfg.OpenRouterModels = models
	return nil
}

func fetchOpenRouterModels(
	ctx context.Context,
	client *http.Client,
	endpoint, key string,
) ([]openRouterModelConfig, error) {
	if strings.TrimSpace(key) == "" {
		return nil, fmt.Errorf("OpenRouter API key is required")
	}
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("create OpenRouter model request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+key)
	resp, err := client.Do(req) //nolint:gosec // Endpoint is fixed by caller; tests inject a local server.
	if err != nil {
		return nil, fmt.Errorf("fetch OpenRouter models: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("OpenRouter model list returned HTTP %d", resp.StatusCode)
	}
	var payload struct {
		Data []struct {
			ID            string `json:"id"`
			Name          string `json:"name"`
			ContextLength int64  `json:"context_length"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode OpenRouter models: %w", err)
	}
	models := make([]openRouterModelConfig, 0, len(payload.Data))
	for _, model := range payload.Data {
		if model.ID == "" {
			continue
		}
		if model.Name == "" {
			model.Name = model.ID
		}
		models = append(
			models,
			openRouterModelConfig{ID: model.ID, Name: model.Name, ContextWindow: model.ContextLength},
		)
	}
	return models, nil
}
