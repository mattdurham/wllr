# modules/server — NOTES

## Why the wire envelope moved to `modules/wire`

The stream envelope (`Event`, `EventKind`, `ProtocolVersion`, `TextInputParams`)
was defined in `wire.go` here. When the multi-instance client
(`modules/client`) needed to decode the same frames, importing `modules/server`
would have pulled in `modules/agent` and the provider SDKs — ~80 MB of engine to
parse JSON. The envelope was extracted to the leaf package `modules/wire`
(depends only on `sdk`), and `wire.go` now re-exports it as aliases so the ~300
references in this package and its tests are unchanged and there is still one
definition. `type Event = wire.Event` is a true alias: `server.Event` and
`wire.Event` are the same type, so a `wire.Event` is assignable to a
`server.Event` parameter. See `modules/wire/NOTES.md` for the full rationale.

## Why the pool observers became additive

The pool originally exposed four single-slot hooks — `SetUsageObserver`,
`SetLifecycleObserver`, `SetWakeNotifier`, `SetContextUsageDispatcher` — written
for one consumer each (the metrics recorder and the TUI harness). Adding an API
server as a second consumer would have silently clobbered whichever installed
first: in `cmd`, metrics installs usage and lifecycle observers during startup,
and the server would have replaced them.

**Decision:** convert the usage and lifecycle hooks to append-only lists with the
original `Set` kept as *replace* and new `Add` methods as *append*. This is the
broadcast layer the multi-client design needs, done where the fan-out actually
belongs (the pool), rather than by the server trying to multiplex a single slot.
The observers are snapshotted under the lock and invoked outside it, so an
observer that calls back into the pool cannot deadlock. The `Set`-replace
semantics are preserved for existing callers and tests.

`SetContextUsageDispatcher` and `SetWakeNotifier` were left single-slot on
purpose: the context dispatcher is meaningful only to whichever front end is
active (TUI *or* serve mode, never both), so `wllr serve` claims it explicitly
and the TUI harness claims it in interactive mode. A future second consumer of
context usage should get the same additive treatment.

## Why per-agent observers, attached via the lifecycle observer

The engine's stream callbacks (`SetOnToken`, `SetOnToolCall`, `SetOnTurnStart`,
`SetOnDone`) are per-`Agent`, a deliberate choice recorded in
`modules/agent/NOTES.md` (token/completion notifications are per-agent, not
pool-level). The bridge therefore attaches them to each agent as it appears,
using the pool lifecycle observer to notice spawns after `Start` and a sweep of
existing agents at `Start`. Because a spawn reports the ID before the creator
relationship is recorded, the bridge only reads `Name()`/`ID()` at attach time,
which are set at spawn.

## Why SSE, not WebSocket

SSE is one-way (server → client), which is exactly the shape of the event
stream: client → server actions are the REST calls (`submit`, `cancel`, inbox
edits). It needs no upgrade handshake, no framing library, works through plain
`net/http`, and is trivially consumable from a browser `EventSource`. A
`tool-*`-style bidirectional channel would add a dependency and a protocol for no
gain until the client needs low-latency upstream messages, at which point a
WebSocket endpoint can be added alongside without disturbing this one.

The one trap: any middleware wrapping the `ResponseWriter` must forward
`http.Flusher`. `statusRecorder` (the logging wrapper) embeds the
`http.ResponseWriter` *interface*, which does not include `Flush`, so the SSE
handler's `w.(http.Flusher)` assertion failed and every stream returned
`streaming unsupported`. The wrapper now implements `Flush` (delegating when the
underlying writer supports it) and `Unwrap` (so `http.ResponseController`, and
future hijack-based upgrades, keep working). This is covered by a test.

## Why `wllr serve` reuses all of `main()`

Serve mode is not a lighter-weight path. `main()` performs the full wiring —
provider, pool, extension host, built-in extensions, MCP bridge, native tools,
config isolation — and `serve` is a branch taken after all of it, exactly like
`--exec`. The subcommand `wllr serve [addr]` is rewritten into the `--serve`
flag form (`serveArgs`) so it reuses the flag parsing rather than duplicating
startup. Bypassing the wiring would mean a server that silently skipped
extension hooks, the provider-request chain, or session recording — the same
mistake `--exec` was explicitly designed to avoid.

Serve mode is headless, so it must log to stderr: `setupLogging` keys off "is
this the TUI", and the TUI test was `execPrompt == ""`. A serve run has an empty
exec prompt, so it was misclassified as TUI and its startup logs went only to the
(possibly absent) `logging` extension. The condition now excludes serve mode too.

## Why HubSink, and how serve mode installs the real bridges

