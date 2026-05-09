package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMessageFraming(t *testing.T) {
	msg := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
	}

	data, _ := json.Marshal(msg)
	framed := frameMessage(data)

	if !strings.HasPrefix(framed, "Content-Length: ") {
		t.Error("Message should start with Content-Length header")
	}

	if !strings.Contains(framed, "\r\n\r\n") {
		t.Error("Message should have double CRLF separator")
	}

	if !strings.HasSuffix(framed, string(data)) {
		t.Error("Message should end with JSON content")
	}
}

func TestDetectLanguage(t *testing.T) {
	tests := []struct {
		filename string
		expected string
	}{
		{"test.go", "go"},
		{"test.py", "python"},
		{"test.js", "javascript"},
		{"test.ts", "typescript"},
		{"test.rs", "rust"},
		{"test.c", "c"},
		{"test.cpp", "cpp"},
		{"test.java", "java"},
		{"test.rb", "ruby"},
		{"test.unknown", ""},
	}

	for _, tt := range tests {
		result := detectLanguage(tt.filename)
		if result != tt.expected {
			t.Errorf("detectLanguage(%s) = %s, want %s", tt.filename, result, tt.expected)
		}
	}
}

func TestGetLSPCommand(t *testing.T) {
	tests := []struct {
		language string
		expected string
	}{
		{"go", "gopls"},
		{"python", "pylsp"},
		{"rust", "rust-analyzer"},
		{"typescript", "typescript-language-server"},
		{"unknown", ""},
	}

	for _, tt := range tests {
		result := getLSPCommand(tt.language)
		if result == "" && tt.expected != "" {
			t.Errorf("getLSPCommand(%s) should return %s, got empty", tt.language, tt.expected)
		}
		if result != "" && tt.expected != "" && !strings.Contains(result, tt.expected) {
			t.Errorf("getLSPCommand(%s) should contain %s, got %s", tt.language, tt.expected, result)
		}
	}
}

func TestParseArguments(t *testing.T) {
	// Test start with auto-detection
	args := map[string]interface{}{
		"file": "test.go",
	}
	action, name, cmd, _ := parseArguments(args)
	if action != "start" {
		t.Errorf("Expected action 'start' for file argument, got %s", action)
	}
	if cmd == "" {
		t.Error("Expected cmd to be set for .go file")
	}

	// Test explicit start
	args = map[string]interface{}{
		"action": "start",
		"name":   "myserver",
		"cmd":    "gopls",
	}
	action, name, cmd, _ = parseArguments(args)
	if action != "start" {
		t.Errorf("Expected action 'start', got %s", action)
	}
	if name != "myserver" {
		t.Errorf("Expected name 'myserver', got %s", name)
	}
	if cmd != "gopls" {
		t.Errorf("Expected cmd 'gopls', got %s", cmd)
	}

	// Test call
	args = map[string]interface{}{
		"action": "call",
		"server": "myserver",
		"method": "textDocument/completion",
	}
	action, serverName, _, method := parseArguments(args)
	if action != "call" {
		t.Errorf("Expected action 'call', got %s", action)
	}
	if serverName != "myserver" {
		t.Errorf("Expected server 'myserver', got %s", serverName)
	}
	if method != "textDocument/completion" {
		t.Errorf("Expected method 'textDocument/completion', got %s", method)
	}
}

func TestRequestIDGeneration(t *testing.T) {
	id1 := nextRequestID()
	id2 := nextRequestID()

	if id2 <= id1 {
		t.Error("Request IDs should be incrementing")
	}
}

func TestIsCommandAvailable(t *testing.T) {
	// Test with a command that should exist
	if !isCommandAvailable("ls") {
		t.Error("ls command should be available")
	}

	// Test with a command that shouldn't exist
	if isCommandAvailable("this-command-definitely-does-not-exist-12345") {
		t.Error("Non-existent command should return false")
	}
}
