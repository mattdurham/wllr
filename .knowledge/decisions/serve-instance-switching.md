---
type: Decision
title: Serve instances switch via an origin-server reverse proxy
description: The origin server reverse-proxies a peer's whole /api surface (A1) instead of following each peer with a modules/client (A2), talking to peers directly (B), or merging instances into one timeline (C); the browser switches one instance at a time.
tags: [server, client, instances, proxy, sse, architecture]
timestamp: 2026-10-09T00:00:00Z
---

**Decision:** The browser reads a *peer* instance's API surface through the
origin server, which front-routes it with a small `net/http/httputil` reverse
proxy (approach **A1**): a peer's whole `/api` surface, including its SSE stream,
is forwarded under `/api/instances/{id}/api/…` with the peer's token injected
server-side. The browser renders **one instance at a time** and *switches* — it
never merges instances into one timeline. Rejected: **A2**, a long-lived
`modules/client` follower per peer re-served by the origin; **B**, direct
browser→peer connections with CORS; **C**, a merged multi-instance view.

**Rationale:** A1 wins on every axis that matters here.

- **Size.** A1 is ~40 lines of stdlib (`proxy.go`) plus a health probe
  (`instances.go`). A2 needs a `modules/client` connection per peer, a reader
  goroutine each, a buffer of peer frames, and a re-encode of every peer frame
  into the origin's stream — new mutable server state with its own lifecycle.
- **No new dependency, no lifecycle.** `httputil` is standard library, so the
  server's dependency list is unchanged; there is no goroutine to start, stop, or
  restart when a peer flaps.
- **Outages stay visible.** A2 hides a peer outage behind `Follow`'s
  forever-retry: the browser sees a connection that is simply quiet, and adds a
  second reconnect layer under the browser's own `EventSource` reconnect. A1 lets
  the peer's own HTTP status surface: the probe reports `down` and the proxy
  answers `502`, so the page can disable the composer and show `id (down)`.
- **The page gets nothing extra.** The browser already reconnects its
  `EventSource` natively and holds no cursor, so a server-side re-serve buys no
  latency or correctness it cannot get from an honest forward.

**Why switching, not merging (C).** The Hub stamps its own monotonic `Seq` per
instance, so a merged view would mix cursors: a gap in instance A tells the page
nothing about instance B, and resuming from one `since` across two streams is
meaningless (client SPECS invariant 2). Merging would also need an `instance`
field on `wire.Event` and a second merge implementation — and merging is already
`wllr-multiplex`'s job. The browser therefore switches: one instance rendered at a
time, one `EventSource`, no shared cursor.

**Why not direct browser→peer (B).** `EventSource` cannot set request headers, so
a bearer credential would have to travel as `?token=` — landing in browser
history, referrer headers, and proxy logs — or live in `localStorage`, i.e. a
credential the page can read. CORS would widen a loopback, token-authenticated API
to every origin the browser visits, and a mixed-content page (`https` front end)
could not reach an `http` peer at all. It would also thread N base URLs through
every call site instead of one `api()` helper.

**Consequence:** The peer `BaseURL` and `Token` stay server-side (invariant 12):
`GET /api/instances` reports ids and status only, and a `down` entry's `error` is
a sanitized cause (`unreachable`, `timeout`, `http <status>`, `malformed
response`) rather than the raw `net/http` error, which would embed the peer's
scheme/host/port/path. The proxy strips both credential forms the origin accepts
(the `Authorization` header and the `?token=` query) before injecting the peer's
own token. Aggregation *by merging* remains a client-side concern
(`wllr-multiplex`) and is deliberately out of the server.

# Applies To

- [server package](../packages/server.md) (`instances.go`, `proxy.go`)
- [client package](../packages/client.md) (the shared instance-declaration parser)

# Origin

The HTML/API/multi-instance work: adding an instance selector to the browser
front end so one page can read the local origin and operator-configured peers.
