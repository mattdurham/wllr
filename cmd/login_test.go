package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestRunLoginCommand_UnsupportedProvider(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runLoginCommand(context.Background(), []string{"--provider", "gemini"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), `OAuth login is not supported for provider "gemini"`) {
		t.Fatalf("stderr = %q, want unsupported provider error", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
}
