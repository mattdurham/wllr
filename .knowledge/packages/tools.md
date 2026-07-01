---
type: Go Package
title: tools
description: Pure adapter from sdk.Tool (wllr's tool schema) to fantasy.AgentTool (the LLM provider abstraction).
resource: ./modules/tools
tags: [tools, adapter, fantasy]
timestamp: 2026-07-01T13:10:47Z
---

The `tools` package is a thin, pure adapter: it converts `sdk.Tool` values
(name, description, JSON schema) into `fantasy.AgentTool` values that the LLM
provider layer understands, wiring each tool's execution back through the host.
No state, no side effects beyond the conversion.

# Specification

- [Contracts and invariants](../../modules/tools/SPECS.md)
- [Design decisions](../../modules/tools/NOTES.md)
- [Test plan](../../modules/tools/TESTS.md)

# Key Interfaces

- The adapter function turning `sdk.Tool` + an executor into `fantasy.AgentTool`.

# Dependencies

- `sdk`, `charm.land/fantasy`
