# LSP Extension Summary

The extension provides practical code-intelligence tools for agents:

- `lsp_diagnostics` for file-scoped compiler or language checks
- `lsp_lint` for broader project validation
- `lsp_symbols` for file outlines
- `lsp_definition` for likely definition sites
- `lsp_references` for usage search
- `lsp_refactor_preview` for rename/refactor reconnaissance
- `lsp_capabilities` for supported tools, languages, and backend metadata

All tools return structured JSON strings. The docs in `README.md` show both input
and output contracts for each tool.

This is intentionally not a generic "start a language server and send arbitrary
LSP requests" interface. That workflow is not a good fit for agentic tool use:
agents need direct operations that answer navigation and validation questions.