Serve mode once left the harness's `earlyUIBridge`/`earlyAgentBridge` no-op stubs
in place, because only `Model.SetProgram` swapped in the full bridges and serve
mode never calls it. Every extension UI request was dropped and every
`agent_spawn` failed with "session not yet started". The stubs also meant the
server could not reuse the harness bridges without pulling in bubbletea.

**Decision:** extract the front-end effects into a renderer-neutral `sdk.UISink`
and give the server `HubSink`, an implementation that publishes a stream frame
per effect. The harness exposes `InstallHeadlessBridges`, which builds the
sub-agent spawner and installs the *same* agent/team/UI bridges the TUI uses,
backed by the sink. `runServe` calls
`harness.InstallHeadlessBridges(h, pool, agent.MainAgentID, logFn, srv.HubSink())`.

The server therefore no longer imports `modules/extension` (it only needs
`sdk`), and — more importantly — serve mode is no longer a lighter-weight
variant: it drives the same bridges as the TUI, differing only in the sink. This
is what makes `create_agent` and the team tools work headlessly.

**What the sink deliberately does not do:**

- `RegisterCommand` performs no TUI slash-command *dispatch*, but forwards
  registration to the harness registry (see "one command registry" below), so
  the command appears in `GET /api/commands` alongside the built-ins.
- `SetModel` is a no-op. In the TUI, switching models/tiers runs through the
  harness's `setModelMsg` handler, which resolves tiers and may rebuild the
  provider. That routing is the concern of the dedicated callback-routing step.
  Returning nil is honest: the request was accepted, nothing changed.
- `AgentWakeup` is a no-op. The harness main-agent callbacks are **not** wired to
  the sink in serve mode — the server's own per-agent observers (`Bridge`) already
  publish `token`/`tool_call`/`turn_done`/`turn_started`, so emitting from the sink
  too would duplicate them. The sink methods exist so the wiring *could* be moved
  to the harness path without code changes.

## Why the scene is rendered server-side, and why the graph ops live in sdk

The TUI's scene graph is drawn by `harness.SceneRenderer`, which is lipgloss
code and cannot be reused without pulling bubbletea into the server. The first
HTML increment therefore faced a fork: render each area in the browser (a second
implementation of the node vocabulary, in JS) or render it server-side and ship
HTML. **Decision:** render server-side. The node vocabulary
(`text`/`vstack`/`hstack`/`viewport`/`spinner`/`divider` and every prop) is
implemented once, in Go (`htmlscene.go`), so a new node type or prop cannot be
supported on one front end and forgotten on the other. The browser client stays
thin: it inserts server-rendered HTML and appends streamed text deltas.

That still leaves the *mutation* semantics (insert/remove/append/replace,
batch-atomicity, the insert-index rules) to be implemented somewhere. Writing
them a second time in the server would be the exact duplication this whole
exercise set out to remove. **Decision:** move the pure graph operations and
layout helpers into `modules/sdk` (`uigraph.go`, `uilayout.go`) and have both
renderers call them. The harness keeps thin package-local shims
(`resolveConstraint`, `collapseBlankLineRuns`) so its call sites and tests are
unchanged; `SceneRenderer.ApplyPatch` now calls `sdk.ApplyOp`. The op-error
strings moved with the code, so the two front ends reject a bad batch with the
same message.

### The append fast path, mirrored

The terminal renderer does not re-render an area on every token; it has an
append fast path (`RenderAppendTextNode`). The server mirrors this: a patch batch
that is *only* `append_text` emits one `ui_text_append` frame per op and no
re-render, while any structural change emits a full `ui_area_html`. Without this,
a long streamed message would re-send the whole transcript for every token — the
same blow-up the TUI went out of its way to avoid.

### What the scene model is not

It is not a layout engine. Areas carry placement and sizing hints and the client
composites by placement, but final layout (analogous to the TUI's terminal
geometry math) is not reproduced: the browser's CSS does the compositing. The
constraint strings are translated to CSS lengths rather than resolved against a
terminal size.

## Why markdown is translated from ANSI, not rendered twice

The primary transcript is not the server's to render: the bundled agents
extension owns a `chat` scene area and fills it with `format_markdown` output,
which is ANSI-styled terminal text. So the browser's problem is not "render
markdown" but "take the styled text the extension already produced and show it
in a DOM".

**Decision:** `ANSItoHTML` (ansihtml.go) translates SGR sequences to spans, and
`htmlscene.go` runs every scene text node through it. The fallback transcript
(the API-driven path, used when there is no chat extension) renders through
`modules/sdk/md` — the very renderer the extension calls — and then the same
converter (`MessageHTML`, mdhtml.go).

**Rationale:** re-rendering the markdown in the server for the scene path would
be impossible without reimplementing what the extension already did — the ANSI
is all the server receives, and it has already lost link hrefs and other
structure. Translating the styled text is the only faithful option, and it
happens to guarantee the browser and terminal agree, because they are showing the
same bytes. For the fallback path, using `md` keeps one definition of markdown
rather than a second Go renderer or a JS library whose behaviour would drift.

