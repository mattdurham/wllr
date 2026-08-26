---
type: Installed Extension
title: statusline (installed)
description: Renders the status-line scene area from live harness state via get_status_info.
resource: ./extensions/statusline
tags: [installed, statusline,ui,scene]
timestamp: 2026-07-01T13:10:47Z
---

The `statusline` installed extension owns the status-line scene area. It reads live harness state through `get_status_info` and renders the status row using the scene-graph UI, keeping status presentation out of the core. The context indicator shows actual window fill and remaining-to-threshold (`ctx:P%/R%`, e.g. `ctx:32%/48%`) and a `C<n>` badge marks how many compactions occurred this session; the node is hidden when the pool's context window is 0 (unknown for a local model without a configured window). It also reacts to `EventContextUsage` after each turn to keep the counts current.

# Source

- [extensions/statusline](../../extensions/statusline) — installed to `~/.wllr/extensions/statusline/` via `make extensions`

# Related

- [WASM extension authoring](../patterns/wasm-extension-authoring.md)
