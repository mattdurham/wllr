# tools — Design Notes

## 1. Extracted from harness/tools.go

*Added: 2026-05-29*

**Decision:** Move sdkToolAdapter, BuildFantasyTools, parseInputSchema from harness/tools.go to a dedicated tools/ package.

**Rationale:** The tool adapter logic had no business being in the TUI layer. It depends on extension.Host (for ExecuteTool) and fantasy (for AgentTool) — neither of which is a UI concern. Moving it to tools/ eliminates the harness→fantasy coupling for tool building and makes the adapter independently testable.

**Consequence:** harness/tools.go becomes a thin wrapper calling tools.BuildFantasyTools. The adapter is now importable by the session package without going through harness.
