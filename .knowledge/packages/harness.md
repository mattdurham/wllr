---
type: Go Package
title: harness
description: The Bubble Tea TUI — rendering, input, pickers, slash commands, scene graph — decoupled from subsystems behind the Renderer interface.
resource: ./modules/harness
tags: [tui, bubbletea, ui, pickers, commands, renderer]
timestamp: 2026-07-01T13:10:47Z
---

The `harness` package is the terminal UI: the Bubble Tea model, chat transcript,
input area, autocomplete dropdown, modal/picker overlays, status bar, and the
declarative scene renderer that WASM extensions draw into. It owns the built-in
slash commands (`/help`, `/clear`, `/reload`, `/model`, `/thinking`, `/login`,
`/status`, `/tools`, `/prompt`) and the core-owned pickers. It is decoupled from
the agent/extension subsystems behind the `Renderer` interface, so the frontend
is swappable.

# Specification

- [Contracts and invariants](../../modules/harness/SPECS.md)
- [Design decisions](../../modules/harness/NOTES.md)
- [Test plan](../../modules/harness/TESTS.md)

# Key Interfaces

- `Renderer` (`modules/harness/renderer.go`) — the TUI decoupling seam (AppendToken, ShowModal, ShowPicker, SetStatus, ResetHistory, …)
- `Model` — the Bubble Tea model; callback fields wire core features: `ModelListFn`/`SelectModelFn`, `ThinkingListFn`/`SelectThinkingFn`, `RecordAuthFn`/`BeginOAuthFn`/`CompleteOAuthFn`, `OnUserMessage`/`OnMessageEnd`
- `Command` / `Registry` — slash-command dispatch (Instant fast path)
- `SceneRenderer` — the scene-graph area renderer fed by `UIBridge`

# Cross-cutting Decisions

- [TUI decoupled behind Renderer + UIBridge](../decisions/tui-decoupled-behind-renderer.md)
- [Reserved __wllr: picker-callback prefix](../decisions/reserved-picker-callback-prefix.md)

# Dependencies

- `sdk`, `agent`, `extension`, `charm.land/bubbletea/v2`

# Usage Patterns

- [Reserved-callback core pickers](../patterns/reserved-callback-pickers.md)
- [Scene-graph UI](../patterns/scene-graph-ui.md)
