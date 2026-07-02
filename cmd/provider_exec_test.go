package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestRunShellCommand_ReturnsOutput(t *testing.T) {
	out, err := runShellCommand(context.Background(), `printf ok`, "", time.Second)
	if err != nil {
		t.Fatalf("runShellCommand: %v", err)
	}
	if out != "ok" {
		t.Fatalf("output = %q, want ok", out)
	}
}

func TestRunShellCommand_TimesOut(t *testing.T) {
	start := time.Now()
	out, err := runShellCommand(context.Background(), `printf start; sleep 5`, "", 100*time.Millisecond)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if err != errExecTimedOut {
		t.Fatalf("error = %v, want errExecTimedOut", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("timeout took too long: %s", elapsed)
	}
	if !strings.Contains(out, "start") {
		t.Fatalf("output = %q, want partial output", out)
	}
}

func TestRunShellCommand_TimeoutKillsChildProcess(t *testing.T) {
	dir := t.TempDir()
	pidPath := filepath.Join(dir, "child.pid")
	cmd := fmt.Sprintf(`sleep 30 & echo $! > %s; wait`, shellQuote(pidPath))

	_, err := runShellCommand(context.Background(), cmd, "", 100*time.Millisecond)
	if err != errExecTimedOut {
		t.Fatalf("error = %v, want errExecTimedOut", err)
	}

	data, readErr := os.ReadFile(pidPath)
	if readErr != nil {
		t.Fatalf("read child pid: %v", readErr)
	}
	pid, convErr := strconv.Atoi(strings.TrimSpace(string(data)))
	if convErr != nil {
		t.Fatalf("parse child pid: %v", convErr)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !processExists(pid) {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("child process %d still exists after timeout", pid)
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func processExists(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}
