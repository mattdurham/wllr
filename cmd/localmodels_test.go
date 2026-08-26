package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLocalModelsUsesConfiguredModels(t *testing.T) {
	cfg := &Config{
		LocalModels: []localModelConfig{
			{
				ID:            "deepseek-v4-flash",
				Name:          "Dwarfstar 4 Flash",
				BaseURL:       "http://127.0.0.1:1/v1",
				ContextWindow: 300000,
			},
			{
				ID:            "qwen/qwen3-coder-next",
				Name:          "Qwen3 Coder Next",
				BaseURL:       "http://127.0.0.1:1/v1",
				ContextWindow: 262144,
			},
		},
	}

	models := localModels(context.Background(), cfg)
	if len(models) != 2 {
		t.Fatalf("len(models) = %d, want 2: %+v", len(models), models)
	}
	if models[0].ID != "deepseek-v4-flash" || models[0].LocalBaseURL != "http://127.0.0.1:1/v1" {
		t.Fatalf("configured model not preserved: %+v", models[0])
	}
	if models[1].ID != "qwen/qwen3-coder-next" || models[1].LocalBaseURL != "http://127.0.0.1:1/v1" {
		t.Fatalf("second configured model not annotated: %+v", models[1])
	}
}

func TestLocalProviderSublabel(t *testing.T) {
	got := localProviderSublabel(&Config{LocalBaseURL: "http://localhost:8000/v1", LocalContextWindow: 100000})
	want := "http://localhost:8000/v1 · 100k ctx"
	if got != want {
		t.Fatalf("localProviderSublabel = %q, want %q", got, want)
	}
}

func TestFirstAvailableModelKeepsPreferredWhenPresent(t *testing.T) {
	models := []modelInfo{{ID: "a"}, {ID: "b"}}
	if got := firstAvailableModel("b", models); got != "b" {
		t.Fatalf("firstAvailableModel = %q, want b", got)
	}
	if got := firstAvailableModel("missing", models); got != "a" {
		t.Fatalf("firstAvailableModel missing = %q, want a", got)
	}
}

func TestLocalModelsDiscoversModelsFromEndpoint(t *testing.T) {
	var sawModels, sawReasoning bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("authorization = %q, want bearer token", got)
		}
		switch r.URL.Path {
		case "/v1/models":
			// The standard OpenAI-compatible listing.
			sawModels = true
			_, _ = fmt.Fprint(
				w,
				`{"object":"list","data":[{"id":"model-a","name":"Model A","context_length":262144},{"id":"model-b"}]}`,
			)
		case "/api/v1/models":
			// LM Studio's app API v1 (reasoning capability discovery) —
			// expected to be probed best-effort; a 404 is a fine answer.
			sawReasoning = true
			w.WriteHeader(http.StatusNotFound)
			_, _ = fmt.Fprint(w, `{"error":"unknown"}`)
		default:
			t.Errorf("unexpected request path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	cfg := &Config{LocalModels: []localModelConfig{{
		ID: "configured-model", BaseURL: server.URL + "/v1", APIKey: "test-key",
	}}}
	models := localModels(context.Background(), cfg)
	if len(models) != 2 {
		t.Fatalf("len(models) = %d, want 2: %+v", len(models), models)
	}
	if models[0].ID != "model-a" || models[0].Name != "Model A" {
		t.Fatalf("first discovered model = %+v", models[0])
	}
	if models[0].ContextWindow != 262144 {
		t.Fatalf("discovered context window = %d, want 262144", models[0].ContextWindow)
	}
	if models[1].Name != "model-b" || models[1].LocalBaseURL != server.URL+"/v1" {
		t.Fatalf("second discovered model = %+v", models[1])
	}
	if !sawModels {
		t.Error("expected a request to /v1/models")
	}
	if !sawReasoning {
		t.Error("expected a best-effort probe of /api/v1/models (reasoning discovery)")
	}
}

