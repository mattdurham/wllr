# modules/server — TESTS

Tests live beside the code and use `httptest` (no socket) plus the real
`agent.AgentPool` driven by `modules/testutil.FakeLM`, so the streaming path is
exercised end to end rather than mocked.

## hub_test.go

| Test | Intent |
|------|--------|
| `TestHubPublishAssignsSequence` | `Seq` starts at 1 and increments; `LastSeq` tracks it |
| `TestHubFansOutToAllSubscribers` | Two subscribers both receive a published event |
| `TestHubReplaysBacklogAfterSince` | `Subscribe(since)` replays exactly the events with `Seq > since`, in order |
| `TestHubBacklogIsBounded` | The replay buffer keeps only the newest `backlogCap` events |
| `TestHubDropWhenSubscriberFull` | Publishing past a full subscriber buffer drops rather than blocks, and the drop is counted |
| `TestHubCancelUnsubscribes` | `cancel` unregisters the subscriber and is idempotent; the delivery channel is deliberately left open so a publish racing the snapshot cannot send on a closed channel |
| `TestHubPublishConcurrentWithCancel` | **`-race`:** publishing from one goroutine while another subscribes/cancels never sends on a closed channel (no panic) |
| `TestHubSubscribeAfterPublishReplaysAll` | A late subscriber with `since=0` replays the whole retained backlog |

## server_test.go

`newTestPool` spawns a root agent on a `FakeLM` that answers every turn with
`"hello world"` streamed word by word. `collectUntilTurnDone` drains the hub
until `turn_done` or a 10s deadline.

| Test | Intent |
|------|--------|
| `TestServerStreamsTokensAndTurnDone` | A submit produces `user_message`, `turn_started`, incremental `token` frames spelling the response, and a final `turn_done`; `Seq` strictly increases |
| `TestServerStreamsRenderedAssistantMessage` | A completed turn emits an `assistant_message` frame carrying the raw text and its rendered HTML, **before** `turn_done`, with no raw escapes in the HTML |
| `TestServerHistoryReflectsCompletedTurn` | After a turn, `GET /api/history` returns the user + assistant messages |
| `TestServerAgentsListsRoot` | `GET /api/agents` lists the root agent with `main=true` |
| `TestServerSubmitRejectsEmptyContent` | Whitespace-only content → `400` |
| `TestServerSubmitRejectsUnknownAgent` | Unknown agent → `404` |
| `TestServerSubmitStartsTurn` | Valid submit → `202` and events follow |
| `TestServerCancelUnknownAgent` | Cancel of an unknown agent → `404` |
| `TestServerAuthRequired` | With a token, requests without it → `401`; `Bearer` header and `?token=` both → `200` |
| `TestServerStreamReplaysBacklogFromSince` | `GET /api/stream?since=` emits a `ready` frame then replays the missed events (the `Flush`-forwarding regression guard) |
| `TestServerInboxHandlersRequirePool` | `GET /api/inbox`, `POST /api/inbox/clear`, and `POST /api/inbox/edit` answer `503` (not a nil-pool panic) when the server has no pool |

## context_test.go

The `/context` breakdown endpoint and its engine-to-wire flattening.

| Test | Intent |
|------|--------|
| `TestHandleContextReturnsBreakdown` | `GET /api/context` returns the root agent's breakdown; `estimated_total` equals the engine's own `Sum()`, and the last request is empty before a turn |
| `TestHandleContextUnknownAgent` | Unknown `?agent=` → `404` |
| `TestHandleContextRejectsNonGet` | `POST /api/context` → `405` |
| `TestBuildContextViewMapsFields` | The flattening carries system components (chars/4), tools by tool, largest messages, and the canonical transcript through to the view |

## hubsink_test.go

The HTTP implementation of the renderer-neutral `sdk.UISink` (`HubSink`).
Presentational methods are asserted by subscribing to a hub and reading the
frame each publishes; streaming intents map onto the same event kinds a chat
client consumes; `Submit` is asserted against a real `AgentPool`. The
engine-mutating half of the extension contract (base prompt, history reset,
tool-call bookkeeping) now lives in the harness `harnessUIBridge` and is covered
by `modules/harness/bridges_sink_test.go`.

