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

## 4. JSONL Journal: best-effort, no UI impact

*Added: 2026-05-30*

**Decision:** Journal write errors are silently dropped (logged via `slog`) and never propagated to the user or returned from `ConversationSession` methods.

**Rationale:** Session persistence is a quality-of-life feature. A disk-full or permission error on the journal file must never interrupt a conversation. The user's primary goal is talking to the LLM, not persisting logs.

**Consequence:** Silent data loss is possible if the journal write fails. This is acceptable given that the feature is opt-in and the failure mode is not data corruption — sessions simply stop being recorded.

## 5. OnMessageEnd / OnUserMessage hooks on harness.Model

*Added: 2026-05-30*

**Decision:** Session persistence is wired via two exported function fields on `harness.Model`: `OnMessageEnd func(role, content string)` and `OnUserMessage func(content string)`. These are set by `cmd/main.go` after model creation.

**Rationale:** The journal needs to intercept both user input and completed assistant turns. Wiring via exported callbacks on the Model avoids adding a `session` import to the `harness` package (which would create a dependency on the journal) and keeps the harness package focused on rendering.

**Consequence:** `cmd/main.go` is responsible for setting these hooks. Callers that do not set them get nil callbacks (no-op path in the model, already guarded by nil checks).

## 6. Core session.Journal removed — history extension is the source of truth

*Added: 2026-06-30*

**Decision:** Remove `journal.go` (`Journal`, `OpenJournal`, `WriteEntry`, `Close`, `NewSessionID`, `LoadSession`, `journalEntry`, `splitLines`) and `journal_test.go` from this package, plus the `openSessionJournal()` wiring and the `OnUserMessage`/`OnMessageEnd` journal callbacks in `cmd/main.go`. `Wire`/`ConversationSession`/`Session` and the harness `OnUserMessage`/`OnMessageEnd` hooks themselves are KEPT.

**Rationale:** The bundled `history` WASM extension strictly supersedes the core journal: it records the same messages plus tool calls, writes JSONL under `~/.wllr/sessions/`, and adds browse/rollback UI. `session.LoadSession` had zero production callers (test-only), so the read side was dead. Maintaining two parallel session-recording paths was redundant and risked divergence. This aligns with the project direction that history is the primary session storage.

**Consequence:** `cmd/main.go` no longer opens a core journal or sets the two message callbacks for persistence (the harness hooks remain available for other consumers/extensions). NOTES §4 (best-effort journal writes) and §5 (hook wiring) describe the removed core-journal wiring; they are retained for history but no longer reflect active core behavior — recording now lives in the `history` extension. SPECS §8 rewritten accordingly; the Journal API table and its invariants (6–10) are removed.
