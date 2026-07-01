---
type: Go Package
title: agent
description: LLM turn execution, the agent pool, sub-agent/team spawning, and runtime model/provider-option swapping.
resource: ./modules/agent
tags: [agent, pool, turns, concurrency, sub-agents, teams]
timestamp: 2026-07-01T13:10:47Z
---

The `agent` package runs LLM turns. An `Agent` owns a conversation history, an
inbox mailbox, and a language model; the `AgentPool` registers agents (the
`main` agent plus spawned sub-agents) and holds the shared `fantasy.Provider`.
It supports runtime model switching (`SetModel`) and reasoning-level switching
(`SetProviderOptions`) without a restart, sub-agent spawning, and team
coordination.

# Specification

- [Contracts and invariants](../../modules/agent/SPECS.md)
- [Design decisions](../../modules/agent/NOTES.md)
- [Test plan](../../modules/agent/TESTS.md)

# Key Interfaces

- `Agent` — one conversation; `Submit`, `SetModel`, `SetProviderOptions`, `ModelName`, mailbox `Deliver`
- `AgentPool` — `Spawn`, `Get`, `Close`, `LanguageModelForModel`, `SetProvider`, `SetDefaultModelName`, `SetContextWindow`
- `Spawner` — sub-agent construction (applies Anthropic thinking budget)
- `ProviderRequestInterceptor` — the before_provider_request reroute hook

# Cross-cutting Decisions

- [Single lmMu guards runtime model/provider-option swaps](../decisions/single-lmmu-runtime-swaps.md)

# Dependencies

- `sdk`, `charm.land/fantasy` (LLM provider abstraction)

# Usage Patterns

- [Interceptor transform chain](../patterns/interceptor-transform-chain.md)
