# tools — Design Notes

## 1. Extracted from harness/tools.go

*Added: 2026-05-29*

**Decision:** Move sdkToolAdapter, BuildFantasyTools, parseInputSchema from harness/tools.go to a dedicated tools/ package.

**Rationale:** The tool adapter logic had no business being in the TUI layer. It depends on extension.Host (for ExecuteTool) and fantasy (for AgentTool) — neither of which is a UI concern. Moving it to tools/ eliminates the harness→fantasy coupling for tool building and makes the adapter independently testable.

**Consequence:** harness/tools.go becomes a thin wrapper calling tools.BuildFantasyTools. The adapter is now importable by the session package without going through harness.

## 2026-10-08: Tool-call watchdog (tempo hang)

**Decision:** Bound every sdkToolAdapter.Run with a timeout (default 10m, config
`tool_call_timeout`), returning a `timed out and was abandoned` error to the
model on breach; the ExecuteTool goroutine is abandoned via a buffered channel.

**Rationale:** The tempo incident (Oct 2026) — an extension search exec wedged
for 16+ minutes and froze the whole agent: no further turns, no session events.
The adapter is the one choke point every extension tool call passes through, so
a watchdog there protects all extensions at once (vs. per-extension timeouts).
Forced interruption of a wedged WASM host call is not possible from the adapter,
so abandonment is deliberate: the turn continues, and agent hard-kill ends the
leaked goroutine with the process.

**Consequence:** The host concrete type is reached through the exported
ToolExecutor interface (extension.ToolResult exported as an alias); fakes in
tests satisfy the same two methods.