func TestApplyLocalModelChoiceUsesDiscoveredModel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `{"data":[{"id":"remote-model"}]}`)
	}))
	defer server.Close()

	cfg := &Config{Provider: providerLocal, LocalModels: []localModelConfig{{
		ID: "configured-model", BaseURL: server.URL + "/v1",
	}}}
	if !applyLocalModelChoice(context.Background(), cfg, "remote-model") {
		t.Fatal("applyLocalModelChoice returned false")
	}
	if cfg.Model != "remote-model" || cfg.LocalBaseURL != server.URL+"/v1" {
		t.Fatalf("selected config = %+v", cfg)
	}
	if _, ok := cfg.localModelByID("remote-model"); !ok {
		t.Fatal("discovered model was not registered for provider construction")
	}
}

// TestApplyLocalModelChoiceWindowlessModelClearsInheritedWindow: selecting a
// model the endpoint exposes with no context metadata must clear the window a
// previous selection resolved (262144 in this case), so the pool does not
// keep representing the previous model. An explicit local_models window on the
// new model is authoritative and survives even though the endpoint says
// nothing.
func TestApplyLocalModelChoiceWindowlessModelClearsInheritedWindow(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/models" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = fmt.Fprint(w, `{"data":[{"id":"windowless-model"}]}`)
	}))
	defer server.Close()

	cfg := &Config{Provider: providerLocal, Model: "previous-model", LocalModels: []localModelConfig{{
		ID: "windowless-model", BaseURL: server.URL + "/v1",
	}}}
	cfg.ContextWindow = 262144 // inherited from the previous selection
	cfg.LocalContextWindow = 262144

	if !applyLocalModelChoice(context.Background(), cfg, "windowless-model") {
		t.Fatal("applyLocalModelChoice returned false")
	}
	if cfg.ContextWindow != 0 {
		t.Fatalf("ContextWindow = %d, want 0 (window-less model must not inherit the previous window)", cfg.ContextWindow)
	}
	if cfg.LocalContextWindow != 0 {
		t.Fatalf("LocalContextWindow = %d, want 0", cfg.LocalContextWindow)
	}
}

// TestApplyLocalModelChoiceConfigWindowSurvivesWindowlessEndpoint: the
// authoritative rule for local models — an explicitly configured
// local_models context_window wins over a silent endpoint, so a model whose
// server exposes no context metadata still resolves its configured window
// when (re)selected.
func TestApplyLocalModelChoiceConfigWindowSurvivesWindowlessEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/models" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		// The endpoint lists the model but exposes no context metadata.
		_, _ = fmt.Fprint(w, `{"data":[{"id":"configured-model"}]}`)
	}))
	defer server.Close()

	cfg := &Config{Provider: providerLocal, LocalModels: []localModelConfig{{
		ID: "configured-model", BaseURL: server.URL + "/v1", ContextWindow: 262144,
	}}}
	cfg.ContextWindow = 131072 // stale value from another model

	if !applyLocalModelChoice(context.Background(), cfg, "configured-model") {
		t.Fatal("applyLocalModelChoice returned false")
	}
	if cfg.ContextWindow != 262144 {
		t.Fatalf("ContextWindow = %d, want 262144 (the model's explicit config window)", cfg.ContextWindow)
	}
	if got := contextWindowForSelection(providerLocal, "configured-model", cfg); got != 262144 {
		t.Fatalf("contextWindowForSelection = %d, want 262144", got)
	}
}

func TestQueryLocalModels_NonOKStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	models, result := queryLocalModels(context.Background(), server.URL+"/models", "")
	if result != queryLocalModelsBadResponse {
		t.Fatalf("expected queryLocalModelsBadResponse for non-200 status, got %v models=%+v", result, models)
	}
}

func TestQueryLocalModels_MalformedJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `not json`)
	}))
	defer server.Close()

	if _, result := queryLocalModels(context.Background(), server.URL+"/models", ""); result != queryLocalModelsBadResponse {
		t.Fatalf("expected queryLocalModelsBadResponse for malformed JSON, got %v", result)
	}
}

