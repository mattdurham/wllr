---
type: Decision
title: The TUI is decoupled behind Renderer + UIBridge
description: Subsystems never call Bubble Tea directly; the frontend is swappable by implementing Renderer + UIBridge and calling session.Wire.
tags: [harness, session, tui, decoupling]
timestamp: 2026-07-01T13:10:47Z
---

**Decision:** The agent/extension/session subsystems talk to the UI only through
the `harness.Renderer` interface and the `UIBridge`. To swap the TUI, implement
`Renderer` + `UIBridge` and call `session.Wire(host, pool, mainID, yourRenderer)`.

**Rationale:** Isolates the Bubble Tea implementation so the core is testable
without a terminal and the frontend is replaceable (e.g. a headless or alternate
UI) without touching agent/extension logic.

**Consequence:** Do not reach into Bubble Tea from non-harness packages. New
UI-facing capabilities are added as `Renderer`/`UIBridge` methods.

# Applies To

- [harness package](../packages/harness.md), [session package](../packages/session.md)

# Origin

Documented in AGENTS.md and modules/harness/renderer.go.
