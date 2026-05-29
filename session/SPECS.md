# session — Specifications

## Overview

The `session` package manages a single conversation session: wiring subsystems together, managing lifecycle (start, submit, cancel, close), and dispatching events. It is the single coordinator that knows about agents and extensions without being tied to any UI framework.

## 1. Session Interface

The `Session` interface defines the public contract:

| Method | Description |
|--------|-------------|
| `Start(ctx)` | Fires `session_start` events via the extension host. No-op if host is nil. |
| `Submit(ctx, content, display)` | Sends user input to the main agent's inbox. Returns error if pool is nil. |
| `Cancel()` | Cancels all active agent turns. No-op if pool is nil. |
| `ReloadExtensions(ctx, paths)` | Hot-reloads WASM extensions. No-op if host is nil. |
| `Close(ctx)` | Cancels all agents then closes the extension host. |

## 2. ConversationSession

`ConversationSession` implements `Session`. It holds:
- `host *extension.Host` — the WASM extension runtime
- `pool *agent.AgentPool` — the agent registry
- `mainID string` — ID of the primary agent (usually "main")
- `renderer harness.Renderer` — the UI adapter (may be nil)

## 3. Wire Constructor

```go
func Wire(host *extension.Host, pool *agent.AgentPool, mainAgentID string, renderer harness.Renderer) Session
```

Wire creates a `ConversationSession`. The caller is responsible for installing interface bridges on the host (via `host.SetAgentBridge`, `host.SetTeamBridge`, `host.SetUIBridge`, `host.SetCapabilities`, `host.SetMCPBridge`) before or after calling Wire.

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
