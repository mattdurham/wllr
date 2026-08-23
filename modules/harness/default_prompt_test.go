package harness

import (
	"strings"
	"testing"

	"github.com/mattdurham/wllr/modules/sdk"
)

func TestBuildDefaultActionPrompt_IncludesLSPGuidance(t *testing.T) {
	prompt := buildDefaultActionPrompt([]sdk.Tool{
		{Name: "read_file"},
		{Name: "lsp_diagnostics"},
		{Name: "lsp_lint"},
		{Name: "lsp_symbols"},
		{Name: "lsp_definition"},
		{Name: "lsp_references"},
		{Name: "lsp_refactor_preview"},
		{Name: "lsp_capabilities"},
	}, nil)

	for _, want := range []string{
		"### Code Intelligence",
		"LSP tools are the primary tools for diagnostics, linting, code navigation",
		"At the start of repo/code work, call `lsp_capabilities`",
		"Before broad `grep`, `rg`, `find`, or large `read_file` sweeps",
		"use `lsp_refactor_preview`",
		"use `lsp_diagnostics` or `lsp_lint` before raw `go test`",
		"output contracts are available",
		"Use `exec`/manual search as a fallback",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestBuildDefaultActionPrompt_OmitsLSPGuidanceWithoutDiagnosticsTool(t *testing.T) {
	prompt := buildDefaultActionPrompt([]sdk.Tool{{Name: "read_file"}}, nil)

	if strings.Contains(prompt, "### Code Intelligence") {
		t.Fatalf("prompt should not include LSP guidance without LSP tools:\n%s", prompt)
	}
}

func TestBuildDefaultActionPrompt_IncludesProjectScope(t *testing.T) {
	prompt := buildDefaultActionPrompt(nil, nil)

	for _, want := range []string{
		"### Project Scope",
		"current working directory where wllr was launched as the project root",
		"scope file reads, searches, edits, tests, and shell commands",
		"Prefer relative paths",
		"Do not inspect parent directories, home directories, sibling repositories",
		"Use the `edit_file` tool for source-code edits",
		"Do not use `sed`, `perl`, Python, or shell redirection to modify files",
		"`apply_patch` is a Codex-side editing capability",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
}