**Consequence:** `ANSItoHTML` is the single place a message body becomes HTML,
so it is also where escaping is enforced — it escapes all text, styled or not.
That is what makes the client's `innerHTML` of server-rendered fragments safe.
The lie-detector test `TestMessageHTMLNeverEmitsRawEscapes` asserts no input
yields a raw control byte.

### Two transcript sources, and why only one is active

The chat scene area and the client's own transcript list would both render every
message once the extension is enabled, so the page tracks whether a `chat` area
exists and suppresses its message bubbles when one does (it collapses to the
tool/activity log, which the chat area never renders). Without this, a deployment
with the extension would show each message twice. The detection is driven by the
area frames themselves, so it is correct whether the area is present at connect
(found via `/api/scene`) or created later (`ui_area`).

## Why the queue and tool panes reuse existing surfaces

The terminal's queued-messages and tools panes are harness-owned
(`renderQueuedMessages` / `renderToolActivity`), rendered outside `ChatView`.
The browser equivalents deliberately introduce no new endpoint or frame:

- The queued pane reads `GET /api/inbox` and clears with `/api/inbox/clear` — the
  same pool snapshot and mutation the TUI already used. Mirroring an existing
  surface keeps the two front ends from drifting on what "queued" means.
- The tool pane is assembled purely from the `tool_call` / `tool_result` frames
  the stream already carries, keyed by `tool_call_id` so a result updates its
  call's row instead of appending a duplicate.

Both sit outside the transcript, so they render regardless of which transcript
source is active — the same layering the terminal has.

## Why the context breakdown is flattened, not marshalled directly

`agent.ContextBreakdown` is an engine struct with no JSON tags and an embedded
`fantasy.Usage`, so marshalling it would leak internal field names and expose a
third-party type as the wire contract. `context.go` maps it to an explicit
`contextView`, keeping the API stable across internal renames — the same reason
`messageView` exists for history. `estimated_total` is deliberately the engine's
`Sum()` rather than a recomputation, so the browser cannot disagree with the
terminal about the total. Rendering the view to text is left to the page: the
breakdown is a harness-owned modal (the reimplement-not-share decision), and the
only page-side work is formatting numbers, not deciding what they mean.

## Why the client page is syntax-checked by node

Go compiles the server but not the JavaScript inside the embedded page, so a
syntax error in `clientHTML` would ship and only appear as a blank page in a
browser. `TestHandleClientScriptParses` extracts the `<script>` body and runs
`node --check` over it, skipping when node is absent so the suite does not
require a JS toolchain.

## Why command discovery reuses the harness registry

The browser needs the terminal's autocomplete dropdown and help. Rather than
define a server-side command table, serve mode installs `Model.CommandLister()`
(the harness registry) so `GET /api/commands` returns the terminal's set, and
points `HubSink.SetCommandRegistrar` at `Model.RegisterCommand` so an extension
command registered *after* serve-mode wiring still lands in that one registry.

**Rationale:** a second command table would be a second thing to keep in sync,
and a divergence (a browser offering a command the terminal does not, or missing
the trigger where the terminal shows it) is invisible until a user types it. The
*trigger rule* is shared for the same reason — the client reimplements the small
`sdk.SlashWordAt` predicate, and a test pins the harness delegate to it.

**Execution is deliberately split by front end.** Discovery is shared, but a
command's *effect* is not one operation: the terminal runs the registry handler
in its event loop, the browser has none. The client performs locally the
commands that open its own UI (the model picker, the /context modal, the tools
pane, the login picker, help) and routes the rest through the existing callback
endpoint — `/model <x>` and `/thinking <x>` become the same `__wllr:` core
callbacks a picker sends, and an extension command becomes the same
`EventOnCommand` dispatch a button uses. No second command dispatcher exists in
`server`; it stays transport-only.

## Why the callback router is split across sdk and harness

A picker or text-input selection travels the opposite way from every other
frame: it goes *into* the engine. The TUI resolves it inside its own key handler
(`Model.updateKeyPressPicker`), which needs the live model and an event loop. A
browser has neither, so it POSTs `/api/ui/callback` and needs a router.

**Decision:** the contract (`sdk.UICallbackRouter`), the `__wllr:` split
(`sdk.IsCoreCallback`), and the programmatic implementation (`sdk.FuncRouter`)
live in `sdk`; the concrete dispatcher (`harness.ExtensionRouter`) lives in
`harness`, where the extension host already is. The `Server` holds only an
`sdk.UICallbackRouter` interface and forwards to it.

