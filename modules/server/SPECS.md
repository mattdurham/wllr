# modules/server — SPECS

The `server` package exposes a running wllr engine over HTTP so a non-terminal
front end (for example a browser) can read and drive a session. It is a
read-mostly view over an `*agent.AgentPool`: it observes engine callbacks and
broadcasts them, and it mutates engine state only through the pool's own public
methods.

## Boundary

- **Depends on:** `modules/agent`, `modules/sdk`, `modules/wire`, the standard
  library.
- **Never depended on by:** `modules/agent`, `modules/harness`, `modules/sdk`,
  `modules/wire`. The dependency arrow points one way, so the server can be
  omitted from a build without touching the engine.
- **The wire envelope is not defined here.** `Event`, `EventKind`, the `Kind*`
  constants, `TextInputParams`, and `ProtocolVersion` live in `modules/wire` and
  are re-exported as aliases: `Event` from `event.go`, `TextInputParams` from
  `textinputparams.go`, and `EventKind`, the `Kind*` constants, and
  `ProtocolVersion` from `wire.go` (`type Event = wire.Event`, `const
  KindToken = wire.KindToken`, …). This lets `modules/client` decode the same
  frames without importing this package (and, transitively, the engine). See
  `modules/wire/NOTES.md`.
- The server does **not** import `modules/harness` and does **not** use
  bubbletea. It can run with no TTY, which is what `wllr serve` relies on. It
  implements the renderer-neutral `sdk.UISink` (`HubSink`) and imports neither
  `modules/extension` nor `modules/harness`.
- **Peer proxying stays stdlib.** The instance reverse proxy (`proxy.go`) uses
  `net/http/httputil`, so the dependency list above is unchanged. In particular
  the server does **not** import `modules/client`: the operator's peer table is
  the server-owned `Peer` type, and `cmd` maps the shared instance-declaration
  parser onto it.
- **Shared, not duplicated, scene logic.** The server owns its own scene model
  and HTML renderer, but the graph-mutation semantics it uses (`sdk.ApplyOp` and
  friends) and the layout helpers (`sdk.ParseDimension`, `sdk.ResolveConstraint`,
  `sdk.PropSides`) live in `modules/sdk` and are the same ones the terminal
  renderer uses. The two front ends cannot drift on patch semantics; only the
  final node-to-pixels step differs (CSS here, lipgloss in the harness).

## Invariants

1. **Publish never blocks the engine.** `Hub.Publish` uses a non-blocking send to
   each subscriber; a full buffer drops the event and increments the drop
   counter. A stalled HTTP client must never stall an in-flight model turn.
2. **Sequence numbers are monotonic and gap-detectable.** Every published event
   gets the next `Seq`. A client that observes a gap knows it missed events and
   can re-sync from `GET /api/history`.
3. **Subscribe is atomic with replay.** Registration and the backlog snapshot
   happen under one lock, so an event published concurrently is either in the
   replay slice or delivered on the channel — never both, never neither.
   Cancellation only unregisters; it never closes the delivery channel, because
   `Publish` sends outside the lock and a close racing that send would panic
   (the reader stops on its own `ctx.Done()` instead).
4. **The bridge never clobbers another observer.** Pool-level observers are
   installed with the additive `AddUsageObserver` / `AddLifecycleObserver`, so
   the metrics recorder in `cmd` keeps receiving turns and lifecycle changes.
5. **No engine mutation outside the pool API.** Handlers call `pool.Send`,
   `pool.Cancel`, `pool.SnapshotInbox`, `pool.ClearInbox`, and
   `pool.EditInboxMessage`. They never touch `Agent` internals.
6. **Auth is enforced before any handler runs** when a token is configured, for
   every route including the stream.
7. **The HubSink owns the scene graph server-side.** Scene calls are applied to a
   `SceneModel` (scenemodel.go) and rendered to HTML (htmlscene.go); a client
   receives pre-rendered regions rather than reimplementing the node vocabulary.
   This keeps one renderer authoritative and matches the terminal renderer's
   behaviour because both mutate the graph through `sdk`.
8. **Scene and message HTML is escaped where it is rendered.** Scene text runs
   through `ANSItoHTML` (ansihtml.go), which escapes it while translating SGR
   styling into spans; message bodies run through `MessageHTML` (mdhtml.go),
   which escapes user text and escapes assistant text via the same converter. A
   message containing markup always renders as text. The client inserts these
   server-rendered fragments with `innerHTML` — safe because the server escaped
   them — but never interpolates raw content into markup itself.
