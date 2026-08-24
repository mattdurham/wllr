package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
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
	// TopProvider is an OpenRouter-style nested shape some OpenAI-compatible
	// servers use instead of (or alongside) a top-level context_length.
	TopProvider struct {
		ContextLength int64 `json:"context_length"`
	} `json:"top_provider"`
}

// discoverLocalModels queries each configured OpenAI-compatible endpoint. The
// standard endpoint is <base_url>/models, where base_url normally ends in /v1.
// Context metadata is optional and vendor-specific; explicit local_models
// context_window values take precedence over anything returned by the endpoint.
func discoverLocalModels(ctx context.Context, cfg *Config) []modelInfo {
	if cfg == nil || len(cfg.LocalModels) == 0 {
		return nil
	}

	configuringByID := make(map[string]modelInfo)
	for _, model := range configuredLocalModels(cfg) {
		configuringByID[model.ID] = model
	}

	reasoningByBase := make(map[string]map[string]*lmsReasoningCapability)
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
		var reasoning map[string]*lmsReasoningCapability
		if capByModel, ok := reasoningByBase[baseURL]; ok {
			reasoning = capByModel
		} else {
			// The OpenAI-compatible /models endpoint carries no reasoning
			// metadata; LM Studio's app API v1 does. Probe it best-effort and
			// attach per-model thinking modes to the discovered entries.
			reasoning = queryLMSV1Models(ctx, lmsAppModelsEndpoint(baseURL), local.APIKey)
			reasoningByBase[baseURL] = reasoning
		}
		for _, remote := range models {
			id := strings.TrimSpace(remote.ID)
			if id == "" || seen[id] {
				continue
			}
			seen[id] = true
			model := configuringByID[id]
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
			if len(model.ThinkingModes) == 0 {
				if cap, ok := reasoning[id]; ok {
					model.ReasoningDeclared = true
					model.ThinkingModes = lmsReasoningToModes(cap)
					model.ReasoningDefault = lmsReasoningDefault(cap, model.ThinkingModes)
				}
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
	case m.TopProvider.ContextLength > 0:
		return m.TopProvider.ContextLength
	default:
		return 0
	}
}

// lmsReasoningCapability is the reasoning entry in an LM Studio app-API model's
// capabilities object: the boolean/graded reasoning options the model supports
// and the model's default setting.
type lmsReasoningCapability struct {
	AllowedOptions []string `json:"allowed_options"`
	Default        string   `json:"default"`
}

// lmsAppModelsEndpoint derives LM Studio's app API v1 model-listing endpoint
// from a configured OpenAI-compatible base URL. The base URL may be the
// OpenAI-compatible root ("…/v1"), the server root ("…"), or the app API v1
// root ("…/api/v1" — the suffix probeLocalModelsEndpoint may persist); all
// resolve to the same server host.
func lmsAppModelsEndpoint(baseURL string) string {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		return ""
	}
	if strings.HasSuffix(base, "/api/v1") {
		return base + "/models"
	}
	if strings.HasSuffix(base, "/v1") {
		base = strings.TrimSuffix(base, "/v1")
	}
	return base + "/api/v1/models"
}

// lmsCapabilities is the capabilities object on an LM Studio app-API model.
// The pointer's presence (even for an empty object) marks the model as
// endpoint-declared; a nil/absent capabilities object means the listing said
// nothing about the model.
type lmsCapabilities struct {
	Vision            bool                    `json:"vision"`
	TrainedForToolUse bool                    `json:"trained_for_tool_use"`
	Reasoning         *lmsReasoningCapability `json:"reasoning"`
}

// lmsV1Model is one entry in LM Studio's app API v1 /api/v1/models listing.
type lmsV1Model struct {
	Key              string           `json:"key"`
	DisplayName      string           `json:"display_name"`
	MaxContextLength int64            `json:"max_context_length"`
	Capabilities     *lmsCapabilities `json:"capabilities"`
}

type lmsV1ModelsResponse struct {
	Models []lmsV1Model `json:"models"`
}

// queryLMSV1Models probes LM Studio's app API v1 endpoint (<server root>
// /api/v1/models), which is the one OpenAI-ecosystem endpoint that exposes
// per-model reasoning capabilities. Returns a map keyed by model ID (the
// "key" field, which matches the OpenAI-compatible model ID). A nil
// capability value marks a model whose listing has a capabilities object but
// no reasoning entry (declared, but not a reasoning model). Unreachable or
// non-JSON endpoints (any other server type) yield a nil map — discovery is
// best-effort.
func queryLMSV1Models(ctx context.Context, endpoint, apiKey string) map[string]*lmsReasoningCapability {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil
	}
	if strings.TrimSpace(apiKey) != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil
	}
	var payload lmsV1ModelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil
	}
	out := make(map[string]*lmsReasoningCapability, len(payload.Models))
	for _, m := range payload.Models {
		id := strings.TrimSpace(m.Key)
		if id == "" || m.Capabilities == nil {
			continue
		}
		out[id] = m.Capabilities.Reasoning
	}
	return out
}

