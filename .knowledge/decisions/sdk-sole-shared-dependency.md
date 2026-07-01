---
type: Decision
title: sdk is the only package shared by harness and extension
description: All host↔extension types live in sdk (a leaf); nothing else is imported by both sides, preventing import cycles.
tags: [sdk, extension, harness, architecture]
timestamp: 2026-07-01T13:10:47Z
---

**Decision:** Every type that crosses the host↔extension boundary lives in
`sdk`, which has no internal wllr imports. It is the only package imported by
both `harness` and `extension`.

**Rationale:** Keeps the UI and the WASM host decoupled and cycle-free, and gives
extension authors a single, stable import for the wire contract.

**Consequence:** New shared types (events, payloads, UI nodes, ABI constants) go
in `sdk`, not in `harness` or `extension`. `sdk` must not grow a dependency on
either side.

# Applies To

- [sdk package](../packages/sdk.md), [extension package](../packages/extension.md), [harness package](../packages/harness.md)

# Origin

Inferred from code structure; documented in docs/architecture.md.
