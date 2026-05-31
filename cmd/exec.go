package main

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"os/exec"
	"sync"
)

// runExec executes a shell command in the given directory and returns its combined output.
// Each output line is passed to onLine (may be nil).
func runExec(ctx context.Context, command, dir string, onLine func(string)) (string, error) {
	if dir == "" {
		dir = "."
	}
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Dir = dir
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
	return stripAnsi(buf.String()), waitErr
}
