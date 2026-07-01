# wllr — Knowledge

wllr is a terminal AI coding assistant (Bubble Tea v2 TUI) for **developers who
want a better sandboxed agent**: capabilities (exec, file, network, UI, MCP) are
mediated through typed host bridges and a permission model, and behavior is
extended via isolated WebAssembly modules rather than trusted in-process plugins.

This is the project knowledge catalog. Start here, then navigate to the relevant section.

## Sections

* [Packages](packages/index.md) — Core Go modules + built-in and installed extensions (20 concepts)
* [Decisions](decisions/index.md) — Cross-cutting architectural decisions (8)
* [Patterns](patterns/index.md) — Reusable patterns: WASM authoring, scene-graph UI, pickers, interceptors (4)
* [Playbooks](playbooks/index.md) — Pre-commit checks, adding an extension (2)
* [Features](features/index.md) — Planned work (2)

## The WASM extension API (what most authors want)

* **ABI reference:** [docs/extensions.md](../docs/extensions.md) — required exports, host_call methods, events, payloads, permissions.
* **How to write one:** [Authoring a WASM extension](patterns/wasm-extension-authoring.md) — copy `wllrsdk.go`, write `main.go`.
* **Drawing UI:** [Scene-graph UI](patterns/scene-graph-ui.md) — `ui_*` host calls into named areas the harness renders.
* **Changing behavior:** [Interceptor transform chain](patterns/interceptor-transform-chain.md) — transform/block tool calls and provider requests.
* **The host side:** [extension package](packages/extension.md) — the five bridges an embedder implements.

## Quick reference — the most important things to know

* **It's a sandbox.** Extensions run as isolated WASM with no direct OS access; every capability is host-mediated and permission-gated. See [Sandboxed by design](decisions/sandboxed-by-design.md).
* **`sdk` is the contract.** All host↔extension types live in the [sdk package](packages/sdk.md), the only package shared by harness and extension.
* **The UI is swappable.** Subsystems talk to the TUI only via [Renderer + UIBridge](decisions/tui-decoupled-behind-renderer.md); swap the frontend with `session.Wire`.
* **Core stays unopinionated.** wllr ships seams (interceptors, hookable events), not policy — see [Capabilities over policy](decisions/capabilities-over-policy.md).
* **Modules are spec-driven.** Every `modules/` change updates SPECS/NOTES/TESTS — see [the // NOTE invariant](decisions/spec-driven-modules.md).
