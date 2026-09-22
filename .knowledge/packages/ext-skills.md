---
type: Installed Extension
title: skills (installed)
description: Discovers and exposes skills as slash commands, injecting skill instructions on invocation.
resource: ./extensions/skills
tags: [installed, skills,commands]
timestamp: 2026-07-01T13:10:47Z
---

The `skills` installed extension discovers skill definitions and registers them
as slash commands; invoking one injects its instructions into the conversation.

SKILL.md frontmatter may declare `model:` (a [model tier](../decisions/model-tiers.md)
name such as `high`, or an exact model ID) and optional `thinking:`. On activation
the extension calls the `set_model` host call before injecting the skill body, so
the first turn that reads the skill already runs on the requested model. A failed
switch is logged and the skill still runs on the current model.

# Source

- [extensions/skills](../../extensions/skills) — installed to `~/.wllr/extensions/skills/` via `make extensions`

# Related

- [WASM extension authoring](../patterns/wasm-extension-authoring.md)
