package main

import (
	"strings"
	"testing"
)

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

func TestCapabilitiesDescribesAgenticToolsAndBackends(t *testing.T) {
	out := capabilities()
	for _, want := range []string{
		"lsp_diagnostics",
		"lsp_lint",
		"lsp_symbols",
		"lsp_definition",
		"lsp_references",
		"lsp_refactor_preview",
		"gopls",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("capabilities missing %q:\n%s", want, out)
		}
	}
}

func TestRunDiagnosticsRequiresFile(t *testing.T) {
	out, handled := runDiagnostics(map[string]any{})
	if !handled {
		t.Fatal("missing file should be reported as handled error")
	}
	if !strings.Contains(out, "file parameter required") {
		t.Fatalf("unexpected output: %s", out)
	}
}

func TestRunDiagnosticsUnsupportedLanguageReturnsStructuredNote(t *testing.T) {
	out, handled := runDiagnostics(map[string]any{"file": "README.unknown"})
	if handled {
		t.Fatal("unsupported files should return a structured non-fatal result")
	}
	if !strings.Contains(out, "unsupported or unknown file type") {
		t.Fatalf("unexpected output: %s", out)
	}
}

func TestRunDiagnosticsNoConfiguredDiagnosticReturnsStructuredNote(t *testing.T) {
	out, handled := runDiagnostics(map[string]any{"file": "index.ts"})
	if handled {
		t.Fatal("missing backend should return a structured non-fatal result")
	}
	if !strings.Contains(out, "no diagnostic command configured") {
		t.Fatalf("unexpected output: %s", out)
	}
}

func TestDefinitionPatternUsesSymbolWhenProvided(t *testing.T) {
	pattern := definitionPattern("go", "runDiagnostics")
	if !strings.Contains(pattern, "runDiagnostics") {
		t.Fatalf("pattern should include symbol: %s", pattern)
	}
}
