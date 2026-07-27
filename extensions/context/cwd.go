package main

// This file holds host-testable CWD injection logic.

func cwdNote(cwd string) string {
	return "You are operating in the current working directory: " + cwd
}