9. **ANSI is translated, not re-interpreted.** The primary transcript is a scene
   area the bundled agents extension fills with `format_markdown` output (ANSI
   text). The server converts that styled text to spans (`ANSItoHTML`) rather
   than re-rendering the markdown, so the browser shows exactly what the terminal
   shows and one component owns the markdown decision.
10. **Markdown has one definition.** Assistant message bodies rendered
    server-side go through `modules/sdk/md` — the same renderer the extension
    calls via `format_markdown` — then `ANSItoHTML`. A message cannot render
    differently in the TUI and the browser because there is one renderer.
11. **Model selection reuses the harness's own list and ids.** `SetModelLister`
    installs the harness `ModelListFn`; `GET /api/models` returns keys built with
    `sdk.ModelPickerKey` and the shared `sdk.ModelPickerCallback`; a selection
    posted back routes through the harness `classifyCoreSelection`. Nothing about
    model selection is re-implemented server-side, so the browser's model menu is
    the terminal's.
12. **Peer topology and credentials stay server-side.** `GET /api/instances`
    reports ids and status only — never a peer `BaseURL` or `Token`. A `down`
    entry's `error` is a sanitized cause (`unreachable`, `timeout`, `http <code>`,
    `malformed response`), never the peer's scheme, host, port, or path: the raw
    `net/http` error would otherwise embed them. The proxy
    injects the peer's bearer token (`Authorization: Bearer <peerToken>`) and
    deletes the inbound credential in both forms the origin accepts (the
    `Authorization` header **and** the `?token=` query parameter), so the browser
    never holds a peer credential and an origin token is never relayed to a peer.
    A 3xx `Location` from a peer is reduced to a same-origin absolute path:
    any target carrying a scheme or authority — an absolute `http://host/x`, a
    protocol-relative `//host/x`, a backslash variant such as `/\host/x`, or a
    non-path scheme such as `javascript:` — collapses to its path and query (or
    `/`), so a cross-origin target can never reach the browser. Peer response
    headers are sanitised for the same reason: `Location` is relativised and
    `Set-Cookie` is stripped (nothing in the system is cookie-based), so a peer
    cannot plant state in the browser under the origin's origin.
13. **The instance proxy is not an open proxy.** Proxy targets come only from
    operator configuration (`Options.Peers`), and only a path under
    `/api/instances/{id}/api/` for a *known* peer id is forwarded. An unknown id
    (including the self id, which is served in-process and deliberately not
    proxiable) is `404`; a path outside the `/api` prefix never matches the route
    and is never forwarded. Containment is **lexical**: the origin only
    guarantees the forwarded path is under the proxy prefix. A `%2e%2e` segment
    is forwarded as a literal `..` under that prefix (the escape survives the
    mux, which cleans only the escaped path), so resolving `..` is the peer's
    responsibility, not the origin's.
14. **Switching instances is view-only.** The page renders one instance at a
    time and holds exactly one `EventSource`, which it closes and re-opens on
    switch. No cursor or sequence number is merged across instances
    (cross-reference `modules/client` SPECS invariant 2); the page's `resetView`
    clears every per-instance surface, and a stale-response guard drops a reply
    issued for an instance the user has switched away from.

## HTTP surface

| Method | Path                 | Purpose |
|--------|----------------------|---------|
| GET    | `/api/health`        | Liveness, `protocol`, `last_seq`, subscriber and drop counts |
| GET    | `/api/agents`        | Agent tree: id, name, running, inbox, history length, tools, compactions |
| GET    | `/api/history`       | Transcript for `?agent=` with `?limit=`/`?offset=`; returns `total` |
| GET    | `/api/context`       | The `/context` breakdown for `?agent=`: context window, last request, next-request estimate, canonical transcript (see below) |
| POST   | `/api/submit`        | `{"agent","content"}` → starts a turn (`202`) |
| POST   | `/api/cancel`        | `{"agent"}` → cancels the active turn |
| GET    | `/api/inbox`         | Snapshot of `?agent=`'s queued messages |
| POST   | `/api/inbox/clear`   | `{"agent"}` → discard queued messages |
| POST   | `/api/inbox/edit`    | `{"agent","index"|"message_id","content"}` → edit one queued message |
| GET    | `/api/stream`        | Server-Sent Events stream (see below) |
| GET    | `/api/scene`         | Every area with its rendered HTML (snapshot to seed a client) |
| GET    | `/api/models`        | Selectable models: `models[]` (each with a provider-qualified `key`), the `callback` to select under, and the live `current` provider/model |
| GET    | `/api/commands`      | Registered slash commands: `commands[]` (`{name,desc,instant}`) for autocomplete and help (see below) |
| POST   | `/api/ui/callback`   | `{"callback","value"}` (or `"args"`) → routes a UI selection back to the engine (see below) |
| GET    | `/api/instances`     | This origin's `self` id and one entry per configured peer (`id`, `self`, `status`, `protocol`, `last_seq`, optional `error`) — ids and status only, never a URL or token (see below) |
| ANY    | `/api/instances/{id}/api/…` | Reverse-proxies a known peer's `/api` surface (including its SSE stream) with the peer token injected server-side (see below) |
| GET    | `/`                  | Self-contained HTML front end (client.go); any other non-API path → `404` |

