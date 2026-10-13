# modules/client — BENCHMARKS

## Memory and CPU

The client is I/O-bound; it holds one goroutine and one buffered channel per
instance. There is no busy-waiting: a follower blocks on the HTTP response body,
and reconnect sleeps via a timer.

- **Merged buffer:** `DefaultMergeBuffer` = 1024 events. Bounded so a stalled
  consumer cannot grow memory without limit; when full, followers block
  (backpressure) rather than drop.
- **Per-subscriber (server side):** the server's Hub buffers 512 events per
  connection and drops beyond that. A client that keeps up never sees a gap.

## Reconnect cost

Backoff starts at `DefaultBackoffBase` (500 ms), doubles, and caps at
`DefaultBackoffMax` (30 s), with 20% jitter. A single instance that flaps costs
one HTTP GET (the SSE open) per retry; a reconnect with a `since` cursor
transfers only the missed events, not the whole backlog.

## Binary size

The client links `modules/wire` + `modules/sdk`, not the engine: `wllr-multiplex`
is ~11 MB against the engine's ~93 MB. See `modules/wire/BENCHMARKS.md`.
