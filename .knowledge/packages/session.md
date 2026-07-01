---
type: Go Package
title: session
description: Subsystem wiring and lifecycle — the single coordinator that connects agents, extensions, and renderer without a UI dependency.
resource: ./modules/session
tags: [session, lifecycle, wiring, decoupling]
timestamp: 2026-07-01T13:10:47Z
---

The `session` package is the coordinator that wires the host, agent pool, and
renderer together and manages lifecycle (start, submit, cancel, close) without
being tied to any UI framework. `Wire(host, pool, mainID, renderer)` returns a
`Session`; swap the TUI by implementing `Renderer` + `UIBridge` and calling
`Wire` with your own renderer.

Session recording is **not** owned here — the `history` built-in extension is
the sole session store (the former core `session.Journal` was removed).

# Specification

- [Contracts and invariants](../../modules/session/SPECS.md)
- [Design decisions](../../modules/session/NOTES.md)
- [Test plan](../../modules/session/TESTS.md)

# Key Interfaces

- `Session` — Start, Submit, Cancel, ReloadExtensions, Close
- `ConversationSession` — the concrete implementation
- `Wire(...)` — constructor / decoupling seam

# Cross-cutting Decisions

- [History is the sole session store](../decisions/history-is-sole-session-store.md)
- [TUI decoupled behind Renderer + UIBridge](../decisions/tui-decoupled-behind-renderer.md)

# Dependencies

- `agent`, `extension`, `harness` (Renderer), `sdk`
