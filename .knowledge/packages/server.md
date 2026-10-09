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

It can also front a small **peer table**: an operator may configure remote wllr
instances, and the origin reports them — ids and status only — from
`GET /api/instances` and reverse-proxies each peer's `/api` surface (SSE included)
under `/api/instances/{id}/api/`, injecting the peer's token server-side. The
browser switches between instances one at a time; it never merges them and never
holds a peer URL or credential.

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
- `instances.go` — `GET /api/instances` (self + peers, ids/status only) and the
  TTL-cached peer health probe (`instanceProbe`); a down entry's `error` is a
  sanitized cause, never the peer URL
- `proxy.go` — the peer reverse proxy (`net/http/httputil`): forwards a known
  peer's `/api` surface with the peer token injected and origin credentials
  stripped server-side
- `client.go` — the self-contained browser front end

# Cross-cutting Decisions

- [The wire protocol is a leaf package](../decisions/wire-protocol-extraction.md)
- [The TUI is decoupled behind Renderer + UIBridge](../decisions/tui-decoupled-behind-renderer.md)
- [Serve instances switch via an origin-server reverse proxy](../decisions/serve-instance-switching.md)

# Dependencies

`agent`, `sdk`, `wire`, standard library. Does **not** import `harness` or
`extension`, so it runs with no TTY (`wllr serve`). Peer proxying adds no
dependency: `net/http/httputil` is standard library, and `modules/client` is
still **not** imported (the operator's peer table is the server-owned `Peer`
type).
