# modules/server — BENCHMARKS

No formal benchmarks yet. The performance-relevant properties are documented so
future work can preserve them:

## Publish is O(subscribers) and non-blocking

`Hub.Publish` snapshots the subscriber set under the hub lock, then performs one
non-blocking channel send per subscriber. The engine callback that calls it (a
token delta, a lifecycle change) is therefore never blocked by a slow HTTP
client. A full 512-event subscriber buffer drops the event instead of stalling.

## Backlog trim avoids unbounded growth and array pinning

Retention is bounded by `backlogCap`. When the cap is exceeded, the retained
window is copied into a right-sized slice rather than re-slicing, so the backing
array does not grow without bound as events are published indefinitely.

## Hot path is the token stream

The highest-frequency event is `token` (one per model text delta). Each publish
does one map-iteration over the subscriber set and one buffered send per
subscriber; there is no allocation on the fast path when a subscriber buffer has
room. `Event` values are copied into the channel, so no per-event heap
allocation is required beyond the caller's existing one.

Extension UI frames (`ui_patch` in particular, which fires per streamed scene
update) travel the same non-blocking path, so a client that cannot keep up drops
scene patches rather than stalling the extension call. A dropped `ui_patch` is
gap-detectable by `Seq`; the client can re-fetch a scene snapshot from
`GET /api/scene` to resync.

## Scene re-render is the cost the append fast path avoids

A structural scene patch re-renders the whole area's HTML server-side. That is
O(area size) per patch, which would be unacceptable at token frequency. A patch
batch consisting solely of `append_text` ops therefore takes the fast path: the
model applies the append and the sink emits one small `ui_text_append` frame per
op, with no render. The terminal renderer made the same split
(`RenderAppendTextNode`); the wire format mirrors it. A future benchmark should
measure the delta between the two paths for a long streamed message.

## Future benchmarks to add

- `BenchmarkHubPublish` — publish throughput with 0, 1, and 8 subscribers.
- `BenchmarkHubPublishFullBuffer` — cost of the drop path under a stalled client.
- `BenchmarkSSEFrameEncode` — JSON encoding per frame.
- `BenchmarkRenderAreaHTML` — render cost for a chat area at transcript scale,
  to bound the structural-patch path.
- `BenchmarkANSItoHTML` — the converter runs over every scene text node and every
  assistant message body, so it is on the render hot path. It has a fast path
  that bypasses parsing when the input contains no ESC; a benchmark should
  confirm the no-ANSI case (plain user prompts) costs only an escape, and bound
  the styled case at transcript scale.
- `BenchmarkMessageHTML` — the `md.Render` + `ANSItoHTML` pipeline per message,
  since a history fetch renders every message at once (O(history) per request).
