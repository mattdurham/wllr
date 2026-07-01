---
type: Decision
title: Sandboxed by design — WASM isolation + permissions + typed bridges
description: Extensions run as isolated WASM with no direct OS access; every capability is mediated by a typed host bridge and gated by permissions.
tags: [extension, sdk, sandbox, security, permissions]
timestamp: 2026-07-01T13:10:47Z
---

**Decision:** Extensions are WebAssembly modules run under wazero, each with its
own linear memory and key-value store, with **no direct OS access**. Every
capability — exec, file I/O, network, UI, MCP, agent control — is reached only
through a typed host bridge, and each capability is gated by a per-extension
`Permission` (`exec`, `file_open`, `file_read`, `file_write`, `network_read`,
`network_write`, `ui`).

**Rationale:** wllr's audience is developers who want a *better sandboxed agent*.
Trusted in-process plugins can do anything the host can; WASM isolation plus an
explicit permission model plus a narrow, typed capability surface means an
extension (or a misbehaving agent driving one) can only do what it was granted.

**Consequence:** New capabilities must be added as host-mediated bridge methods
with a permission check — never as raw syscalls from the extension. The
`permissions` extension is the interactive consent layer on top of this model.

# Applies To

- [extension package](../packages/extension.md)
- [permissions extension](../packages/ext-permissions.md)

# Origin

Inferred from the extension host design (interfaces.go, host.go permission checks) and confirmed as the project's stated purpose.
