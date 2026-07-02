//go:build wasip1

package main

import "os"

func runCommand(command, dir string) (string, error) {
	return Exec(command, dir)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
