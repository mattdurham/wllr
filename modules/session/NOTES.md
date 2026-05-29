# session — Design Notes

## 1. Session Extraction from harness

*Added: 2026-05-29*

**Decision:** Extract a `session` package that owns conversation lifecycle (start, submit, cancel, close), leaving `harness` as a pure TUI rendering layer.

**Rationale:** The `harness.Model.SetProgram` function previously contained ~260 lines of callback wiring and session logic. Extracting it creates a clear boundary: the session package knows about agents and extensions, the harness package knows about bubbletea. This enables future non-TUI front-ends to reuse the session layer without depending on bubbletea.

**Consequence:** The `session` package imports `harness.Renderer` (the interface defined in the harness package). This is intentional — the Renderer interface is defined at the boundary where the TUI implementation lives. If a circular import ever appears (e.g., if harness imports session for Wire), the Renderer interface should be moved to a neutral `tui/` package.

## 2. Wire Does Not Install Bridges

*Added: 2026-05-29*

**Decision:** `session.Wire` does not call `host.SetAgentBridge`, `host.SetUIBridge`, etc. The caller is responsible for bridge installation.

**Rationale:** Bridge installation requires access to the program renderer and agent pool, which the harness provides in `SetProgram`. Separating bridge installation from session creation keeps Wire simple and avoids requiring the session package to know about the bridge adapter structs defined in harness.

**Consequence:** Callers must install bridges before (or immediately after) calling Wire. The harness.SetProgram installs bridges before returning the Session.

## 3. Import Direction: session → harness

*Added: 2026-05-29*

**Decision:** The `session` package imports `harness.Renderer`. The `harness` package does not import `session` (yet).

**Rationale:** The `Renderer` interface defines what the session layer calls to update the UI. Placing it in `harness` is correct because the concrete implementation (`programRenderer`) lives in harness. The session package is the consumer of the interface, so it must import harness.

**Consequence:** If harness ever imports session (e.g., for `session.Wire`), the import would create a cycle. At that point, extract `Renderer` to a `tui/` or `render/` package.
