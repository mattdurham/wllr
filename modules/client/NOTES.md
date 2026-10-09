# modules/client — NOTES

## Why the instance-declaration parser lives here

`wllr serve` and `wllr-multiplex` both take a repeatable `id=url` flag and a
JSON `[{"id","url","token"}]` file, with the same token precedence. Two copies
of the same ~25-line parser would drift, so it lives once, here — the package
that already owns `Config`, the type the parser produces. An alternative was
`cmd/internal` (a shared command package), but that adds a package boundary for
no additional property: `client` is already imported by both binaries, and the
parser is naturally an operations helper on the type it builds.

### One function, not three

The design sketched three names — `ParseInstanceFlags`, `LoadInstanceFile`, and
`BuildInstances`. The shipped shape is a single `BuildInstances` (plus the
`InstanceFlags` flag helper). Each of the split parts would have had exactly one
caller and no independent user, so the split was two extra exported names for
nothing. `BuildInstances` is the exact shape the `wllr-multiplex` `buildConfigs`
already had, so the behavior and every error string `wllr-multiplex` emits are
unchanged — the refactor is behavior-preserving, pinned by `instances_test.go`.
The one addition since is that `BuildInstances` now takes the *caller's* flag
labels (`instanceFlag`, `configFlag`) so error wording names a flag the caller
actually has: `wllr-multiplex` passes `"instance"`/`"config"` (its strings are
byte-for-byte unchanged), `wllr serve` passes `"peer"`/`"peers"`. It also returns
a `ErrNoInstances` sentinel for the zero-instance case, so `wllr serve` can recognize
"a peer source was given but yielded nothing" and continue rather than abort. The
refactor is otherwise behavior-preserving except for one deliberate validation
change: a file entry with an empty `id` or `url` is now rejected at startup
(`wllr-multiplex` previously accepted `[{"id":"ci","url":""}]` unchecked), so
file entries are validated exactly like flag entries — the M3 review item, and
`client/SPECS.md` describes it.

## The cursor bug that the gap test caught

The first cut adopted the `ready` frame's `Seq` as the resume cursor, on the
reasoning that `ready.Seq` is "the last sequence number" and so a natural
starting point. It is not. `ready.Seq` is the server's **last** sequence number
at connect time, while the events that follow are the **replay** (an older
window). If a connection dropped right after `ready` and before the replay was
read, resuming from `ready.Seq` would ask the server for "everything after the
latest", permanently skipping the replay window.

The rule that fixes it: **the resume cursor is the highest `Seq` actually
delivered to `OnEvent`.** The `ready` frame and any `Seq == 0` frame carry no
cursor information and leave the cursor untouched. This is the single most
important invariant in the package, so it has its own spec item and its own test
(`TestFollowReconnectsAndResumesFromCursor`, `TestFollowFlagsGapOnSequenceDiscontinuity`).

There is a genuine tradeoff in the choice: resuming from the last *delivered*
event can re-deliver an event that was sent but not processed before the drop.
That is correct for at-least-once delivery — better to see an event twice than to
lose it — and the `Gap` flag plus a history re-fetch is the escape hatch when
exactly-once matters to a consumer.

## Why the multiplexer does not deduplicate across instances

Two instances are independent histories; the same `Seq` means nothing across
them, and the same agent name is common (`main`). The only merge the client
performs is *interleaving* — one channel, every event tagged with its instance.
Any application-level correlation (same user, same task) is the consumer's job,
because only the consumer knows what "the same" means. `AgentKey()` gives the
unambiguous per-agent identity; `ParseAgentRef` turns it back into parts for
addressing a specific instance's agent.

## Backpressure direction is the opposite of the server

The server's `Hub` **drops** events for a slow subscriber, because stalling would
stall an in-flight model turn. The client's `Multiplexer` **blocks** — a slow
consumer applies backpressure to the follower goroutines. The asymmetry is
deliberate: the server protects the engine, the client protects correctness, and
neither has the other's constraint.

## Jitter defaults and test determinism

`Backoff` jitters by 20% so N instances that dropped together (a host restart)
do not reconnect in lockstep. Because the default uses the package-global random
source, tests disable jitter with `Backoff.NoJitter` (and may inject a seeded
`Rand`) rather than depending on timing. The reconnect tests use millisecond
bases so they finish quickly without being flaky.

## Why `Follow` pumps the `ready` frame when `OnConnect` is set

`OnConnect` reports the protocol version so a caller can gate compatibility, and
the version arrives on the `ready` frame — the first frame. Rather than have the
caller re-read it, `Follow` reads exactly one frame, calls `OnConnect`, then
hands that same frame to `OnEvent` like any other. When `OnConnect` is nil the
frame is left to the normal drain loop. Either way the frame reaches `OnEvent`
exactly once.

## Importing `wire`, not `server`

The client imports `modules/wire` for the frame envelope. It does **not** import
`modules/server`, which would drag in `modules/agent` and the provider SDKs. This
is the whole reason `wire` exists; see `modules/wire/NOTES.md`.

## Why the client does not expose `Scene`

An earlier cut gave the client a `Scene` helper for `GET /api/scene`, on the
reasoning that a caller seeding a view before the stream catches up would want
it. Nothing ever called it: the front end reads the scene over the stream plus
its own `fetch("api/scene")`, and the `wllr-multiplex` reference app only lists
and tails. An exported method with no caller is dead weight in a published API —
every reader has to decide whether to use it — so it was removed rather than
kept on speculation. The server-side endpoint and its `HubSink` snapshot are
unchanged, so restoring the helper is a two-line change against an endpoint that
already exists.
