---
type: Decision
title: Local model context window resolution — config entry, then endpoint, then nothing
description: How wllr resolves a local model's context window at startup and on model switch, and why a window-less result clears the pool's window instead of inheriting the previous model's.
tags: [local-models, context-window, config, discovery, compaction]
generated: { by: agent:main, at: 2026-08-08T00:00:00Z }
status: stable
---

The context window of the *selected local model* is resolved with a fixed
precedence and is applied to the agent pool unconditionally — a zero result
clears the pool's window rather than leaving the previous model's value in
place:

1. **Explicit user override** (`WLLR_CONTEXT_WINDOW` env or `context_window`
   in the `wllr` config group) — always wins; `cfg.ContextWindowConfigured`
   marks it and no model metadata ever overwrites or clears it.
2. **The selected model's `local_models` entry `context_window`** —
   authoritative over anything the endpoint exposes, because it is the value
   the user deliberately typed.
3. **The endpoint's exposed value** (`/models` fields: `context_length`,
   `max_context_length`, `max_model_len`, `num_ctx`, `top_provider.context_length`)
   — fills in only when no config value exists.
4. **Nothing** — a model with no window in any source resolves to 0: the
   pool keeps a 0 window, the statusline `ctx` indicator stays hidden, and
   agent-side compaction keeps its defensive 1M fallback (see
   `modules/agent` SPECS §9). The display never invents a number.

**Invariants** (enforced in `cmd/localmodels.go` and `cmd/config.go`):

- `rememberLocalModel` and `applyLocalModelSelection` both apply the
  precedence above to `cfg.ContextWindow` / `cfg.LocalContextWindow`. A
  window-less *discovered* model must NOT strip a window the user configured
  for it (config entry wins); a window-less model with no config value
  CLEARS a window inherited from a previous selection.
- The stale-selection fallback in `resolveLocalProviderConfig` (selected
  model no longer in the endpoint listing) routes through `rememberLocalModel`
  for the replacement, so the same precedence applies and the replacement's
  window — or its absence — is what the pool sees.
- The `/model` and provider pickers apply `contextWindowForSelection`
  unconditionally (0 included) so an in-session switch to a window-less model
  also clears; an explicit override survives every switch.
- Discovery logs a warning when the endpoint reports a window that differs
  from an explicitly configured one (a model swap that did not update the
  config); it does not silently override the user's value.

**Why clearing instead of inheriting:** the regression this guards against is
a window-less model silently inheriting another model's configured window
(e.g. a 262k window on model A leaking into model B after a switch or stale
selection), which drives the compaction threshold and the status display with
a number that is not the current model's. A hidden indicator plus the agent
module's defensive fallback is the honest state for "window unknown".

## Affected code

- `cmd/localmodels.go` — `resolveLocalProviderConfig`, `discoverLocalModels`
  (config-wins merge + mismatch warning), `rememberLocalModel`,
  `applyLocalModelChoice`.
- `cmd/config.go` — `applyLocalModelSelection` (LoadConfig path).
- `cmd/modelcatalog.go` — `contextWindowForSelection` (picker path: explicit
  override, config entry, pool-resolved window, catalog).
- `cmd/main.go` — startup `pool.SetContextWindow(cfg.ContextWindow)` plus the
  picker's unconditional apply.

## Tests

- `TestResolveLocalProviderConfigClearsStaleWindowOnFallback`,
  `TestResolveLocalProviderConfigAdoptsReplacementWindowOnFallback`,
  `TestResolveLocalProviderConfigHonorsExplicitWindowOnFallback`,
  `TestApplyLocalModelChoiceWindowlessModelClearsInheritedWindow`,
  `TestApplyLocalModelChoiceConfigWindowSurvivesWindowlessEndpoint`,
  `TestDiscoverLocalModelsConfiguredWindowWinsOverEndpoint`
  (all in `cmd/localmodels_test.go`).
