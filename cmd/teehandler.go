package main

import (
	"context"
	"log/slog"
)

// teeHandler fans slog records out to a fixed set of sub-handlers. In wllr it
// composes the stderr handler (exec/headless mode) with the WASM log dispatcher
// (dispatchLogHandler), which forwards records to the bundled `logging`
// extension. File writing is no longer done in core — see cmd/loghandler.go.
type teeHandler struct {
	handlers []slog.Handler
}

// newTee builds a teeHandler over the given non-nil sub-handlers.
func newTee(handlers ...slog.Handler) *teeHandler {
	out := make([]slog.Handler, 0, len(handlers))
	for _, h := range handlers {
		if h != nil {
			out = append(out, h)
		}
	}
	return &teeHandler{handlers: out}
}

func (h *teeHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, sub := range h.handlers {
		if sub.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (h *teeHandler) Handle(ctx context.Context, r slog.Record) error {
	for _, sub := range h.handlers {
		if sub.Enabled(ctx, r.Level) {
			_ = sub.Handle(ctx, r)
		}
	}
	return nil
}

func (h *teeHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	subs := make([]slog.Handler, len(h.handlers))
	for i, sub := range h.handlers {
		subs[i] = sub.WithAttrs(attrs)
	}
	return &teeHandler{handlers: subs}
}

func (h *teeHandler) WithGroup(name string) slog.Handler {
	subs := make([]slog.Handler, len(h.handlers))
	for i, sub := range h.handlers {
		subs[i] = sub.WithGroup(name)
	}
	return &teeHandler{handlers: subs}
}
