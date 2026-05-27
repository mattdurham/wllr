package main

import (
	"log/slog"
	"os"
)

// teeHandler fans slog records out to stderr and a rolling log file under
// ~/.wllr/logs/. Each process run gets its own timestamped file.
// Log file errors are silently ignored so a missing directory never breaks the app.
type teeHandler struct {
	stderr  slog.Handler
	file    slog.Handler
	logFile *os.File
}
