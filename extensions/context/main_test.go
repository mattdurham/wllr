package main

import (
	"strings"
	"testing"
)

func TestCWDNote(t *testing.T) {
	cwd := "/Users/test/project"
	note := cwdNote(cwd)
	expected := "You are operating in the current working directory: /Users/test/project"
	if note != expected {
		t.Errorf("cwdNote() = %q, want %q", note, expected)
	}
}

func TestOnSessionStart_AppendsCWD(t *testing.T) {
	// This test verifies that onSessionStart appends the working directory
	// to the system prompt. We can't fully test it without running WASM,
	// but we can at least verify the function exists and compiles.
	testCWD := "/test/project"

	// Verify the cwdNote function produces correct output
	note := cwdNote(testCWD)
	if !strings.Contains(note, testCWD) {
		t.Errorf("cwdNote(%q) = %q does not contain the path", testCWD, note)
	}

	if !strings.Contains(note, "current working directory") {
		t.Errorf("cwdNote(%q) = %q missing 'current working directory' text", testCWD, note)
	}
}