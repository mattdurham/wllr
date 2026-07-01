---
type: Installed Extension
title: tasks (installed)
description: In-memory task lists with atomic multi-worker claiming (tasklist_create, tasks_create/update/list/get/claim).
resource: ./extensions/tasks
tags: [installed, tasks,coordination,tools]
timestamp: 2026-07-01T13:10:47Z
---

The `tasks` installed extension adds task-management tools. `tasks_claim` atomically selects the lowest pending, dependency-satisfied task under the list lock, so racing workers never get the same task — the basis for multi-agent coordination. See its [README](../../extensions/tasks/README.md).

# Source

- [extensions/tasks](../../extensions/tasks) — installed to `~/.wllr/extensions/tasks/` via `make extensions`

# Related

- [WASM extension authoring](../patterns/wasm-extension-authoring.md)
