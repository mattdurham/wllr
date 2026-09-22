package main

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"net/http/pprof"
	"os"
	"time"
)

const fallbackPprofAddr = "127.0.0.1:6060"

// metricsPath is the Prometheus exposition endpoint, served on the debug
// listener alongside the pprof endpoints.
const metricsPath = "/metrics"

func defaultPprofAddr() string {
	if addr := os.Getenv("WLLR_PPROF_ADDR"); addr != "" {
		return addr
	}
	return fallbackPprofAddr
}

func startPprofServer(addr string) func() {
	return startDebugServer(addr, nil)
}

// startDebugServer serves the profiling endpoints plus, when metrics is
// non-nil, the Prometheus exposition at /metrics. One listener serves both so a
// local scraper and a profiler target the same address.
func startDebugServer(addr string, metrics *wllrMetrics) func() {
	if addr == "" {
		return func() {}
	}

	mux := newPprofMux()
	if metrics != nil {
		mux.Handle(metricsPath, metrics.handler())
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		slog.Warn("wllr: pprof listen failed", "addr", addr, "error", err)
		return func() {}
	}

	server := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		slog.Info("wllr: pprof listening", "addr", listener.Addr().String())
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			slog.Warn("wllr: pprof server stopped", "addr", listener.Addr().String(), "error", err)
		}
	}()

	return func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			slog.Warn("wllr: pprof shutdown failed", "addr", listener.Addr().String(), "error", err)
		}
	}
}

func newPprofMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	return mux
}