func TestQueryLocalModels_EmptyDataList(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `{"data":[]}`)
	}))
	defer server.Close()

	models, result := queryLocalModels(context.Background(), server.URL+"/models", "")
	if result != queryLocalModelsOK {
		t.Fatalf("expected queryLocalModelsOK even with empty data (the endpoint responded successfully), got %v", result)
	}
	if len(models) != 0 {
		t.Fatalf("len(models) = %d, want 0", len(models))
	}
}

func TestQueryLocalModels_ConnectionRefused(t *testing.T) {
	// Port 1 is a privileged, normally-unbound port — no server listens there.
	if _, result := queryLocalModels(context.Background(), "http://127.0.0.1:1/models", ""); result != queryLocalModelsUnreachable {
		t.Fatalf("expected queryLocalModelsUnreachable for connection-refused endpoint, got %v", result)
	}
}

func TestContextWindowFromOpenAIModel_FallbackChain(t *testing.T) {
	tests := []struct {
		name  string
		model openAIModel
		want  int64
	}{
		{"context_length wins", openAIModel{ContextLength: 100, MaxContextLength: 200, MaxModelLength: 300, NumContext: 400}, 100},
		{"max_context_length fallback", openAIModel{MaxContextLength: 200, MaxModelLength: 300, NumContext: 400}, 200},
		{"max_model_len fallback", openAIModel{MaxModelLength: 300, NumContext: 400}, 300},
		{"num_ctx fallback", openAIModel{NumContext: 400}, 400},
		{"top_provider.context_length fallback (OpenRouter-style)", func() openAIModel {
			m := openAIModel{}
			m.TopProvider.ContextLength = 131072
			return m
		}(), 131072},
		{"none set", openAIModel{}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := contextWindowFromOpenAIModel(tt.model); got != tt.want {
				t.Errorf("contextWindowFromOpenAIModel(%+v) = %d, want %d", tt.model, got, tt.want)
			}
		})
	}
}

func TestQueryLocalModels_SuccessTranslation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(
			w,
			`{"data":[{"id":"model-a","name":"Model A","context_length":131072},{"id":"model-b","max_model_len":8192}]}`,
		)
	}))
	defer server.Close()

	models, result := queryLocalModels(context.Background(), server.URL+"/models", "")
	if result != queryLocalModelsOK {
		t.Fatalf("expected queryLocalModelsOK, got %v", result)
	}
	if len(models) != 2 {
		t.Fatalf("len(models) = %d, want 2", len(models))
	}
	if models[0].ID != "model-a" || models[0].Name != "Model A" || contextWindowFromOpenAIModel(models[0]) != 131072 {
		t.Errorf("first model = %+v", models[0])
	}
	if models[1].ID != "model-b" || contextWindowFromOpenAIModel(models[1]) != 8192 {
		t.Errorf("second model = %+v", models[1])
	}
}

func TestResolveLocalProviderConfigFallsBackFromStaleModel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `{"data":[{"id":"replacement-model","context_length":131072}]}`)
	}))
	defer server.Close()

	cfg := &Config{
		Provider: providerLocal,
		Model:    "removed-model",
		LocalModels: []localModelConfig{{
			ID:      "removed-model",
			BaseURL: server.URL + "/v1",
		}},
	}
	resolveLocalProviderConfig(context.Background(), cfg)

	if cfg.Model != "replacement-model" {
		t.Fatalf("model = %q, want replacement-model", cfg.Model)
	}
	if cfg.LocalBaseURL != server.URL+"/v1" {
		t.Fatalf("local base URL = %q, want %q", cfg.LocalBaseURL, server.URL+"/v1")
	}
}

