---
type: Pattern
title: Scene-graph UI from an extension
description: Extensions draw UI declaratively via ui_create_area / ui_patch host calls into named scene areas the harness renders.
tags: [ui, scene-graph, extension, uibridge]
timestamp: 2026-07-01T13:10:47Z
---

The UI an extension draws is a **declarative scene graph**, not direct terminal
writes. An extension creates a named area with a placement (e.g. status,
transcript), then applies patches that set/replace a tree of `UINode`s
(vstack/hstack/text/divider). The harness `SceneRenderer` (driven by `UIBridge`)
renders the current tree at the right width, and refreshes when the scene is
mutated off-loop. This is how `statusline` owns the status row and `agents`
owns the chat transcript.

Host calls: `ui_create_area`, `ui_update_area`, `ui_patch`, `ui_remove_area`
(require the `ui` permission). Node/patch types live in `sdk` (`UINode`,
`UIArea`, `UIPatchOp`); author helpers are `UICreateArea`, `UIPatch`,
`UIText`/`UIVStack`/`UIHStack`/`UIDivider`, etc.

# Example

```go
UICreateArea("statusline", "status")
UIPatch("statusline", /* ops: set root to a vstack of text nodes */)
```

# Applies To

- [harness package](../packages/harness.md) (SceneRenderer, UIBridge)
- [statusline extension](../packages/ext-statusline.md), [agents extension](../packages/ext-agents.md)
