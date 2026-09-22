package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

// TestLocalModelWindowDoesNotLeakFromSession guards against a model inheriting
// the session's context window. cfg.ContextWindow / cfg.LocalContextWindow are
// session-scoped (they describe only the selected model), so using them for
// another local model made a tier target report the wrong window.
func TestLocalModelWindowDoesNotLeakFromSession(t *testing.T) {
	path := withConfigPath(t)
	os.WriteFile(path, []byte(`{"wllr":{
	  "provider":"local","model":"big",
	  "local_models":[
	    {"id":"big","base_url":"http://127.0.0.1:1/v1","context_window":262144},
	    {"id":"small","base_url":"http://127.0.0.1:2/v1"}
	  ]}}`), 0o600)

	cfg, _ := LoadConfig()
	if got := contextWindowForSelection(providerLocal, "big", cfg); got != 262144 {
		t.Fatalf("big window = %d, want 262144", got)
	}
	// "small" has no configured window; it must NOT inherit big's 262144.
	if got := contextWindowForSelection(providerLocal, "small", cfg); got != 0 {
		t.Fatalf("small window = %d, want 0 (session window leaked)", got)
	}
}

// TestLocalModelWindowFromDiscovery verifies a non-selected local model
// resolves its own endpoint-advertised window (the tier case), rather than the
// session model's or nothing.
func TestLocalModelWindowFromDiscovery(t *testing.T) {
	resetLocalWindowState()
	defer resetLocalWindowState()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[
		  {"id":"small","context_length":8192},
		  {"id":"big","context_length":262144}
		]}`))
	}))
	defer srv.Close()

	path := withConfigPath(t)
	if err := os.WriteFile(path, []byte(`{"wllr":{
	  "provider":"local","model":"big",
	  "model_tiers":{"low":"small"},
	  "local_models":[
	    {"id":"big","base_url":"`+srv.URL+`/v1"},
	    {"id":"small","base_url":"`+srv.URL+`/v1"}
	  ]}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if discovered := discoverLocalModels(context.Background(), cfg); len(discovered) != 2 {
		t.Fatalf("discovered %d models, want 2", len(discovered))
	}
	if got := contextWindowForSelection(providerLocal, "small", cfg); got != 8192 {
		t.Errorf("small window = %d, want 8192 (its own advertised window)", got)
	}
	if got := contextWindowForSelection(providerLocal, "big", cfg); got != 262144 {
		t.Errorf("big window = %d, want 262144", got)
	}
}

// TestLocalDiscoveredWindowBeatsStaleSessionWindow covers the switch case: after
// a tier applies, cfg.Model is the new model while cfg.ContextWindow may still
// hold the previous model's window. The model's own discovered window must win.
func TestLocalDiscoveredWindowBeatsStaleSessionWindow(t *testing.T) {
	resetLocalWindowState()
	defer resetLocalWindowState()

	path := withConfigPath(t)
	if err := os.WriteFile(path, []byte(`{"wllr":{
	  "provider":"local","model":"big",
	  "local_models":[
	    {"id":"big","base_url":"http://127.0.0.1:1/v1","context_window":262144},
	    {"id":"small","base_url":"http://127.0.0.1:2/v1"}
	  ]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ContextWindow != 262144 {
		t.Fatalf("precondition: session window = %d, want 262144", cfg.ContextWindow)
	}
	rememberDiscoveredLocalWindow("small", 8192)
	cfg.Model = "small" // tier applied; session window is now stale
	if got := contextWindowForSelection(providerLocal, "small", cfg); got != 8192 {
		t.Errorf("small window = %d, want 8192 (its own discovered window)", got)
	}
}

// TestTierModelFromDiscoveryIsUsable covers tagging a model the /models picker
// listed from endpoint discovery (not declared in local_models). Resolving and
// applying such a tier must work: the model is remembered for the session at
// apply time, exactly as selecting it from /models would.
func TestTierModelFromDiscoveryIsUsable(t *testing.T) {
	resetLocalWindowState()
	defer resetLocalWindowState()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[
		  {"id":"main-model","context_length":262144},
		  {"id":"worker-model","context_length":32768}
		]}`))
	}))
	defer srv.Close()

	path := withConfigPath(t)
	if err := os.WriteFile(path, []byte(`{"wllr":{
	  "provider":"local","model":"main-model",
	  "model_tiers":{"low":{"provider":"local","model":"worker-model"}},
	  "local_models":[{"id":"main-model","base_url":"`+srv.URL+`/v1"}]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}

	prov, model, _, err := resolveModelTier("low", cfg.Provider, cfg)
	if err != nil {
		t.Fatalf("resolveModelTier on a discovered model: %v", err)
	}
	if !applyLocalModelChoice(context.Background(), cfg, model) {
		t.Fatal("applyLocalModelChoice rejected the discovered tier model")
	}
	if w := resolveLocalModelWindow(context.Background(), cfg, model); w != 32768 {
		t.Errorf("worker window = %d, want 32768 (its own advertised window)", w)
	}
	lm, err := modelForProviderTier(context.Background(), cfg, prov, model)
	if err != nil {
		t.Fatalf("build tier model: %v", err)
	}
	if lm.Model() != "worker-model" {
		t.Errorf("model = %q, want worker-model", lm.Model())
	}
}

// TestSubagentTierDiscoveredModelUsable covers the sub-agent path for a tier
// whose local model was only endpoint-discovered. The resolver must remember
// the model and resolve its own window; otherwise it silently falls back to the
// session model, quietly defeating the cheap-worker-tier setup.
func TestSubagentTierDiscoveredModelUsable(t *testing.T) {
	resetLocalWindowState()
	defer resetLocalWindowState()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"worker-model","context_length":32768}]}`))
	}))
	defer srv.Close()

	path := withConfigPath(t)
	os.WriteFile(path, []byte(`{"wllr":{
	  "provider":"local","model":"main-model",
	  "model_tiers":{"low":{"provider":"local","model":"worker-model"}},
	  "local_models":[{"id":"main-model","base_url":"`+srv.URL+`/v1"}]}}`), 0o600)
	cfg, _ := LoadConfig()

	prov, model, _, err := resolveModelTier("low", cfg.Provider, cfg)
	if err != nil {
		t.Fatal(err)
	}
	// The resolver remembers a discovered local model before building it.
	if !applyLocalModelChoice(context.Background(), cfg, model) {
		t.Fatal("discovered tier model not available")
	}
	if cw := resolveLocalModelWindow(context.Background(), cfg, model); cw != 32768 {
		t.Fatalf("cw = %d, want 32768", cw)
	}
	lm, err := modelForProviderTier(context.Background(), cfg, prov, model)
	if err != nil {
		t.Fatalf("sub-agent tier model unusable: %v", err)
	}
	if lm.Model() != "worker-model" {
		t.Fatalf("model = %q", lm.Model())
	}
}
