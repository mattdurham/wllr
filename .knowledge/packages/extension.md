---
type: Go Package
title: extension
description: wazero-based WASM extension host — loads modules, dispatches events, and mediates all capabilities through five typed bridges.
resource: ./modules/extension
tags: [wasm, wazero, host, bridges, sandbox, permissions]
timestamp: 2026-07-01T13:10:47Z
---

The `extension` package is the sandbox core of wllr. It loads `.wasm` modules
with [wazero](https://github.com/tetratelabs/wazero) (pure-Go, no CGo), gives
each its own linear memory and key-value store, and dispatches lifecycle events
to them. Extensions never touch the OS directly — every capability (exec, file
I/O, network, UI, MCP, agent control) is mediated through a **typed bridge** the
host owns, gated by a per-extension permission set. This is what makes wllr a
*sandboxed* agent rather than a trusted-plugin one.

# Specification

- [Contracts and invariants](../../modules/extension/SPECS.md)
- [Design decisions](../../modules/extension/NOTES.md)
- [Test plan](../../modules/extension/TESTS.md)
- [WASM Extension Author API](../../docs/extensions.md) — the authoritative host↔extension ABI reference

# Key Interfaces

The five bridges (`modules/extension/interfaces.go`) an embedder implements:

- `AgentBridge` — spawn, close, message agents
- `TeamBridge` — create and manage agent teams
- `UIBridge` — notify, modal, picker, status bar, scene-graph areas
- `CapabilityProvider` — exec, file I/O, HTTP, env (the OS-facing capabilities)
- `MCPBridge` — MCP server subprocess management

`Host.DispatchEventChain` runs the transform-capable interceptor chain across
extensions (see the interceptor pattern).

# Cross-cutting Decisions

- [WASM isolation + permission model = the sandbox](../decisions/sandboxed-by-design.md)
- [Capabilities over policy](../decisions/capabilities-over-policy.md)
- [sdk is the sole shared dependency](../decisions/sdk-sole-shared-dependency.md)

# Dependencies

- `sdk` (wire types, ABI)

# Usage Patterns

- [WASM extension authoring](../patterns/wasm-extension-authoring.md)
- [Scene-graph UI](../patterns/scene-graph-ui.md)
- [Interceptor transform chain](../patterns/interceptor-transform-chain.md)