// lmsReasoningModeID maps an LM Studio reasoning option (its allowed_options
// vocabulary) to a wire-safe OpenAI reasoning_effort ID: "off" is the boolean
// spelling of "none" and "on" (boolean models) maps to "medium" — the
// endpoint only accepts the six OpenAI values and rejects "on" with a 400, so
// "reasoning on" must be expressed as a named effort. Other values must be
// standard effort IDs to be usable.
func lmsReasoningModeID(opt string) (string, bool) {
	switch opt {
	case "off":
		return thinkingModeNone, true
	case "on":
		return thinkingModeMedium, true
	default:
		if _, ok := openAIReasoningEffortByMode[opt]; ok {
			return opt, true
		}
		return "", false
	}
}

// thinkingModeLabel returns the display name and description for a standard
// OpenAI reasoning-effort mode ID.
func thinkingModeLabel(id string) (name, desc string) {
	switch id {
	case thinkingModeNone:
		return "None", "No extended reasoning"
	case thinkingModeMinimal:
		return "Minimal", "Minimal extended reasoning"
	case thinkingModeLow:
		return "Low", "Extended reasoning (low effort)"
	case thinkingModeMedium:
		return "Medium", "Extended reasoning (medium effort)"
	case thinkingModeHigh:
		return "High", "Extended reasoning (high effort)"
	case thinkingModeXHigh:
		return "X-High", "Extended reasoning (maximum effort)"
	}
	return id, ""
}

// thinkingModesFromIDs builds picker modes from a user-declared list of
// standard reasoning-effort IDs (local_models thinking_modes config). Unknown
// IDs are dropped; order is preserved.
func thinkingModesFromIDs(ids []string) []thinkingMode {
	if len(ids) == 0 {
		return nil
	}
	out := make([]thinkingMode, 0, len(ids))
	seen := make(map[string]bool)
	for _, id := range ids {
		if _, ok := openAIReasoningEffortByMode[id]; !ok || seen[id] {
			continue
		}
		seen[id] = true
		name, desc := thinkingModeLabel(id)
		out = append(out, thinkingMode{ID: id, Name: name, Description: desc})
	}
	return out
}

// lmsReasoningToModes converts an LM Studio reasoning capability into picker
// thinking modes. Graded options map to their standard IDs; on a boolean-only
// model (off/on) the "on" entry is offered as medium with an "On" label, so
// the user sees the model's own vocabulary in the picker. Returns nil when
// nothing usable is declared.
func lmsReasoningToModes(cap *lmsReasoningCapability) []thinkingMode {
	if cap == nil || len(cap.AllowedOptions) == 0 {
		return nil
	}
	// Boolean-only (off/on) gets a distinct label for the enabled state; a
	// graded set that also lists "on" keeps the plain "Medium" label.
	booleanOnly := false
	{
		nonNone := 0
		for _, opt := range cap.AllowedOptions {
			if id, _ := lmsReasoningModeID(opt); id != thinkingModeNone {
				nonNone++
			}
		}
		booleanOnly = len(cap.AllowedOptions) == 2 && nonNone == 1
	}
	seen := make(map[string]bool)
	var modes []thinkingMode
	for _, opt := range cap.AllowedOptions {
		id, ok := lmsReasoningModeID(opt)
		if !ok || seen[id] {
			continue
		}
		seen[id] = true
		name, desc := thinkingModeLabel(id)
		if booleanOnly && id == thinkingModeMedium {
			name, desc = "On", "Extended reasoning (on)"
		}
		modes = append(modes, thinkingMode{ID: id, Name: name, Description: desc})
	}
	if len(modes) == 0 {
		return nil
	}
	return modes
}

