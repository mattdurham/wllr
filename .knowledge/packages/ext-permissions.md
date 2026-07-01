---
type: Installed Extension
title: permissions (installed)
description: Interactive permission gating for capability requests (the sandbox's consent layer).
resource: ./extensions/permissions
tags: [installed, permissions,sandbox,security]
timestamp: 2026-07-01T13:10:47Z
---

The `permissions` installed extension is the consent layer of the sandbox: it gates capability requests, giving the developer control over what an agent may exec, read, write, or reach on the network. See its [README](../../extensions/permissions/README.md).

# Source

- [extensions/permissions](../../extensions/permissions) — installed to `~/.wllr/extensions/permissions/` via `make extensions`

# Related

- [WASM extension authoring](../patterns/wasm-extension-authoring.md)
