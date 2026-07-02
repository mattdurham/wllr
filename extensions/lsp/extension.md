---
name: lsp
version: 0.1.0
description: Agent-facing code intelligence tools
author: Assistant
command: ./lsp
tools:
  lsp_capabilities:
    input: {}
    output: tools, backends, note
  lsp_diagnostics:
    input:
      file: Source file to check
    output: kind, target, language, command, ok, output, error
  lsp_lint:
    input:
      path: File or directory to validate
      file: Alternate source file field
    output: kind, target, language, command, ok, output, error
  lsp_symbols:
    input:
      file: Source file to inspect
    output: kind, target, pattern, ok, matches, error
  lsp_definition:
    input:
      symbol: Symbol name to locate
      path: Directory or file to search
    output: kind, target, pattern, ok, matches, error
  lsp_references:
    input:
      symbol: Symbol name to search for
      path: Directory or file to search
    output: kind, target, pattern, ok, matches, error
  lsp_refactor_preview:
    input:
      symbol: Current symbol name
      new_name: Proposed replacement name
      path: Directory or file to search
    output: kind, path, symbol, new_name, pattern, matches, ok, note, error
---

# LSP Extension

Use this extension for diagnostics, linting, code navigation, finding references,
and refactor previews. The extension returns structured JSON and leaves file
edits to normal editing tools.
