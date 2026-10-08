package main

// exec.go — runExec executes a shell command for the extension capability
// provider and returns its combined output.
//
// The command runs in its own process group (Unix; Setpgid), so cancelling
// ctx kills the whole tree — sh plus any grandchildren (rg, awk, …). This
// matters because grandchildren inherit the output pipes: killing only sh
// would leave them holding the write ends, and the pipe readers below would
// block on Read forever, hanging runExec even though the context was
// cancelled (the tempo search hang, Oct 2026: a wedged rg survived its
// parent for hours because nothing killed the group).
//
// Kill escalation mirrors runShellCommand (the model's exec tool): SIGTERM to
// the group, a short grace period, then SIGKILL. After the group dies the
// pipes close and the readers reach EOF, so the wait below cannot hang.
// Termination maps to the shared errExecCancelled sentinel so callers treat
// watchdog kills and user cancels identically.

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

// execKillGrace is how long a SIGTERM'd process group has to exit before the
// monitor escalates to SIGKILL. Defined in provider.go (shared with
// runShellCommand's escalation path).

// runExec executes a shell command in the given directory and returns its
// combined output. Each output line is passed to onLine (may be nil).
func runExec(ctx context.Context, command, dir string, onLine func(string)) (string, error) {
	if dir == "" {
		dir = "."
	}
	// Not CommandContext: its default kill targets only the direct child and
	// cannot kill a process group. Cancellation is handled by the monitor below.
	cmd := exec.Command("sh", "-c", command)
	cmd.Dir = dir
	applyProcessGroup(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		_ = stdout.Close()
		return "", err
	}
	if err := cmd.Start(); err != nil {
		return "", err
	}
	killed := false
	var killMu sync.Mutex
	stopMonitor := monitorCancel(ctx, cmd, &killed, &killMu)

	mu := sync.Mutex{}
	buf := bytes.Buffer{}
	wg := sync.WaitGroup{}
	readPipe := func(r io.Reader) {
		defer wg.Done()
		sc := bufio.NewScanner(r)
		for sc.Scan() {
			ln := sc.Text()
			mu.Lock()
			buf.WriteString(ln)
			buf.WriteByte('\n')
			mu.Unlock()
			if onLine != nil {
				onLine(ln)
			}
		}
	}
	wg.Add(2)
	go readPipe(stdout)
	go readPipe(stderr)
	wg.Wait()
	waitErr := cmd.Wait()
	stopMonitor() // before reading `killed`: the monitor may write it until stopped
	killMu.Lock()
	killedNow := killed
	killMu.Unlock()
	if killedNow || ctx.Err() != nil {
		killProcessTree(cmd) // reap any stragglers still holding the pipes
		return stripAnsi(buf.String()), errExecCancelled
	}
	return stripAnsi(buf.String()), waitErr
}

// monitorCancel escalates SIGTERM→SIGKILL against cmd's process group when ctx
// is cancelled, and records that termination was forced in *killed so the
// caller can report cancellation even if cmd.Wait returns a non-context error
// first. The returned func stops the monitor (idempotent).
func monitorCancel(ctx context.Context, cmd *exec.Cmd, killed *bool, mu *sync.Mutex) (stop func()) {
	done := make(chan struct{})
	stopOnce := sync.Once{}
	go func() {
		select {
		case <-ctx.Done():
			mu.Lock()
			*killed = true
			mu.Unlock()
			terminateProcessGroup(cmd.Process.Pid, syscall.SIGTERM)
			// Grandchildren that ignore SIGTERM must not hold the pipes (and
			// this process) forever: escalate after the grace period.
			select {
			case <-done:
			case <-time.After(execKillGrace):
				mu.Lock()
				*killed = true
				mu.Unlock()
				killProcessTree(cmd)
			}
		case <-done:
		}
	}()
	return func() {
		stopOnce.Do(func() { close(done) })
	}
}
