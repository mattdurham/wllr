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

Journal persistence is handled externally by `cmd/main.go` via the harness `OnUserMessage` and `OnMessageEnd` callbacks. `ConversationSession` does not hold a journal reference; it has no journal-related methods.

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

## 8. Session Journal (JSONL Persistence)

`journal.go` provides append-only JSONL session persistence. Each line is a valid JSON object.

### Schema

| Entry type | Required fields |
|------------|----------------|
| `session_start` | `type`, `id`, `ts` (RFC3339) |
| `message` | `type`, `role` (`user`/`assistant`), `content`, `ts` |
| `session_end` | `type`, `ts` |

### Journal API

| Symbol | Description |
|--------|-------------|
| `OpenJournal(path string) (*Journal, error)` | Opens or creates a JSONL file for append-only writing |
| `(*Journal).WriteEntry(v any) error` | Marshals v and appends as a JSON line; goroutine-safe |
| `(*Journal).Close() error` | Flushes buffered data and closes the file |
| `NewSessionID() string` | Returns `YYYYMMDD-HHMMSS-XXXX` format ID using crypto/rand |
| `LoadSession(path string) ([]sdk.Message, error)` | Reads a JSONL file and returns user/assistant messages |

### File location

`~/.wllr/sessions/<session-id>.jsonl` — created by `cmd/main.go` at startup.

### Invariants

6. `Journal.WriteEntry` is goroutine-safe via an internal mutex; concurrent callers produce N valid JSON lines.
7. `newSessionID()` always returns a string matching `\d{8}-\d{6}-[0-9a-f]{4}`.
8. `LoadSession` skips `session_start` and `session_end` lines; returns only `message` entries with role `user` or `assistant`.
9. `OpenJournal` creates the file with permission 0600 (owner read/write only).
10. Journal persistence is best-effort: errors are logged via `slog` but never propagate to the user.
