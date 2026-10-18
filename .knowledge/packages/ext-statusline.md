---
type: Built-in Extension
title: statusline (built-in)
description: Renders the status-line scene area from live harness state via get_status_info.
resource: ./extensions/statusline
tags: [builtin, statusline,ui,scene]
timestamp: 2026-07-01T13:10:47Z
---

The built-in `statusline` extension owns the status-line scene area. It reads live harness state through `get_status_info` and supplies the scene fields. The model identifies the selection without a separate provider label. The harness fits the bundled tree to terminal cell width: collapse then hide the directory, drop secondary fields, shorten the model while retaining activity and context, and finally clamp with an ANSI-safe ellipsis. Fitting works on a cloned tree so resizing restores hidden content. The context indicator shows used tokens over the maximum context (`ctx:used/max`, e.g. `ctx:32000/524288`) and a `C<n>` badge marks successful compactions. The context node is hidden when the window is unknown. The extension reacts to `EventContextUsage` after each turn.

# Source

- [extensions/statusline](../../extensions/statusline) — embedded as `cmd/builtins/statusline.wasm`

# Related

- [WASM extension authoring](../patterns/wasm-extension-authoring.md)
