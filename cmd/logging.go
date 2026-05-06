package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

// teeHandler fans slog records out to stderr and a rolling log file under
// ~/.wllr/logs/. Each process run gets its own timestamped file.
// Log file errors are silently ignored so a missing directory never breaks the app.
type teeHandler struct {
	stderr  slog.Handler
	file    slog.Handler
	logFile *os.File
}

// newTeeHandler creates a handler that writes to the log file and optionally stderr.
// Pass writeStderr=false in TUI mode to prevent log output bleeding into the display.
func newTeeHandler(stderr *os.File, writeStderr bool) *teeHandler {
	var stderrH slog.Handler
	if writeStderr && stderr != nil {
		stderrH = slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: slog.LevelInfo})
	}

	logDir := logDir()
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return &teeHandler{stderr: stderrH}
	}

	name := fmt.Sprintf("%s.log", time.Now().Format("2006-01-02T15-04-05"))
	f, err := os.OpenFile(filepath.Join(logDir, name), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return &teeHandler{stderr: stderrH}
	}

	fileH := slog.NewTextHandler(f, &slog.HandlerOptions{Level: slog.LevelDebug})
	return &teeHandler{stderr: stderrH, file: fileH, logFile: f}
}

func (h *teeHandler) Enabled(ctx context.Context, level slog.Level) bool {
	if h.stderr != nil && h.stderr.Enabled(ctx, level) {
		return true
	}
	if h.file != nil && h.file.Enabled(ctx, level) {
		return true
	}
	return false
}

func (h *teeHandler) Handle(ctx context.Context, r slog.Record) error {
	if h.stderr != nil {
		_ = h.stderr.Handle(ctx, r)
	}
	if h.file != nil {
		_ = h.file.Handle(ctx, r)
	}
	return nil
}

func (h *teeHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	n := &teeHandler{logFile: h.logFile}
	if h.stderr != nil {
		n.stderr = h.stderr.WithAttrs(attrs)
	}
	if h.file != nil {
		n.file = h.file.WithAttrs(attrs)
	}
	return n
}

func (h *teeHandler) WithGroup(name string) slog.Handler {
	n := &teeHandler{logFile: h.logFile}
	if h.stderr != nil {
		n.stderr = h.stderr.WithGroup(name)
	}
	if h.file != nil {
		n.file = h.file.WithGroup(name)
	}
	return n
}

func logDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".wllr/logs"
	}
	return filepath.Join(home, ".wllr", "logs")
}
