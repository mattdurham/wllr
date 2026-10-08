package main

// exec_test.go — regression tests for runExec's process-group kill.
//
// The Oct 2026 tempo incident: an extension search ran `rg | awk` where a
// wedged grandchild held the output pipes; cancelling the context killed
// only sh, so runExec hung for hours. These tests pin the kill-the-whole-tree
// behavior: after ctx cancellation runExec must return promptly and no
// grandchild may survive holding the pipes.

import (
	"context"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestRunExec_CancelKillsGrandchildren reproduces the tempo hang: sh spawns a
// long-lived child that holds stdout. When the context is cancelled runExec must
// unblock quickly (the group kill closes the pipe → EOF → wait returns).
func TestRunExec_CancelKillsGrandchildren(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := runExec(ctx, "sleep 300 | cat; echo unreachable", "", nil)
		done <- err
	}()
	time.Sleep(200 * time.Millisecond) // let sh + children start
	cancel()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected cancellation error from runExec")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("runExec did not return within 5s of context cancel — process-group kill failed")
	}
}

// TestRunExec_PipePressureCancel pins the pipe-deadlock scenario specifically:
// the child produces far more output than the pipe buffers can hold while the
// writer is slow, so at cancel time grandchildren are mid-write. The kill must
// still break through (a wedge here is the exact production failure).
func TestRunExec_PipePressureCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		// writer floods the pipe; reader trickles — both stay alive past cancel
		_, err := runExec(ctx, "yes 'some output line for the pipe' | while read -r l; do echo \"$l\"; sleep 0.05; done", "", nil)
		done <- err
	}()
	time.Sleep(500 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected cancellation error")
		}
	case <-time.After(8 * time.Second):
		t.Fatal("runExec hung under pipe pressure after cancel — deadlock regression")
	}
}

// TestRunExec_KillsWholeProcessTree verifies that descendants of sh are gone
// after cancellation (signal them and expect ESRCH — no such process).
func TestRunExec_KillsWholeProcessTree(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := runExec(ctx, `sleep 300 & child=$!; wait $child`, "", nil)
		done <- err
	}()
	time.Sleep(300 * time.Millisecond)
	cancel()
	<-done // must return; TestRunExec_CancelKillsGrandchildren asserts timing

	// Find any surviving `sleep 300` processes and signal them: a live one
	// would have exited ESRCH. PIDs can't be mapped to our exact child portably,
	// so instead assert no NEW sleep 300 survives after a grace period.
	time.Sleep(500 * time.Millisecond)
	out, err := exec.Command("pgrep", "-f", "sleep 300").Output()
	if err == nil && len(strings.TrimSpace(string(out))) > 0 {
		pids := strings.Fields(string(out))
		alive := 0
		for _, pid := range pids {
			if syscall.Kill(atoi(pid), 0) == nil {
				alive++
			}
		}
		if alive > 0 {
			t.Fatalf("%d `sleep 300` descendant(s) survived the group kill: %v", alive, pids)
		}
	}
}

func atoi(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return -1
		}
		n = n*10 + int(c-'0')
	}
	return n
}

// TestRunExec_NormalCompletionStillWorks guards the rewrite: a plain command
// must still return its output and nil error when nothing is cancelled.
func TestRunExec_NormalCompletionStillWorks(t *testing.T) {
	out, err := runExec(context.Background(), "echo hello; echo err >&2", "", nil)
	if err != nil {
		t.Fatalf("runExec: %v", err)
	}
	if !strings.Contains(out, "hello") || !strings.Contains(out, "err") {
		t.Fatalf("output missing expected lines: %q", out)
	}
}

// TestRunExec_ExecOutputDrainedUnderSlowConsumer — onLine streaming must not
// deadlock when the consumer is slower than the producer (the reader goroutine
// always drains regardless of onLine speed).
func TestRunExec_ExecOutputDrainedUnderSlowConsumer(t *testing.T) {
	var lines int
	_, err := runExec(context.Background(), "for i in 1 2 3 4 5; do echo line$i; done", "", func(string) {
		time.Sleep(10 * time.Millisecond)
		lines++
	})
	if err != nil {
		t.Fatalf("runExec: %v", err)
	}
	if lines != 5 {
		t.Fatalf("expected 5 streamed lines, got %d", lines)
	}
}