An omitted `agent` resolves to the root agent (`main`). An unknown agent yields
`404`. A missing `content` on submit yields `400`.

### UI callbacks (`POST /api/ui/callback`)

The stream sends picker/text-input/agent-tree frames carrying an opaque
`callback` id; the client posts the chosen value back here. The handler is
transport-only — it holds no routing logic. It forwards the callback to the
installed `sdk.UICallbackRouter` (serve mode installs a
`harness.ExtensionRouter`), which applies the shared `__wllr:` core/extension
split: extension callbacks are dispatched to the extension host as
`EventOnCommand` (this is how agent focus/kill and transcript rebuild work from a
browser), core callbacks go to the `Model`'s core handler
(`Model.CoreCallbackFunc`), which applies model / provider-model / thinking /
OpenRouter-routing selections through the same `apply*` methods the terminal
uses (harness SPECS §3b.i).

| Case | Status |
|---|---|
| routed | `200 {"callback","routed":true}` |
| no router installed | `501` |
| router returns `sdk.ErrNoCoreHandler` / `sdk.ErrNoExtensionHost` | `501` (unsupported here, client may fall back) |
| router returns any other error | `500` |
| missing `callback` or bad JSON | `400` |
| non-POST | `405` |

The `501`-vs-`500` split is deliberate: a core callback that no front end
handles — a multi-step setup flow (auth, local-model, context-window, catalog)
that the stateless core handler does not classify — answers `501` (not
implemented here) rather than `500`, and a client can treat it as "not available"
instead of a fault. A handled core selection answers `200` even when the apply
surfaced an error, because the error is reported as a `notify` frame (matching
the terminal's behavior) rather than a transport failure.

### Commands (`GET /api/commands`)

Returns the registered slash commands so a browser can offer the terminal's
autocomplete dropdown and help. The list is the harness registry — the same set
the terminal reads — because serve mode installs `Model.CommandLister()`. A
command registered by an extension after serve-mode wiring is forwarded through
`HubSink.RegisterCommand` into that registry (`SetCommandRegistrar`), so the
browser's list cannot diverge. A nil lister yields an empty list, not an error.

The autocomplete *trigger* rule (`sdk.SlashWordAt`) is shared; the client
reimplements the identical predicate, and `/model <x>` / `/thinking <x>` typed as
commands route through the same `__wllr:` core callbacks a picker sends, so a
typed command reaches the same engine handler a click would.

### Context breakdown (`GET /api/context`)

Returns the `/context` breakdown — the same composition view the terminal's
`/context` modal shows — for `?agent=` (default root). It is a read-only
snapshot of `agent.ContextBreakdown()`, safe to take while a turn runs (the
engine uses the same locks the submit paths do). `context.go` flattens the
engine struct into an explicit `contextView` rather than marshalling the engine
type: the struct has no JSON tags and embeds a `fantasy.Usage`, so the wire
shape is defined here as a stable contract. `estimated_total` is the engine's
own `Sum()` (system + tools + history), so the browser and terminal agree.

`last_request.is_estimate` distinguishes the pre-first-turn `chars/4` baseline
seed from a real provider report, exactly as the modal labels it.

### Instances (`GET /api/instances`)

`wllr serve` may be given an operator-configured peer table (`Options.Peers`,
from the `-peer`/`-peers`/`-peer-token` flags). This endpoint reports this origin
and those peers so the page's instance selector can list them. It sits on
`s.mux`, so it is behind `authenticate` exactly like every other route. A
non-`GET` method is `405 {"error":"GET only"}`.

```json
{
  "self": "local",
  "instances": [
    {"id":"local","self":true, "status":"ok",  "protocol":1,"last_seq":1234},
    {"id":"ci",   "self":false,"status":"ok",  "protocol":1,"last_seq":45},
    {"id":"dev",  "self":false,"status":"down","protocol":0,"last_seq":0,
     "error":"unreachable"}
  ]
}
```

| Field | Meaning |
|---|---|
| `self` (top level) | This origin's own instance ID (`Options.SelfID`, default `DefaultSelfID` = `"local"`). |
| `instances[]` | Self first, then peers in configured order. The order is deterministic. |
| `id` | Instance ID. The only key the page needs. |
| `self` (per entry) | `true` for the local instance. |
| `status` | `"ok"` or `"down"`. |
| `protocol` | The instance's wire protocol version. Self is read in-process (`ProtocolVersion`); a peer comes from its `GET /api/health`. Informational: the page does not gate on it (it only warns on a `ready` protocol mismatch), and the origin proxies to a peer regardless. |
| `last_seq` | The instance's last published hub sequence. Self in-process (`s.hub.LastSeq()`); a peer from its `/api/health`. Informational. |
| `error` | `omitempty`; present only when `status == "down"`. A sanitized failure class — `unreachable`, `timeout`, `http <status>`, or `malformed response` — never the peer's scheme, host, port, or path (invariant 12). |

Two deliberate choices worth stating explicitly:

1. **The top-level `self` string and the per-entry `self` bool are different
   fields.** `"self": "local"` names the origin; `"self": true` marks the
   origin's entry in `instances[]`. They read alike but mean different things.
2. **`protocol` and `last_seq` are always present** (no `omitempty` on the ints)
   and are `0` for a `down` instance, which keeps the contract pointer-free.
   They are informational: the page's selector renders only `id` and `status`,
   so they are exposed for other clients rather than gating the page.

Peer health is probed **concurrently**, one goroutine per peer, each bounded by
`probeTimeout` (1.5 s), so one dead peer cannot delay the list beyond its own
timeout. A result is cached for `probeTTL` (5 s) so repeated polls of the
endpoint cannot hammer a down peer. The probe runs on a **request-independent
base context**, so a client that aborts its own `GET /api/instances` cannot
cancel a probe (and cache a false `down`); a probe whose attempt *is* cancelled
is indeterminate and is never cached at all. A peer whose id is empty, outside
the routable charset (`^[A-Za-z0-9._-]+$`), duplicates another, collides with
self, or whose `BaseURL` is unusable is dropped at `New` (`buildPeers`) with a
warning: startup does not fail and no phantom instance reaches the selector.

**Never exposed:** a peer's `BaseURL` or `Token`, nor any of either inside the
`error` string. Ids, status, and a sanitized cause only.

### Peer proxy (`/api/instances/{id}/api/…`)

The origin reverse-proxies a peer's whole `/api` surface — including the SSE
stream — under `/api/instances/{id}/api/`, so the browser page can keep using
its existing relative paths prefixed by the selected instance. The route is a Go
1.22+ wildcard pattern (`/api/instances/{id}/api/`), so a path outside that
prefix never matches and is never forwarded.

| Aspect | Behaviour |
|---|---|
| Path rewrite | `/api/instances/ci/api/history?agent=main` → `<peer>/api/history?agent=main`. The `/api/instances/ci` prefix is stripped *before* the peer's base path is joined, so a `BaseURL` that carries a path (`http://host/wllr`) receives `/wllr/api/history` — the same URL the health probe resolves. The query is re-encoded through `url.Values.Encode()` (keys sorted, values re-escaped), not forwarded byte-for-byte. |
| Query | The inbound `token` parameter is deleted before forwarding; every other parameter is preserved (re-encoded). |
| Headers | The inbound `Authorization` is deleted; when the peer has a token, `Authorization: Bearer <peerToken>` is set. Hop-by-hop headers are handled by the stdlib proxy. |
| Body | Forwarded verbatim (needed for `POST /api/submit`, `/api/cancel`, `/api/inbox/*`, `/api/ui/callback`). |
| Unknown `{id}` | `404 {"error":"unknown instance"}` — includes the self id and any dropped peer. |
| Non-`/api` path | `/api/instances/ci/secret` and `/api/instances/ci/` do not match the route pattern; they fall through to `/` → `handleClient` → `404`. Never forwarded. Containment is lexical: a `%2e%2e` segment is forwarded as a literal `..` under the prefix (the mux cleans only the escaped path), so cleaning `..` is the peer's responsibility. |
| Response | Status, body, and sanitised headers forward. A 3xx `Location` is reduced to a same-origin absolute path — any scheme/authority (absolute, protocol-relative `//host`, a backslash variant, or a non-path scheme) is collapsed to its path+query (or `/`), so a cross-origin target never reaches the browser (invariant 12). `Set-Cookie` is stripped, so a peer cannot plant a cookie in the browser under the origin's origin. `text/event-stream` is flushed immediately (the stdlib `httputil` proxy does this for that content type). |
| Peer unreachable | `502 {"error":"instance unreachable"}` via the proxy's `ErrorHandler`. A down peer is a clean per-instance fault, not a hang. A peer that accepts the connection but never writes response headers is bounded by the shared transport's `ResponseHeaderTimeout` (`probeTimeout`), so it becomes a prompt 502 rather than an indefinite hang. |

The `Rewrite` hook deletes both credential forms the origin accepts (the
`Authorization` header and the `?token=` query parameter) before injecting the
peer's own token, so a browser URL carrying the origin's token is never relayed
to a peer (invariant 12).

## Event schema

One frame = one JSON object. The envelope and the kind vocabulary are defined
in `modules/wire` (`wire.Event`, `wire.EventKind`, `wire.ProtocolVersion`) and
re-exported here as aliases (`wire.go`), so `server.Event` and `wire.Event` are
the same type and there is one definition, shared with `modules/client`. `Kind`
selects the meaning of the remaining fields; unused fields are omitted.

| Kind              | Fields | Source |
|-------------------|--------|--------|
| `ready`           | `protocol`, `seq` | Sent once on stream connect |
| `user_message`    | `text`, `role`, `inbox` | Agent `AddOnTurnStart` |
| `turn_started`    | `main` | Pool usage observer (`Started`) |
| `token`           | `text` | Agent `AddOnToken` |
| `assistant_message` | `text` (raw markdown), `html` (rendered) | Agent `AddOnDone` (from the accumulated turn text), published before `turn_done` |
| `tool_call`       | `tool_call_id`, `tool_name`, `input` | Agent `AddOnToolCall` |
| `turn_done`       | `err` | Agent `AddOnDone` |
| `context_usage`   | `usage` (`sdk.ContextUsage`) | Pool context-usage dispatcher |
| `notify`          | `text` | Compaction notice via the dispatcher |
| `agent_spawned`   | `name`, `live`, `main` | Pool lifecycle observer |
| `agent_closed`    | `live`, `main` | Pool lifecycle observer |
| `tool_result`     | `tool_call_id`, `tool_name`, `output`, `is_error`, `agent_id` | Harness `harnessUIBridge.AfterToolCall` → `HubSink.ToolCallDone` |
| `error`           | `text` | Server-side (reserved) |

### Extension UI frames

Serve mode installs the harness's *real* extension bridges (agent, team, UI) via
`harness.InstallHeadlessBridges`, backed by the server's `HubSink` — the
non-terminal counterpart of the TUI's `teaSink`. Where the kernel would have
turned a UI request into a bubbletea message, `HubSink` publishes one stream
frame, so a web client sees the same UI requests the TUI renders. Because the
agent bridge carries the sub-agent spawner, `create_agent` and the team tools
work headlessly.

| Kind              | Fields | HubSink method (UISink) |
|-------------------|--------|-------------------------|
| `modal`           | `text` | `ShowModal` |
| `picker`          | `picker` (`sdk.ShowPickerParams`) | `ShowPicker` |
| `agent_tree`      | `agent_tree` (`sdk.ShowAgentTreeParams`) | `ShowAgentTree` |
| `focus`           | `text` (agent ID; empty = root) | `FocusAgent` |
| `text_input`      | `text_input` (`server.TextInputParams`) | `ShowTextInput` |
| `abort`           | — | `Abort` |
| `status`          | `key`, `value` | `SetStatus` |
| `console`         | `text` (one line) | `Console` |
| `console_clear`   | — | `Console` (clear) |
| `reset_history`   | `messages` (`[]sdk.Message`) | `ResetHistory` |
| `ui_area`         | `area` (`sdk.UIArea`) | `SceneCreate` |
| `ui_patch`        | `patch` (`sdk.UIPatchParams`) | `ScenePatch` |
| `ui_area_update`  | `area_update` (`sdk.UIUpdateAreaParams`) | `SceneUpdate` |
| `ui_area_remove`  | `text` (area ID) | `SceneRemove` |
| `ui_area_html`    | `area` and/or `area_id`, `html` | `SceneCreate`/`ScenePatch`/`SceneUpdate` |
| `ui_text_append`  | `area_id`, `node_id`, `text` | `ScenePatch` (append-only batch) |

### Scene rendering and the streaming fast path

`HubSink` applies every scene call to its `SceneModel` and then publishes the
result two ways:

- **`ui_area_html`** carries a *fully rendered* area fragment. It accompanies
  area creation, any structural patch (insert/remove/set_root/replace_text), and
  constraint updates. The client replaces the region wholesale.
- **`ui_text_append`** carries a single text delta (`area_id`, `node_id`, `text`)
  and is emitted only when a patch batch consists solely of `append_text` ops
  (`IsTextAppendOnly`). This is the HTML counterpart of the terminal renderer's
  append fast path (`SceneRenderer.RenderAppendTextNode`): a token-by-token stream
  appends to one element instead of re-sending the whole transcript per token.

A client seeds its view from `GET /api/scene` (which returns every area with
HTML in creation order) and then keeps it current from these frames. This makes
a client that connected after an area was created — or that reconnected beyond
the replay backlog — still correct.

The engine-mutating half of the extension contract (base system prompt, history
reset, tool-call bookkeeping) is performed by the harness `harnessUIBridge`,
which then forwards the user-visible effect to `HubSink`. `HubSink.Submit`
routes an extension-injected message to the focused agent (else the root) via
`pool.Send`; `RegisterCommand` and `AgentWakeup` are no-ops in serve mode
(extension-registered commands are forwarded to the harness registry for the
command list, and `turn_started` is already reported by the per-agent observers
— see NOTES.md). `SetModel` — the sink method — is a no-op because model
switching in serve mode runs through the core-callback path
(`__wllr:model` → `harness` `classifyCoreSelection` → `applyModelSelection`),
not through the extension-facing sink.

### Root-agent wiring and the Bridge subscribe additively

Serve mode wires the root agent exactly as the TUI does
(`harness.Model.WireMainAgentCallbacks`), which is what gives the root agent its
tool list and dispatches its extension events (`before_agent_start`, `token`,
steer). It passes a no-op `&sdk.FuncSink{}`: `Bridge` already publishes the root
agent's `token`/`turn_done`/`tool_call` frames, so passing the `HubSink` would
publish each of them twice.

**Invariant:** `Bridge` attaches its per-agent observers **additively**
(`AddOnToken`/`AddOnDone`/`AddOnToolCall`/`AddOnTurnStart`). The agent callbacks
are lists, and a `Set*` from either consumer would silently displace the other —
which is exactly how serve mode lost its streamed transcript and its user-prompt
bubbles. Neither consumer may assume it is the only one.

`Server.SetMessageEndNotifier(fn)` receives `(role, content)` when a turn's
assistant text is finalized, with the content the Bridge accumulated. Serve mode
points it at `harness.Model.DispatchMessageEnd`, so extensions see
`EventMessageEnd` — the event the `agents` extension renders the finalized
(markdown) assistant bubble on, and that `history` records the response from. A
front end with no bubbletea loop has no other trigger for it.

### Tool results

Tool results reach the stream through the extension `after_tool_call` path rather
than an engine callback: the engine exposes no per-agent tool-completion hook.
The harness `harnessUIBridge.AfterToolCall` marks the call done on the pool and
forwards to `HubSink.ToolCallDone`, which emits a `tool_result` frame carrying the
agent, tool name, and final output. Tool *inputs* arrive on `tool_call`. If no
extension fronts the tool call, only the input is streamed and the output remains
available from `GET /api/history`.

### Stream resume

`GET /api/stream?since=<seq>` replays retained events with `Seq > since` before
streaming live events. Retention is bounded by `Options.Backlog`
(`DefaultBacklog` = 4096); older events are not replayable, so a client that has
been away longer than the backlog must re-fetch history.

## HTML front end

The `GET /` page (client.go) is a single self-contained file with no external
assets. It holds no privileged access: it uses the same REST surface and event
stream as any other client, which is what demonstrates the renderer-neutral sink
— the TUI and this page drive one engine through one contract. It renders:

- the transcript (seeded from `/api/history`, extended by `user_message`,
  `token`, `tool_call`, `tool_result`, `turn_done`; replaced by `reset_history`);
- scene areas (seeded from `/api/scene`, kept current by `ui_area_html`,
  `ui_text_append`, `ui_area_update`, `ui_area_remove`), composited by placement;
- the status bar, notices, the console pane, and the overlay surfaces (modal,
  picker — including the two-pane **split picker** with a type-to-filter list,
  an arrow-key-highlighted selection, and a preview pane — agent tree, text
  input, and the **`/context` breakdown modal**);
- a composer (`POST /api/submit`) and a stop button (`POST /api/cancel`);
- an **instance selector** (the top-bar `<select id="instance">`): this origin
  and the operator-configured peers from `GET /api/instances`, switchable one at
  a time over a single re-subscribed stream (see "Instance selector and
  switching" below);
- a **queued-messages pane** (the focused agent's undrained messages, from
  `GET /api/inbox`, with a clickable clear posting to `/api/inbox/clear`) and a
  **tool-activity pane** (a rolling list of recent tool calls and their
  outcomes, driven by the `tool_call`/`tool_result` frames).

A picker item, agent-tree node, or text-input submit posts to
`/api/ui/callback` with the frame's `callback` id and the chosen value, so a
selection reaches the engine through the same router the API exposes (see UI
callbacks above). A `501` from that endpoint (a core callback with no handler)
is silent in the UI — the overlay still closes.

### Two transcript sources, exactly one active

The bundled agents extension drives a `chat` scene area that *is* the primary
transcript (user prompts, streamed assistant text, notifications) when it is
enabled. The page's own transcript list is the fallback for a deployment without
that extension. To avoid rendering every message twice, the page tracks whether a
`chat` area exists (`setChatArea`): when one does, the fallback transcript
suppresses message bubbles and collapses to the tool/activity log; when none does,
it is the transcript. Tool calls always go to the fallback list, because the
chat area never renders them. The queued-messages and tool-activity panes sit
outside the transcript entirely (as they do in the terminal), so they are shown
regardless of which transcript source is active.

### Queued-messages and tool-activity panes

These mirror the terminal's two sidebar panes and reuse the surfaces the API
already exposes rather than a new one:

- **Queued messages.** Seeded and refreshed from `GET /api/inbox?agent=` (the
  same pool snapshot the TUI reads), hidden when empty, showing the newest
  messages with a `… N older message(s)` hint when truncated, and a `clear`
  button that posts `/api/inbox/clear`. It refreshes when the turn starts
  (`user_message`), when it ends (`turn_done`), after a submit, and when the
  focused agent changes. Message content is inserted as text, never markup.
- **Tool activity.** Built from the stream: a `tool_call` frame appends a
  running row keyed by `tool_call_id`, and the matching `tool_result` marks that
  row ok/error in place (so a result never appends a duplicate). Rows show the
  ownering agent when it differs from the focus.

### Markdown rendering

Markdown reaches the browser rendered, never raw:

- **Scene transcript (extension path).** The extension calls `format_markdown`
  and streams ANSI into scene nodes; `ANSItoHTML` (ansihtml.go) translates the
  SGR styling to spans (`htmlscene.go` uses it for every text node).
- **Fallback transcript (API path).** `/api/history` returns an `html` field per
  message (`MessageHTML`, mdhtml.go) and the `assistant_message` frame carries
  the rendered form of the just-completed turn. The page shows raw tokens while
  a turn streams and swaps in the rendered HTML on `assistant_message` — the
  same raw-then-rendered behaviour the terminal transcript has.

Both paths use `modules/sdk/md`, so the browser and the terminal agree on what
the markdown means. Every fragment is escaped by the converter, which is what
makes inserting it with `innerHTML` safe.

### Model selection in the browser

The top-bar model button opens a picker built from `GET /api/models`. Items show
the label, provider, tier tags, and a `current` marker, and clicking one posts the
item's `key` under the returned `callback` to `/api/ui/callback`. The key is
provider-qualified (`sdk.ModelPickerKeySep`) so a choice owned by another provider
switches provider, exactly as the terminal picker does. The list, the keys, and
the callback id all come from the server, so the page hardcodes none of them.

The page checks the server's `protocol` from `/api/health` against the version
substituted into the script at serve time, so there is one source of truth
(`wire.ProtocolVersion`).

Picker items, agent-tree nodes, and text-input submits post the frame's
`callback` id (with the chosen value) to `/api/ui/callback`, so a browser
selection reaches the same engine handler the terminal would have invoked (see
"UI callbacks" above).

### Instance selector and switching

The top-bar `#instance` selector is built from `GET /api/instances`. Each option
is labelled with the instance id, suffixed `(down)` when its probe failed. Every
per-instance call is routed through one `api(path)` helper: the local instance
keeps the plain relative path (served in-process) and a peer is prefixed with
`api/instances/<id>/` (the origin's reverse proxy). `GET /api/instances` and
`GET /api/health` are the only calls that stay unprefixed.

Switching is view-only. `switchInstance` closes the page's single `EventSource`,
clears every per-instance surface via `resetView` (focus, agents, in-flight
bubbles, usage, statuses, provider, chat-area flag, inbox, tools, commands,
suggestions, the transcript, the three scene containers, the queued/tool panes,
the agent selector, the status bar, the model button, the console, and the
composer's disabled state), re-seeds from the new instance, and re-subscribes.
Two safeguards keep instances from leaking into each other:

- **A stale-response guard.** Each `refresh*` captures `state.instance` when it
  starts and drops its response if the user has switched away, so a slow reply
  from instance A can never paint into instance B's view.
- **Direct DOM reset of the composer.** `resetView` re-enables `#input`/`#send`
  directly rather than through `setInstanceReachable(true)`, which early-returns
  when the reachability flag is already true and would otherwise leave a down
  instance's disabled composer disabled forever.

The selection is persisted in `localStorage` and reflected in a `?instance=`
query parameter (so a reload or a shared link lands on the same instance). The
`?instance=` value is written by building on the existing query string
(`URLSearchParams`), so unrelated parameters such as `?token=` survive, and the
fragment (`location.hash`) is re-appended, so a deep link's anchor survives a
switch. An
unknown deep link falls back to `self`. A down instance shows `id (down)`, its
stream's `onerror` disables the composer, and `refreshInstances` re-runs on every
`ready` frame (and on `onerror`) so the selector reflects a recovered peer
without a switch — the page does not poll on a timer.

### Known limitation: the page requires auth to be disabled

The HTML front end talks to the API over `fetch()` and `EventSource`, neither of
which can present a bearer token, and the page does not read the token from its
own URL. Under `WLLR_API_TOKEN` every call from the page is a `401`, so the page
is inert: the instance selector stays empty and the stream never opens. **Auth
and the browser page are mutually exclusive**: an operator who needs auth must
not use the browser page (use `wllr-multiplex` or curl with a bearer token
instead). This is pre-existing and out of scope to change here; the page is a
loopback convenience.

### Split picker and the `/context` modal

A `picker` frame whose `picker.split` is set renders the terminal's two-pane
picker: a type-to-filter list on the left and the highlighted item's `preview`
(falling back to `sublabel`) on the right, navigated with the arrow keys and
confirmed with Enter. Filtering matches label, id, sublabel, *and* preview text,
the same haystack the terminal folds together.

The top-bar **context** button fetches `GET /api/context` and renders the
breakdown as text in a modal, mirroring the terminal's `/context` output (window,
last request labeled provider-reported or seed, next-request estimate with per-
source and per-tool attribution, and the not-in-context canonical transcript).
The numbers come from the engine; only their formatting is done in the page.

## Configuration

- **Addr** defaults to `127.0.0.1:8765` (loopback only). Exposing the API on a
  non-loopback address is an explicit operator choice and should be paired with a
  token.
- **Token** comes from `Options.Token`; `wllr serve` reads `WLLR_API_TOKEN`.
  Empty disables auth.
- **SelfID** names this origin in `GET /api/instances`; it defaults to
  `DefaultSelfID` (`"local"`).
- **Peers** is the operator's remote-instance table (`[]Peer` of `{ID, BaseURL,
  Token}`). The URL and token stay server-side (invariant 12). `wllr serve`
  builds it from the repeatable `-peer id=url` flag, `-peers <file.json>`, and
  the `-peer-token` default. With no peers configured, `GET /api/instances`
  returns just this origin and every other behaviour is unchanged.
