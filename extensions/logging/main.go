//go:build wasip1

// Package main is the logging extension for wllr.
//
// It is the bundled log sink: it subscribes to EventLog and appends each
// record to a rolling, per-run log file under ~/.wllr/logs/<timestamp>.log,
// reproducing the behaviour of the file handler it replaced (slog text format,
// Debug level, one timestamped file per process run). File writing used to live
// in the Go core; moving it here makes the log sink a swappable WASM component
// and lets any extension hook logs via OnLog.
package main

import (
	"fmt"
	"strings"
	"time"
)

// logFilePath is the rolling per-run log file, resolved lazily on the first log
// batch (see resolveLogPath). Lazy resolution — rather than waiting for
// session_start — ensures the buffered startup logs the host flushes from its
// ring buffer are captured, matching the old core handler which wrote from
// process start.
var logFilePath string

func init() {
	OnLog(func(records []LogRecord) {
		if len(records) == 0 {
			return
		}
		// Derive the per-run filename from the FIRST record's timestamp, not the
		// WASM clock: time.Now() inside the wasip1 sandbox is not real wall-clock,
		// whereas record.Time is stamped by the host. This keeps the filename a
		// real per-run timestamp, matching the core handler it replaced.
		path := resolveLogPath(records[0].Time)
		var sb strings.Builder
		for _, r := range records {
			sb.WriteString(formatRecord(r))
		}
		if sb.Len() > 0 {
			_ = AppendFile(path, sb.String())
		}
	})
}

// resolveLogPath returns the per-run log file path, computing it once from the
// first record's host timestamp (RFC3339Nano). All records from one run share a
// single ~/.wllr/logs/<timestamp>.log file.
func resolveLogPath(firstRecordTime string) string {
	if logFilePath != "" {
		return logFilePath
	}
	stamp := fileStamp(firstRecordTime)
	name := stamp + ".log"
	home, err := GetEnv("HOME")
	if err != nil || home == "" {
		logFilePath = ".wllr/logs/" + name
	} else {
		logFilePath = home + "/.wllr/logs/" + name
	}
	return logFilePath
}

// fileStamp converts an RFC3339Nano timestamp to the log filename stamp
// "2006-01-02T15-04-05". Falls back to a fixed stamp if parsing fails (never
// uses the WASM clock, which is not real wall-clock).
func fileStamp(rfc3339 string) string {
	t, err := time.Parse(time.RFC3339Nano, rfc3339)
	if err != nil {
		return "session"
	}
	return t.Format("2006-01-02T15-04-05")
}

// formatRecord renders one record in slog's text style:
//
//	time=<ts> level=<LEVEL> msg=<message> key=value ...
func formatRecord(r LogRecord) string {
	var sb strings.Builder
	sb.WriteString("time=")
	sb.WriteString(r.Time)
	sb.WriteString(" level=")
	sb.WriteString(strings.ToUpper(r.Level))
	sb.WriteString(" msg=")
	sb.WriteString(quoteIfNeeded(r.Message))
	for _, a := range r.Attrs {
		sb.WriteString(" ")
		sb.WriteString(a.Key)
		sb.WriteString("=")
		sb.WriteString(quoteIfNeeded(a.Value))
	}
	sb.WriteString("\n")
	return sb.String()
}

// quoteIfNeeded mirrors slog.TextHandler: values containing spaces, quotes, or
// equals signs are double-quoted via %q; otherwise emitted bare.
func quoteIfNeeded(s string) string {
	if s == "" {
		return `""`
	}
	if strings.ContainsAny(s, " \t\n\"=") {
		return fmt.Sprintf("%q", s)
	}
	return s
}

func main() {}
