---
type: Go Package
title: client
description: A plain HTTP/SSE client for one or many wllr instances, including the multiplexer that merges N instances' event streams.
resource: ./modules/client
tags: [client, multiplex, http, sse, aggregation]
timestamp: 2026-10-08T00:00:00Z
---

The `client` package connects to running wllr engines over their HTTP/SSE API. It
reads (health, agents, history, models, scene), writes (submit, cancel,
UI-callback), and, via `Multiplexer`, follows **N instances at once** and merges
their streams into one, tagging every event with its source instance.

It does **not** embed the engine: it links only `wire` and `sdk`, so client
binaries are small and many can run against many servers.

# Specification

- [Contracts and invariants](../../modules/client/SPECS.md)
- [Design decisions](../../modules/client/NOTES.md)
- [Test plan](../../modules/client/TESTS.md)
- [Performance notes](../../modules/client/BENCHMARKS.md)

# Key Types

- `Config` — one instance (unique `ID`, `BaseURL`, optional `Token`)
- `Client` — one instance: REST helpers + `Follow`
- `Multiplexer` — N instances merged into one `<-chan Tagged`
- `Tagged` — a `wire.Event` + its `Instance` + a `Gap` flag; `AgentKey()` = `"instance/agent"`
- `Backoff` — reconnect schedule (exponential, capped, jittered)

# Application

- `cmd/wllr-multiplex` — the reference CLI (`agents`, `models`, `history`, `submit`, `cancel`, `tail`)

# Cross-cutting Decisions

- [The wire protocol is a leaf package](../decisions/wire-protocol-extraction.md)

# Dependencies

`wire`, `sdk`, standard library. Nothing from the engine.
