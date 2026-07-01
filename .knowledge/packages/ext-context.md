---
type: Installed Extension
title: context (installed)
description: Injects project context (AGENTS.md / CLAUDE.md and cwd files) into the system prompt.
resource: ./extensions/context
tags: [installed, context,prompt]
timestamp: 2026-07-01T13:10:47Z
---

The `context` installed extension gathers project context — global `~/.wllr/AGENTS.md`/`CLAUDE.md` and per-cwd context files — and injects it into the agent's system prompt at session start.

# Source

- [extensions/context](../../extensions/context) — installed to `~/.wllr/extensions/context/` via `make extensions`

# Related

- [WASM extension authoring](../patterns/wasm-extension-authoring.md)
