---
type: Decision
title: A single lmMu guards runtime model and provider-option swaps
description: Agent.lm, modelName, and providerOpts are read/written together under one RWMutex so /model and /thinking can switch the live agent safely.
tags: [agent, concurrency, model, thinking]
timestamp: 2026-07-01T13:10:47Z
---

**Decision:** `Agent` guards `lm`, `modelName`, and `providerOpts` with one
`lmMu sync.RWMutex`. `SetModel` and `SetProviderOptions` write under it;
`Submit` snapshots all three under `RLock` at the start of a turn.

**Rationale:** `/model` and `/thinking` change the running main agent without a
restart. A turn already in flight must finish on the values it captured, while
the next `Submit` picks up the swap — one mutex keeps "model + its request
options" swapped together atomically and avoids a data race with in-flight reads.

**Consequence:** Never read/write these fields without `lmMu`. Model and its
provider options are always changed together, between turns.

# Applies To

- [agent package](../packages/agent.md)

# Origin

modules/agent/NOTES.md §27 (SetModel) and §28 (SetProviderOptions).
