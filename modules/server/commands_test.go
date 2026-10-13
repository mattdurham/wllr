package server

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mattdurham/wllr/modules/sdk"
)

// commandsResponse mirrors the GET /api/commands body for decoding in tests.
type commandsResponse struct {
	Commands []sdk.CommandInfo `json:"commands"`
	Prefix   string            `json:"prefix"`
}

// TestHandleCommandsListsRegistered proves the endpoint exposes the installed
// command list with descriptions and the instant flag, so a browser can offer
// an autocomplete dropdown and help identical to the terminal's.
func TestHandleCommandsListsRegistered(t *testing.T) {
	srv := New(nil, Options{})
	srv.SetCommandLister(sdk.CommandListerFunc(func() []sdk.CommandInfo {
		return []sdk.CommandInfo{
			{Name: "help", Desc: "Show available commands", Instant: true},
			{Name: "history", Desc: "Browse previous conversations"},
		}
	}))

	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/commands", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body commandsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Commands) != 2 {
		t.Fatalf("commands = %d, want 2", len(body.Commands))
	}
	if body.Commands[0].Name != "help" || !body.Commands[0].Instant {
		t.Errorf("command[0] = %+v, want help/instant", body.Commands[0])
	}
	if body.Commands[0].Desc == "" {
		t.Error("description not propagated")
	}
	if body.Prefix != "/" {
		t.Errorf("prefix = %q, want /", body.Prefix)
	}
}

// TestHandleCommandsNilLister proves an uninstalled lister yields an empty list
// rather than an error, so the client degrades to no autocomplete.
func TestHandleCommandsNilLister(t *testing.T) {
	srv := New(nil, Options{})
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/commands", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body commandsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Commands) != 0 {
		t.Errorf("commands = %d, want 0", len(body.Commands))
	}
}

// TestHandleCommandsRejectsNonGet proves the endpoint is read-only.
func TestHandleCommandsRejectsNonGet(t *testing.T) {
	srv := New(nil, Options{})
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/commands", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

// TestHubSinkRegisterCommandForwards proves an extension command registered
// after serve-mode wiring reaches the harness registry (via the installed
// registrar) instead of being dropped, so the browser's command list stays
// complete.
func TestHubSinkRegisterCommandForwards(t *testing.T) {
	sink := NewHubSink(nil, NewHub(8), "main")
	var gotName, gotDesc string
	var gotInstant bool
	sink.SetCommandRegistrar(func(name, desc string, instant bool) {
		gotName, gotDesc, gotInstant = name, desc, instant
	})
	if err := sink.RegisterCommand("custom", "a custom thing", true); err != nil {
		t.Fatalf("RegisterCommand: %v", err)
	}
	if gotName != "custom" || gotDesc != "a custom thing" || !gotInstant {
		t.Errorf("registrar got (%q,%q,%v), want (custom,a custom thing,true)", gotName, gotDesc, gotInstant)
	}
}

// TestHubSinkRegisterCommandNoRegistrar proves registration without a registrar
// is a silent success, so a server without one cannot fail an extension call.
func TestHubSinkRegisterCommandNoRegistrar(t *testing.T) {
	sink := NewHubSink(nil, NewHub(8), "main")
	if err := sink.RegisterCommand("x", "y", false); err != nil {
		t.Fatalf("RegisterCommand without registrar: %v", err)
	}
}
