//go:build !unix

package main

// exec_other.go — no-op process-group helpers for non-Unix platforms.
// Without setpgid a cancelled exec can only kill the direct child; orphaned
// grandchildren may hold the output pipes open until they exit on their own.

import "os/exec"

func applyProcessGroup(cmd *exec.Cmd) {}

func killProcessTree(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}
