package server

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mattdurham/wllr/modules/sdk"
)

// recordingRouter captures the callback it was asked to route.
type recordingRouter struct {
	callback string
	args     []string
	err      error
}

func (r *recordingRouter) RouteCallback(_ context.Context, callback string, args []string) error {
	r.callback, r.args = callback, args
	return r.err
}

func postCallback(t *testing.T, h http.Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/ui/callback", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestUICallback_NoRouterIsNotImplemented(t *testing.T) {
	pool, _ := newTestPool(t)
	srv := New(pool, Options{})
	rec := postCallback(t, srv.Handler(), `{"callback":"agents:focus","value":"main"}`)
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want 501", rec.Code)
	}
}

func TestUICallback_RoutesValueAsSingleArg(t *testing.T) {
	pool, _ := newTestPool(t)
	srv := New(pool, Options{})
	rr := &recordingRouter{}
	srv.SetCallbackRouter(rr)

	rec := postCallback(t, srv.Handler(), `{"callback":"agents:focus","value":"main/coder"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if rr.callback != "agents:focus" || len(rr.args) != 1 || rr.args[0] != "main/coder" {
		t.Fatalf("router got %q %v", rr.callback, rr.args)
	}
}

func TestUICallback_ExplicitArgsOverrideValue(t *testing.T) {
	pool, _ := newTestPool(t)
	srv := New(pool, Options{})
	rr := &recordingRouter{}
	srv.SetCallbackRouter(rr)

	rec := postCallback(t, srv.Handler(), `{"callback":"cb","value":"ignored","args":["a","b"]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if len(rr.args) != 2 || rr.args[0] != "a" || rr.args[1] != "b" {
		t.Fatalf("router args = %v, want [a b]", rr.args)
	}
}

func TestUICallback_RequiresCallback(t *testing.T) {
	pool, _ := newTestPool(t)
	srv := New(pool, Options{})
	srv.SetCallbackRouter(&recordingRouter{})
	rec := postCallback(t, srv.Handler(), `{"value":"x"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestUICallback_BadJSON(t *testing.T) {
	pool, _ := newTestPool(t)
	srv := New(pool, Options{})
	srv.SetCallbackRouter(&recordingRouter{})
	rec := postCallback(t, srv.Handler(), `{not json`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestUICallback_MethodNotAllowed(t *testing.T) {
	pool, _ := newTestPool(t)
	srv := New(pool, Options{})
	srv.SetCallbackRouter(&recordingRouter{})
	req := httptest.NewRequest(http.MethodGet, "/api/ui/callback", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

// The "unsupported here" sentinels must answer 501, not 500, so a client can
// fall back rather than treat a missing handler as a fault.
func TestUICallback_UnsupportedSentinelsAre501(t *testing.T) {
	for _, sentinel := range []error{sdk.ErrNoCoreHandler, sdk.ErrNoExtensionHost} {
		pool, _ := newTestPool(t)
		srv := New(pool, Options{})
		srv.SetCallbackRouter(&recordingRouter{err: sentinel})
		rec := postCallback(t, srv.Handler(), `{"callback":"__wllr:model","value":"x"}`)
		if rec.Code != http.StatusNotImplemented {
			t.Fatalf("sentinel %v: status = %d, want 501", sentinel, rec.Code)
		}
	}
}

func TestUICallback_OtherErrorIs500(t *testing.T) {
	pool, _ := newTestPool(t)
	srv := New(pool, Options{})
	srv.SetCallbackRouter(&recordingRouter{err: errors.New("dispatch failed")})
	rec := postCallback(t, srv.Handler(), `{"callback":"agents:focus","value":"main"}`)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}
