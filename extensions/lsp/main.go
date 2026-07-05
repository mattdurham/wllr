//go:build wasip1

// Package main exposes agent-oriented code intelligence tools.
package main

import "encoding/json"

func init() {
	RegisterToolWithOutput(
		"lsp_capabilities",
		"Describe the code-intelligence workflows and backends this extension can use",
		json.RawMessage(`{"type":"object","properties":{}}`),
		json.RawMessage(
			`{"type":"object","properties":{"tools":{"type":"array","items":{"type":"string"}},"backends":{"type":"array","items":{"type":"object"}},"note":{"type":"string"}}}`,
		),
	)
	RegisterToolWithOutput(
		"lsp_diagnostics",
		"Run file-scoped diagnostics for a source file",
		json.RawMessage(
			`{"type":"object","properties":{"file":{"type":"string","description":"Source file to check"}},"required":["file"]}`,
		),
		json.RawMessage(
			`{"type":"object","properties":{"kind":{"type":"string"},"target":{"type":"string"},"language":{"type":"string"},"command":{"type":"string"},"ok":{"type":"boolean"},"output":{"type":"string"},"error":{"type":"string"}}}`,
		),
	)
	RegisterToolWithOutput(
		"lsp_lint",
		"Run the broadest available lint or validation command for a file or project path",
		json.RawMessage(
			`{"type":"object","properties":{"path":{"type":"string","description":"File or directory to validate"},"file":{"type":"string","description":"Source file to validate"}}}`,
		),
		json.RawMessage(
			`{"type":"object","properties":{"kind":{"type":"string"},"target":{"type":"string"},"language":{"type":"string"},"command":{"type":"string"},"ok":{"type":"boolean"},"output":{"type":"string"},"error":{"type":"string"}}}`,
		),
	)
	RegisterToolWithOutput(
		"lsp_symbols",
		"List likely symbols in a source file",
		json.RawMessage(
			`{"type":"object","properties":{"file":{"type":"string","description":"Source file to inspect"}},"required":["file"]}`,
		),
		json.RawMessage(
			`{"type":"object","properties":{"kind":{"type":"string"},"target":{"type":"string"},"pattern":{"type":"string"},"ok":{"type":"boolean"},"matches":{"type":"array","items":{"type":"string"}},"error":{"type":"string"}}}`,
		),
	)
	RegisterToolWithOutput(
		"lsp_definition",
		"Find likely definition sites for a symbol",
		json.RawMessage(
			`{"type":"object","properties":{"symbol":{"type":"string","description":"Symbol name to locate"},"path":{"type":"string","description":"Directory or file to search"}},"required":["symbol"]}`,
		),
		json.RawMessage(
			`{"type":"object","properties":{"kind":{"type":"string"},"target":{"type":"string"},"pattern":{"type":"string"},"ok":{"type":"boolean"},"matches":{"type":"array","items":{"type":"string"}},"error":{"type":"string"}}}`,
		),
	)
	RegisterToolWithOutput(
		"lsp_references",
		"Find likely references for a symbol",
		json.RawMessage(
			`{"type":"object","properties":{"symbol":{"type":"string","description":"Symbol name to search for"},"path":{"type":"string","description":"Directory or file to search"}},"required":["symbol"]}`,
		),
		json.RawMessage(
			`{"type":"object","properties":{"kind":{"type":"string"},"target":{"type":"string"},"pattern":{"type":"string"},"ok":{"type":"boolean"},"matches":{"type":"array","items":{"type":"string"}},"error":{"type":"string"}}}`,
		),
	)
	RegisterToolWithOutput(
		"lsp_refactor_preview",
		"Preview reference locations before a rename or refactor",
		json.RawMessage(
			`{"type":"object","properties":{"symbol":{"type":"string","description":"Current symbol name"},"new_name":{"type":"string","description":"Proposed replacement name"},"path":{"type":"string","description":"Directory or file to search"}},"required":["symbol","new_name"]}`,
		),
		json.RawMessage(
			`{"type":"object","properties":{"kind":{"type":"string"},"path":{"type":"string"},"symbol":{"type":"string"},"new_name":{"type":"string"},"pattern":{"type":"string"},"matches":{"type":"array","items":{"type":"string"}},"ok":{"type":"boolean"},"note":{"type":"string"},"error":{"type":"string"}}}`,
		),
	)

	OnToolCall(func(_ string, toolName string, input json.RawMessage) (string, bool) {
		var m map[string]any
		_ = json.Unmarshal(input, &m)

		switch toolName {
		case "lsp_capabilities":
			return capabilities(), false
		case "lsp_diagnostics":
			return runDiagnostics(m)
		case "lsp_lint":
			return runLint(m)
		case "lsp_symbols":
			return listSymbols(m)
		case "lsp_definition":
			return findDefinitions(m)
		case "lsp_references":
			return findReferences(m)
		case "lsp_refactor_preview":
			return refactorPreview(m)
		default:
			return "", false
		}
	})
}

func main() {}
