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
		"Prefer LSP tools for diagnostics, linting, code navigation",
		"Use `lsp_diagnostics` or `lsp_lint` after editing supported source files",
		"Use `lsp_symbols`, `lsp_definition`, and `lsp_references`",
		"Use `lsp_refactor_preview` before renames or refactors",
		"output contracts are available",
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
