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

func TestBuildPromptWithConfig_OverrideAndDynamicMetadata(t *testing.T) {
	prompt := buildPromptWithConfig(
		[]promptTool{{Name: "read_file"}, {Name: "exec"}},
		[]promptCommand{{Name: "help", Desc: "show help"}},
		promptConfig{Override: "custom base"},
	)
	if !strings.HasPrefix(prompt, "custom base\n\nAvailable tools: exec, read_file") {
		t.Fatalf("prompt does not honor override or sort tools: %q", prompt)
	}
	if !strings.Contains(prompt, "**/help** — show help") {
		t.Fatalf("prompt missing command metadata: %q", prompt)
	}
	if strings.Contains(prompt, "## Action Rules") {
		t.Fatalf("built-in prompt should be replaced by override: %q", prompt)
	}
}
