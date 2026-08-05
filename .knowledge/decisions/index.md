# Decisions

* [Sandboxed by design — WASM isolation + permissions + typed bridges](sandboxed-by-design.md) — extensions have no direct OS access; capabilities are host-mediated and permission-gated.
* [sdk is the only package shared by harness and extension](sdk-sole-shared-dependency.md) — all boundary types in a leaf package; no cycles.
* [The TUI is decoupled behind Renderer + UIBridge](tui-decoupled-behind-renderer.md) — swappable frontend via `session.Wire`.
* [A single lmMu guards runtime model and provider-option swaps](single-lmmu-runtime-swaps.md) — `/model` and `/thinking` switch the live agent safely.
* [The history extension is the sole session store](history-is-sole-session-store.md) — core `session.Journal` removed.
* [Reserved "__wllr:" prefix for core-owned picker callbacks](reserved-picker-callback-prefix.md) — one picker widget, two routing paths.
* [Capabilities over policy — ship the seam, not opinions](capabilities-over-policy.md) — interceptor chain + hookable events, not baked-in behavior.
* [Modules are spec-driven — the // NOTE invariant](spec-driven-modules.md) — code changes update SPECS/NOTES/TESTS.
* [GitHub Issues are the source of truth for work tracking](github-issue-tracking.md) — use `gh` when available, with GitHub web tooling as the fallback.
