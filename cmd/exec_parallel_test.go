package main

// exec_parallel_test.go — parallel-invocation regression for the Oct 2026
// tempo incident: two concurrent extension execs, one of which wedges a
// grandchild behind a full pipe. Both must complete (or be cancelled by the
// watchdog) without one blocking the other or hanging the process.

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestRunExec_ParallelBothComplete — two concurrent runExec calls (the shape
// the harness produces for parallel tool calls): a fast one and a slow one.
// The fast one must complete while the slow one is still running; the slow one
// completes once its work finishes. This pins "parallel execs don't serialize
// on a shared lock inside runExec".
func TestRunExec_ParallelBothComplete(t *testing.T) {
	fastDone := make(chan string, 1)
	slowDone := make(chan string, 1)

	go func() {
		out, err := runExec(context.Background(), "echo fast-result", "", nil)
		if err == nil {
			fastDone <- out
		}
	}()
	go func() {
		out, err := runExec(context.Background(), "sleep 1; echo slow-result", "", nil)
		if err == nil {
			slowDone <- out
		}
	}()

	select {
	case out := <-fastDone:
		if !strings.Contains(out, "fast-result") {
			t.Fatalf("fast exec output: %q", out)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("fast exec did not complete while slow exec ran — serialization regression")
	}

	select {
	case out := <-slowDone:
		if !strings.Contains(out, "slow-result") {
			t.Fatalf("slow exec output: %q", out)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("slow exec did not complete")
	}
}

// TestRunExec_CancelOneLeavesOtherRunning — cancelling one exec's context must
// not disturb an independent concurrent exec.
func TestRunExec_CancelOneLeavesOtherRunning(t *testing.T) {
	ctxA, cancelA := context.WithCancel(context.Background())
	otherDone := make(chan string, 1)
	cancelledDone := make(chan error, 1)

	go func() {
		_, err := runExec(ctxA, "sleep 300 | cat", "", nil)
		cancelledDone <- err
	}()
	go func() {
		out, err := runExec(context.Background(), "sleep 1; echo survivor", "", nil)
		if err == nil {
			otherDone <- out
		}
	}()

	time.Sleep(200 * time.Millisecond)
	cancelA()

	select {
	case err := <-cancelledDone:
		if err == nil {
			t.Fatal("expected cancellation error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled exec did not return — group kill failed")
	}

	select {
	case out := <-otherDone:
		if !strings.Contains(out, "survivor") {
			t.Fatalf("other exec output: %q", out)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("independent exec was disturbed by the cancelled one")
	}
}