**Rationale:** the split rule is part of the renderer-neutral boundary — if the
browser and the TUI disagreed about which callbacks are core, the same picker
would behave differently in each. Keeping the predicate in `sdk` means both read
one definition. The `Server` stays transport-only: it cannot reach engine state,
because the router it calls is an interface it did not write.

**Consequence:** the `501`-vs-`500` distinction comes straight from the router's
sentinels (`sdk.ErrNoCoreHandler`, `sdk.ErrNoExtensionHost`). Serve mode installs
`harness.NewExtensionRouter(h, m.CoreCallbackFunc())`; the core handler is `m`'s
shared core-selection dispatch (harness SPECS §3b.i), so a browser model or
thinking selection applies through the same `apply*` methods the terminal uses.

## Known limitations (documented, not accidental)
- **`assistant_message` covers streamed turns only.** The frame is built from the
  tokens the Bridge accumulated for the turn, because the engine exposes no
  per-agent "message end" hook that hands back the final content (the extension
  host has `EventMessageEnd`, the pool does not). A provider that answers without
  streaming produces no `assistant_message`, so the browser keeps the raw text
  until the transcript is re-fetched; `/api/history` always has the rendered
  `html`, so it is a display-freshness gap, not a data gap.
- **Tool results depend on an extension fronting the call.** See SPECS.md
  §Tool results; the engine has no per-agent completion hook.
- **Stateless core callbacks are handled; the setup flows are not.** Extension
  callbacks (agent focus/kill, transcript rebuild, WASM buttons) dispatch to the
  host, and the stateless core selections (model, provider-model, thinking,
  OpenRouter routing) apply through `Model.CoreCallbackFunc`. The multi-step
  setup flows — local-model base-URL/manual entry, context-window entry,
  OpenRouter catalog, add-model, and the `__wllr:auth` method choice — build
  state across earlier interactions
  (`pendingAuthProvider`, `localSetupModels`, …) and answer `501` because that
  state lives in the interactive `Model` and is not reconstructable from a cold
  callback. See harness NOTES for the classifier and why only the stateless
  selections are shared.
- **Harness-owned imperative widgets are not yet mirrored.** Pickers, the agent
  tree, and modals have stream frames and a basic overlay, but the rich harness
  surfaces (split pickers with previews, autocomplete, the context modal)
  have no shared data model and are not reproduced. The *extension-driven* UI
  path is shared end to end; the *harness-widget* path is not.
- **One process, one session.** The server fronts a single pool. Aggregating
  many wllr instances is a client-side concern (dial N `/api/stream`
  endpoints), deliberately out of this package: instances are separate processes
  with process-global singletons (extension host, config file, session dir).

## The Bridge and the root-agent wiring are two consumers of one stream

**Decision:** `Bridge` attaches its per-agent observers with `Add*`, and serve
mode's root-agent wiring uses a no-op sink. Recorded after a run of bugs that
were all the same bug in different clothes.

**What happened.** The engine's per-agent callbacks were single slots. The
Bridge used them to publish stream frames; the harness's root-agent wiring used
them to give the agent its tools and dispatch extension events. Whichever ran
second silently replaced the other, and serve mode — which needs both — could
therefore have only one. Rather than choose, serve mode originally skipped the
root-agent wiring entirely, and the cost surfaced as three separate user-visible
failures once the browser front end was exercised against a real model turn:

1. the root agent had **no tools** at all (nothing ever called `SetToolsFn`), so
a turn that needed `exec` just narrated instead of acting;
2. the `agents` extension never received `before_agent_start` or `token`, so the
chat transcript showed no user-prompt bubbles and no streamed text — only a
bare assistant bubble rendered at message end;
3. ordering the two wirings the other way did not help: it moved the loss from
the transcript to the stream frames.

**Decision.** Make the agent callbacks additive (`Add*` appends, `Set*`
replaces) so ordering stops mattering, then wire both. Because the Bridge
already publishes the root agent's `token`/`turn_done`/`tool_call` frames, the
root-agent wiring is given a no-op sink and contributes only what the Bridge
cannot: the tool list and the extension-event dispatch.

**Consequence for future consumers.** Any new per-agent observer must use
`Add*`. `Set*` is reserved for exclusive ownership and for re-wiring a freshly
created agent (the automatic-recovery path). A test asserts the additive
property directly (`bridge_additive_test.go`), because the failure mode is
silence, not an error.

## Why `Server` no longer re-exports `SceneModel` and `Addr`

Both were passthroughs to values the package already exposes: the scene graph
through `HubSink` (whose `SceneModel` is what the renderer and tests use), and
the listen address through `Options.Addr` — the caller passes it in, so it can
keep its own copy. Neither had a caller. Two exported methods that only forward
an unexported field are API surface a reader must consider and reject, so they
were deleted; the underlying `sink.SceneModel()` accessor is untouched.
