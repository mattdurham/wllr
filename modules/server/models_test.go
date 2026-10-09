package server

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mattdurham/wllr/modules/sdk"
)

// modelsResponse mirrors the GET /api/models body for decoding in tests.
type modelsResponse struct {
	Models []struct {
		ID       string   `json:"id"`
		Name     string   `json:"name"`
		Sublabel string   `json:"sublabel"`
		Provider string   `json:"provider"`
		Tiers    []string `json:"tiers"`
		Active   bool     `json:"active"`
		Key      string   `json:"key"`
	} `json:"models"`
	Callback string `json:"callback"`
	Current  struct {
		Provider string `json:"provider"`
		Model    string `json:"model"`
	} `json:"current"`
}

// TestHandleModelsListsChoices proves the endpoint exposes the installed list
// with provider-qualified keys and the shared callback id, so a client can
// render a model menu and switch models without knowing the key encoding.
func TestHandleModelsListsChoices(t *testing.T) {
	pool, _ := newTestPool(t)
	srv := New(pool, Options{})
	srv.SetModelLister(sdk.ModelListerFunc(func() []sdk.ModelChoice {
		return []sdk.ModelChoice{
			{ID: "gpt-x", Name: "GPT X", Provider: "openai", Tiers: []string{"high"}, Active: true},
			{ID: "claude", Name: "Claude", Provider: "anthropic", Sublabel: "128k"},
		}
	}))

	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/models", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body modelsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Models) != 2 {
		t.Fatalf("models = %d, want 2", len(body.Models))
	}
	if body.Callback != sdk.ModelPickerCallback {
		t.Errorf("callback = %q, want %q", body.Callback, sdk.ModelPickerCallback)
	}
	// The key must be the shared provider-qualified form, and must split back to
	// exactly the provider and model the harness picker would decode.
	provider, id, ok := sdk.SplitModelPickerKey(body.Models[0].Key)
	if !ok || provider != "openai" || id != "gpt-x" {
		t.Errorf("key %q split to (%q,%q,%v), want (openai,gpt-x,true)",
			body.Models[0].Key, provider, id, ok)
	}
	if !body.Models[0].Active {
		t.Error("active flag not propagated")
	}
	if len(body.Models[0].Tiers) != 1 || body.Models[0].Tiers[0] != "high" {
		t.Errorf("tiers = %v, want [high]", body.Models[0].Tiers)
	}
	// Current reflects the pool's live provider/model so the client can label.
	if body.Current.Model != "fake-model" {
		t.Errorf("current.model = %q, want fake-model", body.Current.Model)
	}
}

// TestHandleModelsNilLister proves an uninstalled lister yields an empty list
// rather than an error, so the client degrades to "no models configured".
func TestHandleModelsNilLister(t *testing.T) {
	srv := New(nil, Options{})
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/models", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body modelsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Models) != 0 {
		t.Errorf("models = %d, want 0", len(body.Models))
	}
}

// TestHandleModelsRejectsNonGet proves the endpoint is read-only.
func TestHandleModelsRejectsNonGet(t *testing.T) {
	srv := New(nil, Options{})
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/models", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

// TestModelSelectionRoundTrip proves the whole browser model-selection path: the
// key served by GET /api/models, POSTed to /api/ui/callback, arrives at the
// router's core handler with the provider-qualified value the harness dispatch
// decodes. This is the property that makes selecting a model from a browser work.
func TestModelSelectionRoundTrip(t *testing.T) {
	pool, _ := newTestPool(t)
	srv := New(pool, Options{})
	srv.SetModelLister(sdk.ModelListerFunc(func() []sdk.ModelChoice {
		return []sdk.ModelChoice{{ID: "gpt-x", Provider: "openai", Active: true}}
	}))

	var gotCallback string
	var gotArgs []string
	srv.SetCallbackRouter(&sdk.FuncRouter{
		Core: func(_ context.Context, callback string, args []string) error {
			gotCallback, gotArgs = callback, args
			return nil
		},
	})

	// Read the key the server hands the client.
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/models", nil))
	var body modelsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode models: %v", err)
	}
	key := body.Models[0].Key

	// Post it back as a selection.
	payload, _ := json.Marshal(map[string]string{"callback": body.Callback, "value": key})
	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/ui/callback", bytes.NewReader(payload))
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("callback status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if gotCallback != sdk.ModelPickerCallback {
		t.Errorf("routed callback = %q, want %q", gotCallback, sdk.ModelPickerCallback)
	}
	provider, id, _ := sdk.SplitModelPickerKey(gotArgs[0])
	if provider != "openai" || id != "gpt-x" {
		t.Errorf("routed value %q split to (%q,%q), want (openai,gpt-x)", gotArgs[0], provider, id)
	}
}
