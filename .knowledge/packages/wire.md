---
type: Go Package
title: wire
description: The HTTP/SSE protocol contract between a wllr engine and any client — the frame envelope and event-kind vocabulary.
resource: ./modules/wire
tags: [wire, protocol, http, sse, leaf]
timestamp: 2026-10-08T00:00:00Z
---

The `wire` package defines the frames that flow over `wllr serve`'s HTTP/SSE
surface: the `Event` envelope, the `EventKind` vocabulary, `TextInputParams`, and
`ProtocolVersion`. It contains no behavior — only the contract that
`modules/server` emits and `modules/client` decodes.

It is a **leaf** (depends only on `sdk`), so a client can speak the protocol
without linking the engine.

# Specification

- [Contracts and invariants](../../modules/wire/SPECS.md)
- [Design decisions](../../modules/wire/NOTES.md)
- [Test plan](../../modules/wire/TESTS.md)
- [Size/runtime notes](../../modules/wire/BENCHMARKS.md)

# Key Types

- `Event` — one flat frame: `Seq`, `Kind`, `AgentID`, and kind-specific fields
- `EventKind` / `Kind*` — the frame vocabulary (ready, token, turn_done, ui_patch, …)
- `ProtocolVersion` — the wire version, surfaced on `/api/health` and the `ready` frame
- `TextInputParams` — the `text_input` frame payload

# Cross-cutting Decisions

- [The wire protocol is a leaf package](../decisions/wire-protocol-extraction.md)

# Dependencies

`sdk` (shared types), standard library. Nothing else.