| Test | Intent |
|------|--------|
| `TestHubSinkPublishesPresentationalFrames` | `Notify`/`ShowModal`/`ShowPicker`/`ShowAgentTree`/`ShowTextInput`/`Abort`/`Console` each emit the matching kind and payload |
| `TestHubSinkStreamingFrames` | `Token`/`ToolCallStart`/`ToolCallDone`/`StreamDone` emit `token`/`tool_call`/`tool_result`/`turn_done`; `AgentWakeup` emits nothing |
| `TestHubSinkSceneGraphFrames` | `SceneCreate`/`ScenePatch`/`SceneUpdate`/`SceneRemove` apply to the `SceneModel` and emit the structural frame plus the matching `ui_area_html`; missing/unknown id is an error |
| `TestHubSinkScenePatchAppendFastPath` | An append-only batch emits one `ui_text_append` per op and no full-area re-render, while the model still reflects the appended text |
| `TestHubSinkSceneRemoveThenPatchErrors` | A patch after removal fails instead of silently recreating the area |
| `TestHubSinkStatusAndFocus` | `SetStatus` round-trips through `StatusInfo`; `FocusAgent` emits `focus` and changes the default `Submit` target |
| `TestHubSinkResetHistoryPublishes` | `ResetHistory` emits `reset_history` with the replacement messages |
| `TestHubSinkSubmitTargetsFocus` | `Submit` starts a turn on the focused agent (observed as a `user_message` attributed to `main/coder`) |
| `TestHubSinkSatisfiesSinkContract` | The sink is assignable to `sdk.UISink` |

## htmlscene_test.go

The `sdk.UINode` → HTML renderer.

| Test | Intent |
|------|--------|
| `TestRenderAreaHTML_TextEscapingAndStructure` | Area id/placement/node id/text span are present; message text is escaped (`&lt;script&gt;`, `&amp;`), never live markup |
| `TestRenderAreaHTML_AllNodeTypes` | All six node types produce their class, and an unknown type degrades to an empty box rather than erroring |
| `TestRenderNodeHTML_PropsToCSS` | Every prop maps to its CSS declaration (colour tokens, bold/italic/underline/faint, border/radius, align, `fill` width, numeric height, padding/margin shorthand, wrap) |
| `TestRenderNodeHTML_NumericWidthAndHexColor` | Numeric width → `ch`; a raw hex colour passes through |
| `TestRenderNodeHTML_UnknownThemeTokenOmitted` | An unresolvable token emits no colour rather than a broken declaration |
| `TestRenderNodeHTML_CollapsesBlankLineRuns` | A text node's body is run through the shared `sdk.CollapseBlankLineRuns`, so the browser collapses blank-line runs exactly as the terminal renderer does |
| `TestRenderAreaHTML_Constraints` | Area `Min/MaxHeight`, `Min/MaxWidth`, and `Weight` translate to CSS (`1lh`, `25%`, `40ch`, `80%`, `flex-grow`) |

## scenemodel_test.go

The TUI-free scene holder, exercising the shared `sdk` graph ops.

| Test | Intent |
|------|--------|
| `TestSceneModel_CreateDuplicateRemove` | Empty/duplicate ids error; removing a missing area is a no-op |
| `TestSceneModel_MatchesHarnessPatchSemantics` | The behaviours `modules/harness/scene_test.go` pins — set_root, streaming append, batch atomicity, insert-at-index ordering, replace_text, append-on-non-text error — hold identically here |
| `TestSceneModel_UpdateArea` | Updates merge (do not replace) and error on empty/unknown id |
| `TestSceneModel_SnapshotOrderAndEmptyRoot` | Snapshot follows creation order and renders a region for a registered-but-empty area |
| `TestIsTextAppendOnly` | Empty and mixed batches are not append-only; an all-append batch is |

## client_test.go

The HTTP surface for the HTML front end.

