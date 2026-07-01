# session — Specifications

## Overview

The `session` package manages a single conversation session: wiring subsystems together, managing lifecycle (start, submit, cancel, close), and dispatching events. It is the single coordinator that knows about agents and extensions without being tied to any UI framework.

## 1. Session Interface

The `Session` interface defines the public contract:

| Method | Description |
|--------|-------------|
| `Start(ctx)` | Fires `session_start` events via the extension host. No-op if host is nil. |
| `Submit(ctx, content, display)` | Sends user input to the main agent's inbox. The `display` parameter is accepted by the interface but is silently discarded by the `wire.go` implementation — it is not forwarded to the pool. Returns error if pool is nil. |
| `Cancel()` | Cancels all active agent turns. No-op if pool is nil. |
| `ReloadExtensions(ctx, paths)` | Hot-reloads WASM extensions. No-op if host is nil. |
| `Close(ctx)` | Cancels all agents then closes the extension host. |

## 2. ConversationSession

`ConversationSession` implements `Session`. It holds:

- `host *extension.Host` — the WASM extension runtime
- `pool *agent.AgentPool` — the agent registry
- `mainID string` — ID of the primary agent (usually "main")
- `renderer harness.Renderer` — the UI adapter (may be nil)

Session recording (messages and tool calls) is handled by the bundled `history` WASM extension, which writes JSONL under `~/.wllr/sessions/` and provides browse/rollback UI. `ConversationSession` holds no persistence reference and has no journal-related methods. (The former core `session.Journal` was redundant with the `history` extension and has been removed.)

## 3. Wire Constructor

```go
func Wire(host *extension.Host, pool *agent.AgentPool, mainAgentID string, renderer harness.Renderer) Session
```

Wire creates a `ConversationSession`. Wire itself does NOT install any interface bridges on the host. Bridge installation is the caller's responsibility: `harness.Model.New()` installs `earlyUIBridge` and `earlyAgentBridge` stubs immediately, and `harness.Model.SetProgram()` replaces them with the full `harnessUIBridge`, `harnessAgentBridge`, and `harnessTeamBridge`. `CapabilityProvider` is installed by `cmd/main.go`.

## 4. Lifecycle Sequence

```
Wire(host, pool, mainID, renderer)  ← creates session; no side effects
    ↓
Start(ctx)                          ← fires session_start events
    ↓
Submit(ctx, content, display)*      ← zero or more turns
    ↓
Cancel()*                           ← optional; cancels active turn
    ↓
Close(ctx)                          ← releases all resources
```

## 5. Thread Safety

All Session methods are safe to call from any goroutine. The underlying pool and host are goroutine-safe.

## 6. Nil Safety

All methods check for nil host and nil pool and return without error (or appropriate error for Submit). Wire with nil host returns a valid no-op Session.

## 7. Invariants

1. Wire never panics regardless of nil arguments.
2. Start returns nil when no extensions are loaded (nothing to dispatch to).
3. Submit returns an error when pool is nil (cannot send message without pool).
4. Cancel is always a no-op when not streaming (does not block).
5. Close is idempotent: calling it multiple times does not error.

## 8. Session Persistence

Session persistence is **not** a responsibility of this package. The bundled `history` WASM extension records messages and tool calls to JSONL under `~/.wllr/sessions/` and owns the browse/rollback UI. The core `session.Journal` (formerly `journal.go`) was redundant with it — its `LoadSession` had no production callers — and has been removed.