// TestResolveLocalProviderConfigClearsStaleWindowOnFallback: when neither the
// selected (stale) model nor its replacement carries an explicitly configured
// window, the window left in cfg by a previous selection must be cleared, so
// the pool's compaction threshold and the status display do not silently keep
// using the previous model's window. (This is the exact regression from
// qwen3-coder-next → qwen3.8-27b: the window-less replacement inherits a 262k
// window it was never told about.)
func TestResolveLocalProviderConfigClearsStaleWindowOnFallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/models" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = fmt.Fprint(w, `{"data":[{"id":"replacement-model"}]}`)
	}))
	defer server.Close()

	cfg := &Config{
		Provider: providerLocal,
		Model:    "stale-model",
		LocalModels: []localModelConfig{{
			ID:      "stale-model",
			BaseURL: server.URL + "/v1",
		}},
	}
	// Simulate a prior selection that applied a window (e.g. a /model picker
	// swap before the restart): not an explicit user override.
	cfg.ContextWindow = 262144

	if !resolveLocalProviderConfig(context.Background(), cfg) {
		t.Fatal("resolveLocalProviderConfig = false, want true")
	}
	if cfg.Model != "replacement-model" {
		t.Fatalf("model = %q, want replacement-model", cfg.Model)
	}
	if cfg.ContextWindow != 0 {
		t.Fatalf("ContextWindow = %d, want 0 (window-less replacement must not inherit the previous model's window)", cfg.ContextWindow)
	}
	if cfg.LocalContextWindow != 0 {
		t.Fatalf("LocalContextWindow = %d, want 0", cfg.LocalContextWindow)
	}
}

// TestResolveLocalProviderConfigAdoptsReplacementWindowOnFallback: a windowed
// replacement overwrites the stale model's window, so the pool and display
// track the model actually in use.
func TestResolveLocalProviderConfigAdoptsReplacementWindowOnFallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/models" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = fmt.Fprint(w, `{"data":[{"id":"replacement-model","context_length":131072}]}`)
	}))
	defer server.Close()

	cfg := &Config{
		Provider: providerLocal,
		Model:    "stale-model",
		LocalModels: []localModelConfig{{
			ID:      "stale-model",
			BaseURL: server.URL + "/v1",
		}},
	}
	cfg.ContextWindow = 262144 // the stale model's window, as resolved at startup

	if !resolveLocalProviderConfig(context.Background(), cfg) {
		t.Fatal("resolveLocalProviderConfig = false, want true")
	}
	if cfg.Model != "replacement-model" {
		t.Fatalf("model = %q, want replacement-model", cfg.Model)
	}
	if cfg.ContextWindow != 131072 {
		t.Fatalf("ContextWindow = %d, want 131072 (the replacement's own window)", cfg.ContextWindow)
	}
	if cfg.LocalContextWindow != 131072 {
		t.Fatalf("LocalContextWindow = %d, want 131072", cfg.LocalContextWindow)
	}
}

// TestResolveLocalProviderConfigHonorsExplicitWindowOnFallback: an explicit
// user override (WLLR_CONTEXT_WINDOW / config context_window) must survive the
// stale-selection fallback even when the replacement model carries a window of
// its own — ContextWindowConfigured is the contract that "configured" means
// the user, not the model metadata.
func TestResolveLocalProviderConfigHonorsExplicitWindowOnFallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/models" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = fmt.Fprint(w, `{"data":[{"id":"replacement-model","context_length":131072}]}`)
	}))
	defer server.Close()

	cfg := &Config{
		Provider:                providerLocal,
		Model:                   "stale-model",
		ContextWindow:           500000,
		ContextWindowConfigured: true,
		LocalModels: []localModelConfig{{
			ID:      "stale-model",
			BaseURL: server.URL + "/v1",
		}},
	}

	resolveLocalProviderConfig(context.Background(), cfg)
	if cfg.ContextWindow != 500000 {
		t.Fatalf("ContextWindow = %d, want 500000 (explicit override preserved)", cfg.ContextWindow)
	}
}

