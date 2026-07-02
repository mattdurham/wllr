//go:build !wasip1

package main

import (
	"os"
	"os/exec"
)

func runCommand(command, dir string) (string, error) {
	cmd := exec.Command("sh", "-c", command)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
