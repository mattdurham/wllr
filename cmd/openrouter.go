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

// openRouterModelsURL lists the models the account can use, not the global
// catalog: OpenRouter applies the account's guardrails and data policy
// server-side, so this endpoint already excludes models a request would be
// rejected for. The global /models endpoint advertises models the account
// cannot run (its endpoints even report a healthy status), so using it here
// would offer choices that fail only once the user tries them.
const openRouterModelsURL = "https://openrouter.ai/api/v1/models/user"

type openRouterModelConfig struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	ContextWindow int64  `json:"context_window"`

	// SupportedParameters is the request-parameter set the OpenRouter model
	// listing declares for this model; "reasoning" among them means the
	// model can reason (include_reasoning is the legacy spelling). Empty
	// for models pinned before this was captured: OpenRouter always
	// populates the field for every model it lists, so no data means an
	// older pin, not a reasoning-less model.
	SupportedParameters []string `json:"supported_parameters,omitempty"`
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

// openRouterReasoningCapability reports whether a pinned OpenRouter model
// declares reasoning support, from the model listing's supported_parameters
// captured at pin time. declared is false when there is no data — models
// pinned before the parameter was captured, or IDs that are not pinned — and
// the model is then treated as able to reason, preserving the previous
// always-offer behavior. "reasoning" is the modern parameter name;
// "include_reasoning" is the legacy spelling OpenRouter still lists.
func openRouterReasoningCapability(settings wllrSettings, id string) (supported, declared bool) {
	for _, model := range settings.OpenRouterModels {
		if model.ID != id {
			continue
		}
		if len(model.SupportedParameters) == 0 {
			return true, false
		}
		for _, p := range model.SupportedParameters {
			if p == "reasoning" || p == "include_reasoning" {
				return true, true
			}
		}
		return false, true
	}
	return true, false
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
			ID                  string   `json:"id"`
			Name                string   `json:"name"`
			ContextLength       int64    `json:"context_length"`
			SupportedParameters []string `json:"supported_parameters"`
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
			openRouterModelConfig{
				ID:                  model.ID,
				Name:                model.Name,
				ContextWindow:       model.ContextLength,
				SupportedParameters: model.SupportedParameters,
			},
		)
	}
	return models, nil
}
