//go:build unix

package main

// exec_unix.go — process-group helpers for runExec on Unix (darwin/linux).
// Children run in their own process group so a cancelled exec can kill the
// whole tree (sh + rg + awk grandchildren), not just the direct sh child.

import (
	"os/exec"
	"syscall"
)

// applyProcessGroup puts cmd in a fresh process group (pgid == cmd PID).
func applyProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killProcessTree kills the process group led by cmd's PID with SIGKILL.
// Falls back to killing only the direct child when setpgid was unavailable.
func killProcessTree(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	// Negative PID targets the whole process group.
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
		_ = cmd.Process.Kill()
	}
}
