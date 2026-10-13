# modules/wire — NOTES

## Why the envelope was extracted from `modules/server`

The stream envelope (`Event`, `EventKind`, `ProtocolVersion`, `TextInputParams`)
was written inside `modules/server/wire.go`, next to the HTTP handlers that
happened to emit it. That was fine while `server` was the only thing that spoke
the protocol.

The multi-instance client changed that. A client must decode the same frames the
server emits, and the obvious way to guarantee that is to share the type rather
than re-declare it (which is exactly the drift the project avoids elsewhere —
see the `sdk` scene-graph and model-picker-key decisions). But importing
`modules/server` from a client pulls in `modules/agent` and, transitively, the
Anthropic/AWS/Google provider SDKs: ~80 MB of engine to parse JSON.

**Decision:** move the envelope to a leaf package `modules/wire` that depends
only on `sdk`, and have `modules/server` re-export each symbol as an alias. The
alias approach matters: `Event` and the `Kind*` constants are referenced roughly
300 times inside `server`, and aliases keep every one of those sites compiling
unchanged, preserving a single definition without a mechanical rename.

The measurable payoff: `wllr-multiplex` (which imports `modules/client` →
`modules/wire`) is ~11 MB, while the engine binary is ~93 MB. A client does not
ship the engine.

## Aliases, not duplicates

`type Event = wire.Event` and `const KindToken = wire.KindToken` are Go aliases:
`server.Event` and `wire.Event` *are the same type*, so a `wire.Event` produced by
a client is assignable to a `server.Event` parameter and vice versa. This is what
makes "one definition" true at the type level and not just by convention.

## Flat envelope over a tagged union

`Event` is one flat struct with `omitempty` fields rather than an interface with
per-kind structs. The consumer is a browser script (and a thin Go client) that
switches on `kind`; a flat struct means one decode path and no nested
discriminator. The cost is that irrelevant fields are present-but-zero, which the
`omitempty` tags keep off the wire.

## What is deliberately *not* here

The REST response shapes (`Agent`, `Message`, `History`, `Model`, …) live in
`modules/client` as the client's own decode structs. They are small, they are
consumed only by clients, and the server does not share a Go type for them — it
writes `map[string]any`. If a REST shape ever needs a handshake guarantee (not
just JSON field names), it should move here alongside the stream envelope.
