---
type: Decision
title: Model tiers route work by cost, not model name
description: Named provider/model/thinking tiers (high/low) are tagged in /models and referenced by config, skills, and sub-agents instead of literal model IDs.
tags: [models, providers, harness, skills, subagents, config]
timestamp: 2026-09-21T00:00:00Z
---

**Decision:** Introduce **model tiers**: named `{provider, model, thinking}`
targets stored in `wllr.model_tiers` in the shared config group. Two names are
reserved and wired to built-in behavior — `high` (the expensive planning model)
and `low` (the cheaper working model) — but the map accepts any names. Tiers
are tagged interactively in the `/models` picker (`h`/`l` tag, `u` untags) and
referenced by name from `/model <tier>`, a skill's frontmatter, and the
sub-agent default.

**Rationale:** The user wants to plan on an expensive high-thinking model and
delegate execution to a cheap one without naming a model each time. Tying the
choice to a stable name rather than an ID means the underlying models can change
without touching skills or prompts. A tier may name a *different provider* than
the session model, so a plan tier can be Anthropic while the working tier is
local; the pool's single-provider model factory cannot express that, which is
why sub-agent defaulting goes through a host-installed resolver
(`AgentPool.SetSubagentResolver`) rather than pool internals.

**Consequence:**

- `cmd` owns tier storage, validation, and cross-provider resolution; the
  harness owns the picker keys and command surface, and duplicates only the two
  reserved tier *names* (it cannot import `cmd`).
- `set_model` is a new permission-free host call so the skills extension can
  apply a skill's tier without the host learning about providers.
- An explicit `model` in `create_agent` always overrides the working tier; with
  no `low` tier tagged, sub-agents fall back to the session model.
- `AgentPool.ResolveSubagentModel` returns the resolved model *name* alongside
  the language model, so compaction sizing uses the model actually running.
- A tier's model uses **its own** context window. Local models resolve it from
  their `local_models` entry or endpoint discovery, never from the session
  model's window (see [local model context window
  resolution](local-model-context-window-resolution.md)). A tier whose window
  cannot be resolved is rejected with an actionable error rather than switched
  to, and a sub-agent tier degrades to the session model.

# Applies To

- [harness package](../packages/harness.md) (picker tagging, `/model` tier application)
- [agent package](../packages/agent.md) (sub-agent resolver)
- [extension package](../packages/extension.md) (`set_model` host call)
- [skills extension](../packages/ext-skills.md) (frontmatter `model:`/`thinking:`)

# Origin

GitHub issue #43. Supersedes the earlier per-task routing idea in closed #38
(that proposed automatic classification; this is explicit, user-tagged tiers).
