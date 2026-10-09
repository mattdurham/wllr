---
type: Go Package
title: server
description: Exposes a running wllr engine over HTTP — REST for history/submit/agents and an SSE event stream — plus a self-contained HTML front end.
resource: ./modules/server
tags: [server, http, sse, api, html, frontend]
timestamp: 2026-10-08T00:00:00Z
---

The `server` package exposes a running wllr engine over HTTP so a non-terminal
front end (a browser, a CLI) can read and drive a session. It is a read-mostly
view over an `*agent.AgentPool`: it observes engine callbacks and broadcasts them
through a `Hub`, and mutates engine state only through the pool's public methods.

It installs the renderer-neutral `sdk.UISink` (`HubSink`), so serve mode drives
the *same* extension bridges the TUI uses and a browser sees the same UI requests
the terminal would render.

# Specification

- [Contracts and invariants](../../modules/server/SPECS.md)
- [Design decisions](../../modules/server/NOTES.md)
- [Test plan](../../modules/server/TESTS.md)
- [Performance notes](../../modules/server/BENCHMARKS.md)

# Key Types

- `Server` — endpoints, auth, graceful shutdown
- `Hub` — N-subscriber fan-out with a bounded replay backlog; non-blocking publish
- `Bridge` — translates engine callbacks into `wire.Event`s
- `HubSink` — the HTTP/SSE `sdk.UISink` implementation
- `SceneModel` / `htmlscene.go` — scene graph → HTML (shared `sdk` graph ops)
- `client.go` — the self-contained browser front end

# Cross-cutting Decisions

- [The wire protocol is a leaf package](../decisions/wire-protocol-extraction.md)
- [The TUI is decoupled behind Renderer + UIBridge](../decisions/tui-decoupled-behind-renderer.md)

# Dependencies

`agent`, `sdk`, `wire`, standard library. Does **not** import `harness` or
`extension`, so it runs with no TTY (`wllr serve`).
