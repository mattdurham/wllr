---
type: Decision
title: The wire protocol is a leaf package
description: The HTTP/SSE frame envelope lives in modules/wire (a leaf) and is aliased by modules/server, so clients decode frames without linking the engine.
tags: [wire, protocol, client, server, architecture, leaf]
timestamp: 2026-10-08T00:00:00Z
---

**Decision:** The stream envelope — `Event`, `EventKind`, the `Kind*` constants,
`TextInputParams`, and `ProtocolVersion` — lives in `modules/wire`, a leaf
package that depends only on `sdk`. `modules/server` re-exports each symbol as an
alias (`type Event = wire.Event`, `const KindToken = wire.KindToken`, …).

**Rationale:** A client must decode the same frames the server emits, and sharing
the type is how the two cannot drift. But importing `modules/server` from a client
transitively imports `modules/agent` and the Anthropic/Google/AWS provider SDKs —
roughly 80 MB of engine to parse JSON. Extracting the envelope keeps one
definition while letting a client link the protocol alone. The aliases matter:
`Event` and the `Kind*` constants are referenced ~300 times inside `server`, and
aliases keep every site compiling unchanged.

**Consequence:** New frames (a kind, a field, a payload struct) are added in
`wire`, not `server`. `wire` must stay a leaf. The alias is a true Go alias, so
`server.Event` and `wire.Event` are the same type and are interchangeable.

Measured payoff: `wllr-multiplex` (client) is ~11 MB against the engine's ~93 MB.

# Applies To

- [wire package](../packages/wire.md), [client package](../packages/client.md), [server package](../packages/server.md)

# Origin

Extracted while adding the multi-instance client (`modules/client`) in the
HTML/API/multi-instance work.