// lmsReasoningDefault maps the server-declared default to a mode ID.
// Returns "" when the default is absent or not in the offered set (the
// caller then falls back to "none" if offered, else the first mode).
// lmsReasoningDefault maps the server-declared default to a mode ID. It
// compares the raw default string against the raw allowed_options list (NOT
// the mapped IDs — the mapper collapses "on" and "medium" to the same ID, so a
// default of "medium" on an off/on model would otherwise be wrongly accepted).
// Returns "" when the default is absent or not in the declared set.
func lmsReasoningDefault(cap *lmsReasoningCapability, modes []thinkingMode) string {
	if cap == nil || cap.Default == "" {
		return ""
	}
	inSet := false
	for _, opt := range cap.AllowedOptions {
		if opt == cap.Default {
			inSet = true
			break
		}
	}
	if !inSet {
		return ""
	}
	id, ok := lmsReasoningModeID(cap.Default)
	if !ok {
		return ""
	}
	for _, m := range modes {
		if m.ID == id {
			return id
		}
	}
	return ""
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

// localModelsDiscoveryPathSuffixes are appended, in order, to a user-entered
// base URL when probing for an OpenAI-compatible /models endpoint. Most
// OpenAI-compatible servers expect the caller to already include "/v1" in the
// base URL, but users commonly type the bare host:port — this fills that gap
// without requiring them to know the convention.
var localModelsDiscoveryPathSuffixes = []string{
	"/models",
	"/v1/models",
	"/api/v1/models",
}

// probeLocalModelsEndpoint tries each of localModelsDiscoveryPathSuffixes
// against baseURL in order, returning the models found and the base URL
// (baseURL + the matched suffix's directory) that actually worked — so the
// caller persists a base_url that will keep working on subsequent launches,
// not necessarily the literal string the user typed. Returns
// queryLocalModelsUnreachable only if every attempt failed to connect at all
// (no attempt got as far as a response); a bad/empty response from at least
// one reachable attempt is reported as queryLocalModelsBadResponse so an
// unreachable host is never masked by a later attempt's connection failure.
func probeLocalModelsEndpoint(ctx context.Context, baseURL, apiKey string) ([]openAIModel, string, queryLocalModelsResult) {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		return nil, "", queryLocalModelsUnreachable
	}
	reachedAny := false
	for _, suffix := range localModelsDiscoveryPathSuffixes {
		endpoint := base + suffix
		models, result := queryLocalModels(ctx, endpoint, apiKey)
		if result == queryLocalModelsOK && len(models) > 0 {
			resolvedBase := strings.TrimSuffix(endpoint, "/models")
			return models, resolvedBase, queryLocalModelsOK
		}
		if result != queryLocalModelsUnreachable {
			reachedAny = true
		}
	}
	if reachedAny {
		return nil, "", queryLocalModelsBadResponse
	}
	return nil, "", queryLocalModelsUnreachable
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
			ThinkingModes: thinkingModesFromIDs(lm.ThinkingModes),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// localThinkingState caches the last local thinking resolution so the
// (network-bound) LM Studio app API probe runs once per model/endpoint
// instead of on every /thinking invocation.
var localThinkingState struct {
	mu       sync.Mutex
	key      string // provider + model + baseURL
	modes    []thinkingMode
	declared bool
	def      string
	resolved bool
}

// localThinkingInfo resolves the thinking support for the selected local
// model, with a per-session cache. Precedence: explicit local_models
// thinking_modes config > endpoint-declared set (LM Studio app API v1;
// declared=true, where an empty set means the model is known not to support
// reasoning) > the standard OpenAI effort set (declared=false; unknown
// endpoint, OpenAI-compatible servers speak reasoning_effort).
func localThinkingInfo(ctx context.Context, cfg *Config) (modes []thinkingMode, declared bool, def string) {
	if cfg == nil || cfg.Provider != providerLocal || cfg.Model == "" {
		return nil, false, ""
	}
	key := providerLocal + "|" + cfg.Model + "|" + cfg.LocalBaseURL
	localThinkingState.mu.Lock()
	if localThinkingState.key == key && localThinkingState.resolved {
		m, d, df := localThinkingState.modes, localThinkingState.declared, localThinkingState.def
		localThinkingState.mu.Unlock()
		return m, d, df
	}
	localThinkingState.mu.Unlock()

	modes, declared, def = resolveLocalThinkingModes(ctx, cfg)
	localThinkingState.mu.Lock()
	localThinkingState.key = key
	localThinkingState.modes, localThinkingState.declared, localThinkingState.def = modes, declared, def
	localThinkingState.resolved = true
	localThinkingState.mu.Unlock()
	return modes, declared, def
}

// localThinkingModesForModel returns only the resolved modes, for the
// /thinking picker listing.
func localThinkingModesForModel(ctx context.Context, cfg *Config) []thinkingMode {
	modes, _, _ := localThinkingInfo(ctx, cfg)
	return modes
}

func resolveLocalThinkingModes(ctx context.Context, cfg *Config) ([]thinkingMode, bool, string) {
	if lm, ok := cfg.localModelByID(cfg.Model); ok && len(lm.ThinkingModes) > 0 {
		return thinkingModesFromIDs(lm.ThinkingModes), false, ""
	}
	for _, m := range localModels(ctx, cfg) {
		if m.ID == cfg.Model && m.ReasoningDeclared {
			return m.ThinkingModes, true, m.ReasoningDefault
		}
	}
	return supportedThinkingModesForModel(providerLocal, cfg.Model), false, ""
}
