---
type: Go Package
title: sdk
description: Shared wire types, lifecycle events, and ABI constants for the host↔extension boundary.
resource: ./modules/sdk
tags: [sdk, abi, wire-types, events, leaf]
timestamp: 2026-07-01T13:10:47Z
---

The `sdk` package defines every type that crosses the host↔extension boundary:
messages, tool schemas, lifecycle event payloads, UI scene nodes, and ABI
constants. It is the **only** package imported by both `harness` and
`extension`, so it is a dependency leaf with no internal wllr imports — this is
what keeps the two sides decoupled and cycle-free.

# Specification

- [Contracts and invariants](../../modules/sdk/SPECS.md)
- [Design decisions](../../modules/sdk/NOTES.md)
- [Test plan](../../modules/sdk/TESTS.md)

# Key Types

- `Event` / `EventType` — the 17 lifecycle events (see [WASM Extension API](../patterns/wasm-extension-authoring.md))
- `EventResponse` — transform-capable interceptor result (`Payload`, block/reason)
- `Message` / `Role` — conversation history entries
- `Tool` — tool name/description/JSON schema
- `Permission` — `exec`, `file_open`, `file_read`, `file_write`, `network_read`, `network_write`, `ui`
- `UINode` / `UIArea` / `UIPatchOp` — the scene-graph UI model (see [Scene-Graph UI](../patterns/scene-graph-ui.md))
- `ABIVersion`, `ErrOK`/`ErrGeneral`/`ErrCancel` — ABI constants

# Cross-cutting Decisions

- [sdk is the sole shared dependency](../decisions/sdk-sole-shared-dependency.md)

# Dependencies

None (leaf package).

# Usage Patterns

- [WASM extension authoring](../patterns/wasm-extension-authoring.md)
- [Interceptor transform chain](../patterns/interceptor-transform-chain.md)
