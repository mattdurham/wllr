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
