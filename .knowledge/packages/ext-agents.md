---
type: Built-in Extension
title: agents (built-in)
description: Sub-agent orchestration — create_agent, message passing, and the WASM-driven chat transcript.
resource: ./extensions/agents
tags: [built-in, agents, orchestration, chat, wasm]
timestamp: 2026-07-01T13:10:47Z
---

The `agents` built-in extension (embedded in the binary) provides sub-agent
orchestration: a `create_agent` tool, message passing between agents, and it
owns the WASM-driven chat transcript scene area. It uses the AgentBridge and the
scene-graph UI host calls.

# Source

- [extensions/agents](../../extensions/agents)
- Built via `make extensions` → embedded as `cmd/builtins/agents.wasm`

# Uses

- AgentBridge (spawn/message), scene-graph UI ([Scene-Graph UI pattern](../patterns/scene-graph-ui.md))

# Related

- [agent package](agent.md), [WASM extension authoring](../patterns/wasm-extension-authoring.md)