// TestDiscoverLocalModelsConfiguredWindowWinsOverEndpoint: an explicitly
// configured window is authoritative over a conflicting endpoint value (a model
// swap that did not update the config) — discovery only fills in a window when
// none is configured, and logs when the two disagree.
func TestDiscoverLocalModelsConfiguredWindowWinsOverEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			_, _ = fmt.Fprint(w, `{"data":[{"id":"swapped-model","context_length":131072}]}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	cfg := &Config{LocalModels: []localModelConfig{{
		ID:            "swapped-model",
		BaseURL:       server.URL + "/v1",
		ContextWindow: 262144,
	}}}
	models := discoverLocalModels(context.Background(), cfg)
	if len(models) != 1 {
		t.Fatalf("len(models) = %d, want 1: %+v", len(models), models)
	}
	if models[0].ContextWindow != 262144 {
		t.Fatalf("ContextWindow = %d, want 262144 (explicit config wins over the endpoint's value)", models[0].ContextWindow)
	}
}

func TestProbeLocalModelsEndpoint_BareURLWorks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			t.Errorf("request path = %q, want /models", r.URL.Path)
		}
		_, _ = fmt.Fprint(w, `{"data":[{"id":"model-a"}]}`)
	}))
	defer server.Close()

	models, resolvedBase, result := probeLocalModelsEndpoint(context.Background(), server.URL, "")
	if result != queryLocalModelsOK {
		t.Fatalf("result = %v, want queryLocalModelsOK", result)
	}
	if len(models) != 1 || models[0].ID != "model-a" {
		t.Fatalf("models = %+v", models)
	}
	if resolvedBase != server.URL {
		t.Fatalf("resolvedBase = %q, want %q", resolvedBase, server.URL)
	}
}

func TestProbeLocalModelsEndpoint_FallsBackToV1(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			_, _ = fmt.Fprint(w, `{"data":[{"id":"model-a"}]}`)
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = fmt.Fprint(w, `{"error":{"message":"unknown endpoint"}}`)
		}
	}))
	defer server.Close()

	// Simulates a user typing the bare host:port without the "/v1" suffix
	// their OpenAI-compatible server actually expects.
	models, resolvedBase, result := probeLocalModelsEndpoint(context.Background(), server.URL, "")
	if result != queryLocalModelsOK {
		t.Fatalf("result = %v, want queryLocalModelsOK", result)
	}
	if len(models) != 1 || models[0].ID != "model-a" {
		t.Fatalf("models = %+v", models)
	}
	wantBase := server.URL + "/v1"
	if resolvedBase != wantBase {
		t.Fatalf("resolvedBase = %q, want %q (the working path, not the bare input)", resolvedBase, wantBase)
	}
}

func TestProbeLocalModelsEndpoint_AllPathsFail(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	models, resolvedBase, result := probeLocalModelsEndpoint(context.Background(), server.URL, "")
	if result != queryLocalModelsBadResponse {
		t.Fatalf("result = %v, want queryLocalModelsBadResponse (server was reached, just had nothing usable)", result)
	}
	if models != nil || resolvedBase != "" {
		t.Fatalf("models=%+v resolvedBase=%q, want both empty", models, resolvedBase)
	}
}

func TestProbeLocalModelsEndpoint_Unreachable(t *testing.T) {
	// Port 1 is a privileged, normally-unbound port — no server listens there,
	// so every candidate path fails to connect at all.
	models, resolvedBase, result := probeLocalModelsEndpoint(context.Background(), "http://127.0.0.1:1", "")
	if result != queryLocalModelsUnreachable {
		t.Fatalf("result = %v, want queryLocalModelsUnreachable", result)
	}
	if models != nil || resolvedBase != "" {
		t.Fatalf("models=%+v resolvedBase=%q, want both empty", models, resolvedBase)
	}
}

func TestProbeLocalModelsEndpoint_EmptyURL(t *testing.T) {
	_, _, result := probeLocalModelsEndpoint(context.Background(), "  ", "")
	if result != queryLocalModelsUnreachable {
		t.Fatalf("result = %v, want queryLocalModelsUnreachable", result)
	}
}
