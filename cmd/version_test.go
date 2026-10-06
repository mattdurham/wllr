package main

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

// withBuildInfo sets the version vars for the duration of the test and restores
// them afterwards. The vars are package state, so tests using this helper must
// not run in parallel with anything that reads them.
func withBuildInfo(t *testing.T, commit, date string) {
	t.Helper()
	savedCommit, savedDate := versionCommit, versionDate
	t.Cleanup(func() { versionCommit, versionDate = savedCommit, savedDate })
	versionCommit, versionDate = commit, date
}

func TestRunVersionCommand_PrintsCommitAndDate(t *testing.T) {
	withBuildInfo(t, "abc123def456", "2026-04-05T10:42:00Z")
	var stdout, stderr bytes.Buffer
	if code := runVersionCommand(nil, &stdout, &stderr); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(stdout.String(), "abc123def456") || !strings.Contains(stdout.String(), "2026-04-05T10:42:00Z") {
		t.Fatalf("stdout = %q, want commit and build date", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func TestRunVersionCommand_UnknownWithoutLdflags(t *testing.T) {
	withBuildInfo(t, "", "")
	var stdout bytes.Buffer
	if code := runVersionCommand(nil, &stdout, io.Discard); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(stdout.String(), "unknown") {
		t.Fatalf("stdout = %q, want unknown placeholders", stdout.String())
	}
}
