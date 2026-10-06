package main

import (
	"flag"
	"fmt"
	"io"
	"runtime"
)

// Build metadata injected at link time by the Makefile:
//
//	go build -ldflags "-X main.versionCommit=... -X main.versionDate=..."
//
// Empty values (a plain `go build` without ldflags) print as "unknown", so the
// command stays honest about binaries whose provenance was not recorded.
var (
	versionCommit string
	versionDate   string
)

// runVersionCommand prints the build commit and date. It mirrors
// runLoginCommand's signature (args + writers + exit code) so it is testable
// without spawning the binary. Unknown flags are rejected with exit code 2,
// matching flag package conventions.
func runVersionCommand(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("version", flag.ContinueOnError)
	fs.SetOutput(stderr)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	commit, date := versionCommit, versionDate
	if commit == "" {
		commit = "unknown"
	}
	if date == "" {
		date = "unknown"
	}
	_, _ = fmt.Fprintf(stdout, "wllr %s\nbuilt %s (%s/%s)\n", commit, date, runtime.GOOS, runtime.GOARCH)
	return 0
}
