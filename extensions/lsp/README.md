# LSP Extension

This extension exposes agent-facing code intelligence tools. It does not ask the
agent to manage language-server processes directly; the useful workflows are
diagnostics, linting, code navigation, finding references, and refactor previews.
Agents should use these tools as the primary path for coding work: call
`lsp_capabilities` near the start of repo/code work unless the session already
knows available backends and output contracts, use navigation/reference tools
before broad shell search or large file sweeps, and use diagnostics/linting after
source edits before generic shell validation when a backend is available.

All tool results are JSON strings. Errors that mean "bad tool input" return an
error-shaped JSON result and mark the tool call as failed. Missing optional
backends return structured, non-fatal JSON so the agent can continue with normal
file reads, searches, and tests. The central contract reference is
[`docs/tool-contracts.md`](../../docs/tool-contracts.md#lsp-extension).

## Tools

### `lsp_capabilities`

Input:

```json
{}
```

Output:

```json
{
  "tools": ["lsp_diagnostics: run file-scoped diagnostics"],
  "backends": [
    {
      "language": "go",
      "extensions": [".go"],
      "diagnostic_command": "go vet",
      "lsp_server": "gopls"
    }
  ],
  "note": "These tools provide agent-facing code intelligence..."
}
```

### `lsp_diagnostics`

Runs file-scoped diagnostics when a backend is configured.

Input:

```json
{"file": "modules/harness/default_prompt.go"}
```

Output:

```json
{
  "kind": "diagnostics",
  "target": "modules/harness/default_prompt.go",
  "language": "go",
  "command": "go vet 'modules/harness/default_prompt.go'",
  "ok": true,
  "output": "no issues found"
}
```

### `lsp_lint`

Runs the broadest available validation for a file or project path. For Go
projects this uses `go test ./...` from the relevant directory.

Input:

```json
{"path": "."}
```

Output:

```json
{
  "kind": "lint",
  "target": ".",
  "language": "",
  "command": "go test ./...",
  "ok": true,
  "output": "ok ..."
}
```

### `lsp_symbols`

Lists likely symbol definitions in one file.

Input:

```json
{"file": "extensions/lsp/logic.go"}
```

Output:

```json
{
  "kind": "symbols",
  "target": "extensions/lsp/logic.go",
  "pattern": "\\b(func|type|var|const)\\s+[A-Za-z_][A-Za-z0-9_]*\\b",
  "ok": true,
  "matches": ["12:func capabilities() string {"]
}
```

### `lsp_definition`

Finds likely definition sites for a symbol.

Input:

```json
{"symbol": "runDiagnostics", "path": "extensions/lsp"}
```

Output:

```json
{
  "kind": "definition",
  "target": "extensions/lsp",
  "pattern": "\\brunDiagnostics\\b",
  "ok": true,
  "matches": ["logic.go:61:func runDiagnostics(input map[string]any) (string, bool) {"]
}
```

### `lsp_references`

Finds likely references for a symbol.

Input:

```json
{"symbol": "runDiagnostics", "path": "extensions/lsp"}
```

Output:

```json
{
  "kind": "references",
  "target": "extensions/lsp",
  "pattern": "\\brunDiagnostics\\b",
  "ok": true,
  "matches": ["main.go:51:return runDiagnostics(m)"]
}
```

### `lsp_refactor_preview`

Previews all likely references before a rename or refactor. This tool does not
edit files; the agent must review the matches and then use normal file-editing
tools.

Input:

```json
{"symbol": "runDiagnostics", "new_name": "checkFileDiagnostics", "path": "extensions/lsp"}
```

Output:

```json
{
  "kind": "refactor_preview",
  "path": "extensions/lsp",
  "symbol": "runDiagnostics",
  "new_name": "checkFileDiagnostics",
  "pattern": "\\brunDiagnostics\\b",
  "matches": ["logic.go:61:func runDiagnostics(input map[string]any) (string, bool) {"],
  "ok": true,
  "note": "Preview only. Review matches and apply edits with normal file-editing tools."
}
```

## Backends

Diagnostics are configured for:

| Language | Extensions | Diagnostic command |
| --- | --- | --- |
| Go | `.go` | `go vet <file>` |
| Python | `.py` | `python3 -m py_compile <file>` |
| Rust | `.rs` | `rustc --edition=2021 --error-format=json <file>` |

The capability output also lists known language-server commands for discovery.
Those commands are metadata today; the primary agent tools above do not expose
server lifecycle management.