| Test | Intent |
|------|--------|
| `TestHandleClientServesPageWithProtocol` | `GET /` returns the HTML page, contains the API hooks (stream, submit, history, scene, models, context, callback, login), substitutes the protocol placeholder, renders message bodies from the server-provided `html`, and never interpolates raw message text into markup |
| `TestHandleClientHasQueueAndToolPanes` | The page carries the queued-messages and tool-activity panes (containers, clear button, and driver functions over `/api/inbox`, `/api/inbox/clear`, and the `tool_call`/`tool_result` frames), and the queued pane never interpolates message text into markup |
| `TestHandleClientHasCommandAutocomplete` | The page carries the slash-command autocomplete (the `#suggest` dropdown, `/api/commands`, the shared `slashWordAt` rule, and the dispatch that routes `/model`, `/thinking`, `/openrouter-speed` and extension commands through the callback endpoint) |
| `TestHandleClientScriptParses` | The embedded `<script>` body passes `node --check` (Go cannot compile-check the page's JavaScript); skips when node is unavailable |
| `TestHandleClientHasInstanceSelector` | The page has the instance selector and `api()` routing: `switchInstance`/`resetView`/`state.self`, `EventSource(api("api/stream"))`, exactly one `new EventSource(`, no unprefixed per-instance call, and every per-instance reset marker in `resetView` |
| `TestHandleClientRejectsUnknownPath` | Only `/` serves the page; other non-API paths → `404` |
| `TestHandleSceneSnapshot` | `GET /api/scene` returns areas in order with rendered HTML and the protocol version |
| `TestHandleSceneRejectsNonGet` | `POST /api/scene` → `405` |
| `TestSceneFramesReachStream` | An extension scene call publishes `ui_area` then `ui_area_html` on the hub a browser subscribes to |

## ansihtml_test.go

The ANSI → HTML converter (`ANSItoHTML`), the single place a styled message
becomes markup — so escaping is part of every case here.

| Test | Intent |
|------|--------|
| `TestANSItoHTMLEscapesPlainText` | Unstyled markup is escaped |
| `TestANSItoHTMLEscapesInsideStyledRun` | Text inside a styled run is escaped too (escaping is not skipped on that path) |
| `TestANSItoHTMLAttributes` | bold/italic/underline/faint map to their CSS declarations and keep the text |
| `TestANSItoHTMLColors` | 16-colour, bright, 256-colour, and truecolor fg/bg map to colours |
| `TestANSItoHTMLResetClosesSpan` | A reset closes the styled run instead of bleeding colour |
| `TestANSItoHTMLNonSGRAndOSC` | Non-SGR and OSC sequences are dropped, visible text survives, a truncated escape does not panic |
| `TestANSItoHTMLMultiParam` | A combined sequence and the empty-parameter reset form both apply |
| `TestANSItoHTMLNewlinesPreserved` | Newlines survive for `white-space:pre` |

## mdhtml_test.go

`MessageHTML` — the message-body pipeline. The cross-cutting property is that no
input yields a raw control byte.

| Test | Intent |
|------|--------|
| `TestMessageHTMLUserVerbatimEscaped` | User content is escaped, not markdown-rendered (prompts show as typed) |
| `TestMessageHTMLAssistantRendersBold` | Assistant `**bold**` becomes bold styling via `md.Render` |
| `TestMessageHTMLAssistantHeaderStyled` | A markdown header renders bold in the header colour |
| `TestMessageHTMLAssistantCodeBlockStyled` | A fenced code block renders with a background and no raw escapes |
| `TestMessageHTMLAssistantPlainPassthrough` | Plain prose is returned unchanged |
| `TestMessageHTMLEmpty` | An empty body renders empty |
| `TestMessageHTMLNeverEmitsRawEscapes` | No input (markdown, code, tables, quotes, literal escapes) produces a control byte |

## commands_test.go

Command discovery for the browser front end.

| Test | Intent |
|------|--------|
| `TestHandleCommandsListsRegistered` | `GET /api/commands` returns the installed commands with descriptions and the instant flag, and the `/` prefix |
| `TestHandleCommandsNilLister` | A nil lister yields an empty list, not an error |
| `TestHandleCommandsRejectsNonGet` | `POST /api/commands` → `405` |
| `TestHubSinkRegisterCommandForwards` | An extension command registered after serve-mode wiring reaches the installed registrar (the harness registry) |
| `TestHubSinkRegisterCommandNoRegistrar` | Registration without a registrar is a silent success, so it cannot fail an extension call |

## models_test.go

`GET /api/models` and the model-selection round trip. The point is that the key
and callback the server hands out are the ones the harness decodes, so a browser
selection is not a silent no-op.

| Test | Intent |
|------|--------|
| `TestHandleModelsListsChoices` | The installed list is returned with provider-qualified keys that split back to the right provider/model, tiers, the `active` flag, and the shared callback id |
| `TestHandleModelsNilLister` | No lister installed → an empty list, `200`, not an error |
| `TestHandleModelsRejectsNonGet` | `POST /api/models` → `405` |
| `TestModelSelectionRoundTrip` | The key served by `/api/models`, POSTed to `/api/ui/callback`, reaches the core handler and splits back to `(provider, model)` |

## uicallback_test.go

The `POST /api/ui/callback` handler: forwarding and status mapping. The
router is a recording double so the test asserts exactly what the handler
passed, not what a real engine did with it.

| Test | Intent |
|------|--------|
| `TestUICallback_NoRouterIsNotImplemented` | No router installed → `501` |
| `TestUICallback_RoutesValueAsSingleArg` | `value` becomes a one-element `args` and reaches the router |
| `TestUICallback_ExplicitArgsOverrideValue` | An explicit `args` array is used verbatim |
| `TestUICallback_RequiresCallback` | Missing `callback` → `400` |
| `TestUICallback_BadJSON` | Malformed body → `400` |
| `TestUICallback_MethodNotAllowed` | `GET` → `405` |
| `TestUICallback_UnsupportedSentinelsAre501` | `ErrNoCoreHandler`/`ErrNoExtensionHost` → `501`, not `500` |
| `TestUICallback_OtherErrorIs500` | Any other router error → `500` |

## instances_test.go

The `GET /api/instances` contract and the peer health probe, driven against a
real `httptest` peer (a real TCP `/api/health`) through the fully wrapped handler.

| Test | Intent |
|------|--------|
| `TestHandleInstancesDefaultsSelfID` | No peers → `200`, `self == DefaultSelfID`, exactly one entry (`self:true`, `status:"ok"`, `protocol:ProtocolVersion`) |
| `TestHandleInstancesRejectsNonGet` | `POST /api/instances` → `405`, matching every other handler's method guard |
| `TestHandleInstancesListsSelfAndPeers` | Self first then the peer with the health it reported (`protocol`/`last_seq`); the probe sent `Authorization: Bearer <peerToken>` (client SPECS invariant 9); the body contains neither the peer URL nor its token (invariant 12) |
| `TestHandleInstancesReportsPeerDown` | A closed peer still yields `200`; that entry is `status:"down"` with a non-empty `error`, and a healthy peer in the same request is unaffected (failure isolation) |
| `TestHandleInstancesErrorOmitsPeerTopology` | A down peer's `error` is the sanitized class (`unreachable`) and the body contains no `http://`, peer host, or peer port — the raw `net/http` error must not leak (invariant 12) |
| `TestProbeCauseClassifiesWithoutTopology` | `probeCause` maps a connection-refused `url.Error` → `unreachable` and a (wrapped) `context.DeadlineExceeded` → `timeout`, emitting no host/port/`http` in either case |
| `TestProbeOnceReportsNon200Status` | A peer answering `401` is `down` with `error == "http 401"` — a status code is safe to show |
| `TestProbeRejectsNonWllr200` | A `200` from a non-wllr service (`{"hello":"world"}`, no `ok`/`protocol`) is `down` with `error == "malformed response"`, not healthy |
| `TestProbeCanceledRequestDoesNotCacheDown` | A cancelled probe attempt is indeterminate and is not cached as `down`; a cancelled *request* context does not cancel a probe against a healthy peer (the probe runs on a request-independent base context) |
| `TestHandleInstancesCachesPeerProbe` | Two sequential polls hit the peer's `/api/health` exactly once (the `probeTTL` cache) |
| `TestHandleInstancesSkipsInvalidPeers` | Empty id, an id outside the routable charset (`bad/id`, `has space`), duplicate id, self-collision, and an unparseable URL are dropped (`buildPeers`); only `self` + one peer remain, and a dropped/self id is not proxiable (`404`) |

## proxy_test.go

The reverse proxy's rewrite, credential handling, body/stream forwarding, and
fault behaviour. The peer is a real `httptest` server that records what it
received, so the assertions are on the forwarded request, not a mock.

| Test | Intent |
|------|--------|
| `TestPeerProxyForwardsAPIPathQueryAndToken` | Prefix stripped (`/api/instances/ci/api/history` → `/api/history`), query preserved, inbound `Authorization` replaced by the peer token |
| `TestPeerProxyForwardsPathToPeerBase` | A peer `BaseURL` carrying a path (`http://host/wllr`) still receives `/wllr/api/history`: the prefix is stripped *before* the base path is joined (the probe/proxy parity L2) |
| `TestPeerProxyDropsOriginTokenQuery` | An inbound `?token=` is deleted before forwarding and every other parameter survives (invariant 12) |
| `TestPeerProxyForwardsPostBody` | A `POST` body is forwarded verbatim and the peer's response returns unchanged |
| `TestPeerProxyStreamsSSE` | Two peer SSE frames arrive and the response is flushed (`rec.Flushed`) — the buffering regression guard |
| `TestPeerProxyUnknownInstance404` | An unknown id and the self id both `404`; the peer sees zero requests (invariant 13) |
| `TestPeerProxyRejectsNonAPIPath` | `/api/instances/ci/secret` and `/api/instances/ci/` `404` with zero requests at the peer — not an open proxy (invariant 13) |
| `TestPeerProxyReturns502WhenPeerDown` | A closed peer → `502` with `instance unreachable` (failure isolation) |
| `TestPeerProxyReturns502WhenPeerStalls` | A peer that accepts the connection but never writes response headers → prompt `502` (the shared transport's `ResponseHeaderTimeout`), not an indefinite hang |
| `TestPeerProxyStripsPeerLocationOrigin` | A 3xx peer `Location` is reduced to a same-origin absolute path: an absolute `http://evil.example/x?y=1` → `/x?y=1`, a protocol-relative `//evil.example/x` → `/x`, backslash variants and an opaque `javascript:` → a plain path, and an already-relative `/relative?q=1` is left unchanged (invariant 12) |
| `TestPeerProxyStripsPeerSetCookie` | A peer `Set-Cookie` is not forwarded to the browser, so a peer cannot plant origin-scoped state (invariant 12) |
| `TestPeerProxyLogsFailure` | A proxy failure is logged with the peer id (the browser body stays generic) |
| `TestPeerAPIURLJoinsBasePath` | The probe helper resolves a peer `BaseURL` with a path (`http://host/wllr`) or trailing slash to its effective URL (L2); see `TestPeerProxyForwardsPathToPeerBase` for the proxy half |
| `TestServerAuthCoversInstanceRoutes` | With a token configured, both new routes are `401` without it (server SPECS invariant 6) |

## Headless bridge installation

`cmd/serve.go` installs the harness bridges via
`harness.InstallHeadlessBridges(h, pool, mainID, logFn, srv.HubSink())`, then
wires the root agent with `m.WireMainAgentCallbacks(&sdk.FuncSink{})` and points
`srv.SetMessageEndNotifier` at `m.DispatchMessageEnd`.
`TestInstallHeadlessBridgesEnablesSpawner` (in `modules/harness`) asserts the host
now carries a real, spawner-backed `AgentBridge` rather than the `earlyAgentBridge`
stub, so `create_agent` is attempted for real in serve mode.

### bridge_additive_test.go (observers must not clobber each other)

| Priority | Test | Scenario | Assertions |
|---|---|---|---|
| High | `TestBridgeAttachesAdditively` | a pre-existing `SetOnToken` observer, then `Bridge.Start()`, real turn | the Bridge publishes `token` frames AND the pre-existing observer still fires |
| High | `TestBridgeMessageEndNotifierDeliversFinalizedText` | notifier installed, real turn | the notifier receives role `assistant` and non-empty finalized content |

## Manual / end-to-end

`wllr serve 127.0.0.1:PORT` is verified against a built binary: it binds, logs
`api server listening`, serves the HTML client at `/` (17011 bytes, `EventSource`
present, protocol placeholder substituted), answers `/api/health` and
`/api/scene`, returns `404` for an unknown path, and shuts down cleanly.

Multi-instance switching is verified live against two built binaries on
different ports (`wllr serve 127.0.0.1:8765 -instance-id local -peer
dev=http://127.0.0.1:8766` plus a plain `wllr serve 127.0.0.1:8766`):

- `GET /api/instances` lists `self` then `dev`, both `status:"ok"`,
  `protocol:1`; `grep 8766` over the body finds nothing (no URL/token leak).
- `/api/instances/dev/api/health` returns the peer's health through the proxy;
  `/api/instances/dev/api/stream` streams the peer's `ready` and `ui_area`
  frames.
- `/api/instances/dev/secret` and an unknown id both `404`; a third peer pointed
  at a closed port reports `status:"down"` with the sanitized cause
  (`error:"unreachable"`, no host/port in the body), and its proxy answers `502`.
- In a browser the selector lists `local` and `dev`; switching swaps transcript,
  scene areas, statuses, queue, tools, usage, and focus; `?instance=dev`
  survives a reload; and stopping `dev` shows `dev (down)` with a disabled
  composer without disturbing `local`.

The extension-driven scene path is verified live against a **real model turn**
with the bundled extensions built (`make builtins`): on session start
`/api/scene` reports the `chat` area the `agents` extension created, and after a
turn it holds the user-prompt box (`u1`, success colour) followed by the
assistant box (`a1`, accent colour) whose content is the markdown rendered to
ANSI and converted to styled HTML spans (bold heading, code-block background,
bold text) — the same transcript the terminal renders. The stream carries
`ui_text_append` for the incremental streaming path. A tool-requiring turn
produces a `tool_call` frame for `exec` and the model reports the real output,
confirming the root agent has its tool list in serve mode.

The callback endpoint was verified live the same way: an extension callback
(`{"callback":"agents:focus","value":"main"}`) returns
`200 {"callback":"agents:focus","routed":true}`; the stateless core callbacks
(`__wllr:model`, `__wllr:thinking`) return `200 ... routed:true` now that serve
mode installs `m.CoreCallbackFunc()`; a stateful core flow (`__wllr:auth`)
returns
`501`; and a body with no `callback` returns `400`.

Core selection dispatch itself is unit-tested in `modules/harness`
(`coredispatch_test.go`): the classifier mapping and that `ResolveCoreCallback`
reaches the same `apply*` methods the terminal uses, including the cross-provider
route for a provider-qualified picker key.

Extension UI frames could not be observed against the built binary in this
worktree because the built-in WASM extensions (`agents`, `statusline`) are not
built here (`make extensions`); the bridge is covered by `ui_test.go` instead.
On a machine with the builtins present, a fresh `/api/stream` shows `ui_area` /
`status` frames from the `statusline` extension at `session_start`.

Model selection was verified live: `GET /api/models` returned the real
OpenRouter list the terminal picker shows — provider-qualified keys
(`openrouter\u001fz-ai/glm-5.3-flash`), the `active` flag on the live model, and
`"callback":"__wllr:model"` — and the page contained the picker
(`openModelPicker`).

Markdown rendering was verified end-to-end with a real model turn: submitting a
request for a heading, a fenced Go code block, and bold text produced
`GET /api/history` assistant `html` of a heading span
(`color:#89cff0;font-weight:700;`), a code-block span with
`background-color:#0f0f0f;`, and a bold `done` span — the markdown rendered, and
no raw escape bytes in the output.

## Not yet covered

- SSE against a real browser `EventSource` (covered by curl today).
- Scene frames produced by a live built-in extension (the builtins are absent in
  this worktree; covered by `hubsink_test.go` and `client_test.go` instead).
- Interactive callback dispatch for **extension** callbacks (live coverage is
  curl-verified; a browser round-trip is not exercised).
- Core callback dispatch beyond the stateless selections: the setup flows
  (local-model, context-window, catalog, and the picker-based `__wllr:auth`
  method choice) answer `501` — see NOTES.md.
- The `chat`-area transcript path in a browser: the builtins are absent here, so
  the scene-driven transcript and its ANSI rendering are covered by
  `ansihtml_test.go`/`htmlscene_test.go` rather than a live browser session.
- `assistant_message` for a non-streaming provider (the frame is built from
  streamed tokens; see NOTES.md).
