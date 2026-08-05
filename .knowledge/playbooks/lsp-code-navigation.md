---
type: Playbook
title: Use LSP for code navigation and diagnostics
description: Use wllr's LSP tools for precise code navigation, refactoring, and diagnostics before broad text searches.
tags: [lsp, navigation, diagnostics, refactoring, tools]
generated: { by: human:mattdurham, at: 2026-08-05T00:00:00Z }
status: stable
---

Use the LSP extension as the primary code-intelligence path when it is
installed, configured, and responsive. LSP results preserve symbol and package
relationships better than broad text searches.

## Tool order

1. Use `lsp_capabilities` if the available backend or supported operation is
   unclear.
2. Use `lsp_symbols` to map a package or file before reading large source trees.
3. Use `lsp_definition` to locate the implementation of a symbol.
4. Use `lsp_references` before changing an exported function, interface, type,
   event, host-call method, or shared wire field.
5. Use `lsp_refactor_preview` before renames or shared API refactors.
6. Use `lsp_diagnostics` for the touched files after edits.
7. Use `lsp_lint` for broader static checks when the change spans packages or
   when the repository's pre-commit checks require it.

## Fallback behavior

If an LSP tool is unavailable, times out, returns no useful result, or does not
cover the language construct, continue with `rg`, `git grep`, `go doc`, and
targeted `exec` commands. Do not treat an empty LSP result as proof that a
symbol or reference does not exist. Confirm with a second search strategy.

## Safe navigation rules

- Verify the repository root and current branch before resolving paths.
- Read the relevant `SPECS.md` and `NOTES.md` before interpreting a
  spec-driven package.
- Search both production and test code before changing a public contract.
- After a refactor, inspect the diff and run diagnostics before running the
  broader test suite.
