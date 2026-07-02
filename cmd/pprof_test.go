package main

import (
	"net/http/httptest"
	"testing"
)

func TestDefaultPprofAddrFallsBackToLocalhost(t *testing.T) {
	t.Setenv("WLLR_PPROF_ADDR", "")

	if got := defaultPprofAddr(); got != fallbackPprofAddr {
		t.Fatalf("defaultPprofAddr() = %q, want %q", got, fallbackPprofAddr)
	}
}

func TestDefaultPprofAddrUsesEnv(t *testing.T) {
	t.Setenv("WLLR_PPROF_ADDR", "127.0.0.1:0")

	if got := defaultPprofAddr(); got != "127.0.0.1:0" {
		t.Fatalf("defaultPprofAddr() = %q, want env value", got)
	}
}

func TestStartPprofServerDisabled(t *testing.T) {
	cleanup := startPprofServer("")
	cleanup()
}

func TestPprofMuxServesIndex(t *testing.T) {
	req := httptest.NewRequest("GET", "/debug/pprof/", nil)
	rec := httptest.NewRecorder()

	newPprofMux().ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("GET /debug/pprof/ status = %d, want 200", rec.Code)
	}
}
