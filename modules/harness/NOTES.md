# harness — Design Decisions

Append-only design decision log. Never delete entries; add an `*Addendum (date):*` if a decision is reversed.

---

## 1. cancelStream stored as *context.CancelFunc

*Added: 2026-05-06*

**Decision:** `cancelStream` is typed as `*context.CancelFunc` (a pointer to a cancel function) rather than `context.CancelFunc` (a plain function value).

**Rationale:** Bubbletea calls `Update` on a copy of the `Model` struct (value semantics). If `cancelStream` were stored as a plain `context.CancelFunc`, each `Update` call would operate on a fresh copy of the function value. Setting `cancelStream = &cancel` ensures that even after the model is copied, all copies share the same underlying pointer, so whichever copy's `Update` receives `ctrl+c` or `abortStreamMsg` can dereference the pointer to reach the original cancel function.

**Consequence:** All callers must dereference before invoking: `(*m.cancelStream)()`. A nil check is always required before dereferencing.

*Addendum (2026-05-08):* This pattern was used in the pre-pool era when streaming state lived directly on the model. After the pool-based refactor, `cancelStream` was removed from `Model` entirely. Cancellation now routes through `agentPool.Cancel(mainAgentID)`. This note is retained as historical context for why the pointer pattern was needed.

---

## 2. program stored as *tea.Program

*Added: 2026-05-06*

**Decision:** `m.program` is `*tea.Program`, set via `SetProgram` after the bubbletea program is created but before `prog.Run()`.

**Rationale:** The streaming goroutine launched by the agent pool lives outside the bubbletea event loop and needs to send messages back (`TokenMsg`, `StreamDoneMsg`). `(*tea.Program).Send` is the only thread-safe way to inject messages into the event loop. The pointer is captured as a local variable inside the goroutine to avoid data races on `m.program` itself.

**Consequence:** `SetProgram` must be called before `prog.Run()`. Goroutines must not access `m.program` directly — they use the captured `prog` local.

---

## 3. OnAbort sends abortStreamMsg instead of calling cancelStream directly

*Added: 2026-05-06*

**Decision:** `extHost.OnAbort` sends `abortStreamMsg{}` via `prog.Send` rather than invoking a cancel function directly.

**Rationale:** The `OnAbort` callback is registered in `SetProgram` via a closure over initial state. Because bubbletea replaces the live model on every `Update`, any cancellation function captured at registration time would be stale. By sending `abortStreamMsg{}` through the program, the message is processed by the current live model in `updateActions`, which calls `m.agentPool.Cancel(m.mainAgentID)` with the current (correct) pool reference.

**Consequence:** `abortStreamMsg` is an internal message type. Extensions must only use `OnAbort` to cancel the stream.

---

## 4. History snapshot taken before goroutine launch

*Added: 2026-05-06*

**Decision:** `submitToAgent` takes a snapshot of `m.history` before launching any goroutine and passes `content` directly to `pool.Send`. The agent pool manages its own history.

**Rationale:** History is maintained in two places: `m.history` (harness-level, for event dispatch payloads) and `agent.Agent.history` (agent-level, passed to the LLM). Keeping them separate avoids data races: the harness appends to `m.history` synchronously in the Update loop; the agent manages its own history inside goroutines with its own mutex.

**Consequence:** `m.history` reflects the user's view of the conversation (for extension event payloads). The agent's internal history may diverge after compaction.

---

## 5. addAssistantMsgToHistoryMsg exists as a separate message

*Added: 2026-05-06*

**Decision:** The streaming goroutine sends `addAssistantMsgToHistoryMsg` as a distinct `prog.Send` call before returning `StreamDoneMsg`, rather than bundling the assistant content inside `StreamDoneMsg`.

**Rationale:** The `after_provider_response` extension event is dispatched in `cmdDispatchAfterProviderResponse`, returned as a command from the `StreamDoneMsg` handler. Extensions subscribing to `after_provider_response` reasonably expect `m.history` to already contain the completed assistant turn. By sending `addAssistantMsgToHistoryMsg` first (processed before `StreamDoneMsg` in the single-threaded event loop), history is correct at the time the event fires.

**Consequence:** There is a brief window between processing `addAssistantMsgToHistoryMsg` and `StreamDoneMsg` where `m.streaming` is still `true` but the assistant message is already in `m.history`. This is intentional and harmless.

*Addendum (2026-05-08):* With the pool-based refactor, the streaming goroutine is fully inside `agent.Agent.Submit`. The harness does not launch goroutines itself; `addAssistantMsgToHistoryMsg` is no longer sent from the streaming path. History in the harness (`m.history`) is updated synchronously in `SubmitMsg` (user) and via this message type if a caller sends it. The SPECS.md note about history is updated accordingly.

---

## 6. AltScreen set on View not as ProgramOption

*Added: 2026-05-06*

**Decision:** AltScreen is enabled by setting `v.AltScreen = true` on the `tea.View` returned from `Model.View()`, rather than passing `tea.WithAltScreen()` as a program option at startup.

**Rationale:** The bubbletea v2 API changed how AltScreen is controlled. In v2, the `tea.View` struct carries the AltScreen flag and the renderer honours it on each render cycle. The v1 `tea.WithAltScreen()` program option does not exist in v2. Setting it on `View` is the idiomatic v2 approach.

**Consequence:** AltScreen is re-asserted on every render, which is harmless.

---

## 7. Provider replaced by fantasy.LanguageModel

*Added: 2026-05-06*

**Decision:** The `bob/provider.Provider` interface and the entire `bob/provider/` package are deleted. Streaming uses `fantasy.NewAgent(langModel).Stream(ctx, AgentStreamCall{...})`.

**Rationale:** `charm.land/fantasy` provides Anthropic, OpenAI, and Google providers out of the box, with retry logic, tool call support, and streaming abstractions. Maintaining a custom `Provider` interface and Anthropic implementation duplicates work that fantasy already does correctly.

**Consequence:** `harness.New` now takes `(pool, mainAgentID, h)` instead of `(langModel, provName, h)`. The `bob/provider` directory is permanently deleted.

---

## 8. Tool Wiring: sdkToolAdapter bridges sdk.Tool to fantasy.AgentTool

*Added: 2026-05-06*

**Decision:** Extension-registered tools (`sdk.Tool`) are wrapped by `sdkToolAdapter` which implements `fantasy.AgentTool`. The adapter's `Run` method calls `Host.ExecuteTool`, which dispatches `EventBeforeToolCall` and blocks until the extension calls `tool_result`.

**Rationale:** Fantasy's agent loop handles tool call dispatch, retries, and multi-turn conversations internally. Wiring extension tools as `AgentTool` implementations reuses all of that infrastructure without reimplementing tool call handling in the harness.

**Consequence:** Tools registered by extensions after the last stream started are not visible to the current in-flight stream; they appear on the next stream. This is acceptable because extensions register tools during `_init`, which runs at load time before any stream starts.

---

## 9. Pool-Based Agent Runtime — harness.Model Refactor

*Added: 2026-05-06*

**Decision:** `harness.Model` no longer owns a `fantasy.LanguageModel` or manages streaming state directly. Instead it holds `agentPool *agent.AgentPool` and `mainAgentID string`. Token and completion events arrive via pool callbacks wired in `SetProgram`.

**Rationale:** The `bob/agent` package now manages agent lifecycle, conversation history, and token delivery. The harness should be a thin TUI layer — it owns the chat view, input area, status bar, and extension dispatch, but not the LLM call itself.

**Consequence:** `harness.New` signature changed from `New(langModel, provName, h)` to `New(pool, mainAgentID, h)`. Tests no longer set `m.streaming` directly; they test the synchronous state changes and verify token/done delivery via manual callback invocation.

---

## 10. tokenBatcher: Time-Based, No Goroutines

*Added: 2026-05-08*

**Decision:** The token batcher uses a time-based approach (`lastSend time.Time` compared against `time.Now()`) with a `sync.Mutex` on the buffer. No goroutine or channel is involved.

**Rationale:** A goroutine-based batcher (e.g. a ticker goroutine that drains a channel every 30ms) requires careful lifecycle management — the goroutine must be started and stopped in sync with agent turns. With multiple agents potentially active simultaneously, managing per-agent goroutines adds complexity and risk of goroutine leaks. The time-based approach is simpler: each `onToken` call checks whether enough time has passed and flushes synchronously if so. The `flush()` call at `onDone` drains any tail tokens. No goroutines are created or destroyed per turn.

**Consequence:** Tokens are dispatched from within the fantasy streaming goroutine (via `prog.Send`), so the batcher does not need to be goroutine-safe with itself — only with potential concurrent `flush()` calls, which are protected by the mutex.

---

## 11. renderToolGroup is a No-Op — Tool Calls Hidden from UI

*Added: 2026-05-08*

**Decision:** `renderToolGroup` is defined but contains no rendering logic — it is an intentional no-op. Tool call boxes are completely hidden from the chat UI.

**Rationale:** Early versions showed tool call boxes (pending/done/error indicators with the tool name and a preview of the input). User feedback showed that these boxes created visual noise, distracted from the conversation, and often contained internal implementation details (file paths, JSON payloads) that were not useful to the user. The LLM's text response that follows a tool call already contextualizes what happened in natural language. Hiding tool boxes keeps the UI clean and focused on the conversation.

**Consequence:** `renderToolCall` remains in the code (used by `renderToolGroup` when it existed) but is unreachable. It could be removed in a future cleanup. The `toolOutput` field on `chatMessage` stores the raw tool result for potential future diagnostic use but is never rendered. Tool progress is still visible in the status bar ("working." indicator) but not as individual tool boxes.

---

## 12. lastDoneToolID Retained but Not Used for Token Routing

*Added: 2026-05-08*

**Decision:** `ChatView.lastDoneToolID` is still set by `UpdateToolCall` and reset by `FinalizeMessage`, but `AppendToken` ignores it — all tokens always go to `c.current`.

**Rationale:** In an earlier design, tokens after a tool completion were meant to be routed into the tool box as the "LLM response following the tool". This was implemented but caused tokens to appear fragmented: some text before the tool call was in one assistant box, some text after was in the tool box. When tool boxes were hidden (`renderToolGroup` became a no-op), routing tokens to the tool box meant those tokens were invisible to the user. Routing everything to `c.current` ensures all LLM text is visible in the assistant box, regardless of the tool call interleaving.

**Consequence:** `lastDoneToolID` is currently a vestigial field. It could be removed if the tool box design is not revived.

---

## 13. histContent Cache — Why Historical Messages Only

*Added: 2026-05-08*

**Decision:** `histContent` caches the rendered output of all finalized messages, but never includes `c.current` (the in-progress streaming message).

**Rationale:** During streaming, `AppendToken` is called once per token (potentially thousands of times per response). Rebuilding the full rendered string of all historical messages on every token would be O(n) in message count — for a long conversation this would cause visible lag. By caching all finalized messages in `histContent` and only appending the current message, `refreshContent` is O(1) in message count on the common path. `histContent` is only rebuilt (`histDirty = true`) when a message is finalized, added, or removed — infrequent operations.

**Consequence:** `histContent` must never be treated as the final viewport content — callers must always check `c.current` and append it if non-empty.

---

## 14. Autocomplete: slashWordAt Heuristic

*Added: 2026-05-08*

**Decision:** `slashWordAt` finds the last `/` in the input that (a) is at position 0 or preceded by a space, and (b) has no space between it and the end of the string. This identifies an incomplete command word being typed.

**Rationale:** Users may type `/cmd arg /another` — the second `/` should trigger autocomplete for the second command. Finding the *last* `/` that is word-start ensures the autocomplete tracks the command the user is currently typing, not an earlier slash in the input. The no-trailing-space condition ensures autocomplete only shows while the user is still typing the command name, not after they have added a space (at which point they are typing arguments).

**Consequence:** Autocomplete is not shown when the user types a path like `/usr/local` (because `/local` is preceded by `r`, not a space). This is intentional — path completions are out of scope.

---

## 16. sessionStartDoneMsg — Default Action Prompt Injection

*Added: 2026-05-09*

**Decision:** `cmdDispatchSessionStart` returns `sessionStartDoneMsg` (not `ExtensionEventResultMsg`). When `sessionStartDoneMsg` arrives in `updateExtension`, the harness builds a default action prompt from the current registered tools and commands and appends it to the pool's base system prompt via `buildDefaultActionPrompt`.

**Rationale:** Without a built-in system prompt, the agent has no baseline instruction to use its tools proactively — it defaults to describing what it would do rather than doing it. Pi solves this with a hardcoded system prompt in code. Wllr's equivalent is to inject it dynamically after `session_start` completes, so the list accurately reflects every tool and command registered by extensions during `_init` and `session_start`. Using a distinct message type (not the generic `ExtensionEventResultMsg`) ensures the injection happens exactly once per session, not on every command dispatch result.

**Consequence:** The default action prompt is always appended after whatever AGENTS.md and extension-injected content precede it. Extensions that call `set_system_prompt` (full replace) during `session_start` will have the default prompt appended after their content. Tool and command registrations that happen after `session_start` (e.g. dynamic registration mid-session) are not reflected in the injected prompt — the prompt is a point-in-time snapshot.

---

## 15. dispatchOnCommandMsg — Extension-Registered Commands

*Added: 2026-05-08*

**Decision:** When an extension registers a slash command via `register_command`, the harness creates a `Command` whose handler emits `dispatchOnCommandMsg{Name, Args}`. The `updateActions` handler converts this to a `EventOnCommand` dispatch.

**Rationale:** Extension-registered commands must go through the event bus so the registering extension (and any other subscriber) can handle them. The two-step pattern (command handler → `dispatchOnCommandMsg` → `EventOnCommand`) is needed because the command `Handler` runs synchronously in the bubbletea update loop, but `DispatchEvent` involves async WASM execution. Converting to a message allows `updateActions` to return the dispatch as a `tea.Cmd`, keeping WASM execution off the update-loop hot path.

**Consequence:** Extension command handlers see `EventOnCommand` with `OnCommandPayload{Name, Args}`. The harness does not inspect the command name or args further.

## 17. ConsoleView — Live Subprocess Output Pane

*Added: 2026-05-27*

**Decision:** A new `ConsoleView` component is added to `Model` and rendered as a separate
pane between the chat viewport and the input box when `consoleVisible == true`. It uses a
ring buffer (200 lines) and always shows the most-recent lines (live tail, no scroll).

**Rationale:** NOTES.md §11 ("renderToolGroup is a No-Op") established that tool call boxes
are hidden. This addendum adds a separate display mechanism that does not pollute chat history.
The console pane is ephemeral: it appears during subprocess execution and collapses when the
stream ends. It solves two problems simultaneously: (1) the user can see that the LLM is doing
something concrete (not just "working..."), and (2) ANSI-stripped output still reaches the
LLM while raw output (with colour) is visible to the user.

This decision does NOT reverse §11 — `renderToolGroup` remains a no-op. The console pane is
not a tool call box; it is a live feed from the subprocess, not from the LLM.

**Consequence:** `chatHeight()` now subtracts `consoleHeight()` (9 lines when visible).
Layout math tests must account for the new pane. Console lines do NOT appear in
`m.chat.messages` — they are completely separate.

*Addendum (see also):* NOTES.md §11 is not reversed; `renderToolGroup` remains a no-op.

---

## 18. OnAgentRun Sentinel Prompt — Non-Empty String for Belt-and-Suspenders

*Added: 2026-05-27*

**Decision:** `OnAgentRun` calls `pool.Send(id, "[process pending inbox messages]")` instead
of `pool.Send(id, "")`.

**Rationale:** Fix 1 (inbox append in agent/NOTES.md §16) makes empty-prompt valid when the
inbox is non-empty. But if `OnAgentRun` is ever called when the inbox is empty (edge case —
shouldn't happen in normal flow but possible if extensions call `agent_run` without a prior
`send_message`), empty prompt would still fail. The sentinel is a valid user message, uses
minimal tokens, and clearly communicates intent to the agent.

**Consequence:** The sentinel string appears in the agent's conversation history as a user
message. For long-running agents this adds one extra low-token message per agent wakeup.
Acceptable trade-off for correctness.

---

## 19. agentWakeupMsg — TUI Streaming State for Agent-Triggered Turns

*Added: 2026-05-27*

**Decision:** A new `agentWakeupMsg{}` type is sent from `OnAgentRun` (for the main agent
only) before `pool.Send`. The `updateStream` handler sets `m.streaming=true` and starts the
tick timer.

**Rationale:** `m.streaming` is only set in `submitToAgent` (user-triggered turns). When a
sub-agent calls `send_message` and triggers `agent_run`, the main agent runs a new turn but
`m.streaming` stays false. The TUI shows no "working." indicator, no tick timer fires, and
`StreamDoneMsg` arrives without a matching streaming start (a no-op but misleading).

**Consequence:** The TUI correctly shows the streaming indicator during all main-agent turns,
regardless of whether they were user-triggered or sub-agent-triggered.

---

## 20. m.history Removed — History Lives on AgentPool Only

*Added: 2026-05-27*

**Decision:** The `m.history []sdk.Message` field is removed from `harness.Model` entirely,
along with the dead `addAssistantMsgToHistoryMsg` type and its handler.

**Rationale:** After the pool-based refactor (NOTES.md §9), `m.history` was written in
`submitToAgent` (user messages only) and in the `ResetHistoryMsg` handler, but never read for
any functional purpose. The `addAssistantMsgToHistoryMsg` message was never sent by any code
path (confirmed by grep). Retaining the field creates a misleading partial view of the
conversation that could cause bugs if code is written to rely on it. The canonical history
is `pool.Get(mainID).History()`.

**Consequence:** Callers that need the conversation history must call
`pool.Get(mainID).History()`. The `ResetHistoryMsg` handler now sets agent history via
`pool.SetAgentHistory` and rebuilds the chat from the provided messages, without maintaining
a separate `m.history` copy.

---

## 21. OnMessageEnd / OnUserMessage — Exported Callback Fields for Persistence

*Added: 2026-05-30*

**Decision:** Added two exported function fields to `harness.Model`: `OnMessageEnd func(role, content string)` and `OnUserMessage func(content string)`. Both are nil by default and are set by `cmd/main.go` when session persistence is enabled.

**Rationale:** The harness model is the authoritative source of message lifecycle events (user submission in `submitToAgent`, assistant turn completion in the `StreamDoneMsg` handler). Exposing callbacks on the model struct avoids creating an import dependency from `harness` on the `session` package and keeps each package at its proper abstraction level.

**Consequence:** Any caller that creates a `harness.Model` and wants message-level hooks must set these fields before the program starts. Fields are guarded by nil checks in the model; no panic occurs if they are not set.

---

## 22. Command.Instant — Fast-Path Flag for Built-in Commands

*Added: 2026-05-30*

**Decision:** `Command` gains an `Instant bool` field. All built-in commands (`/clear`, `/reload`, `/model`, `/status`, `/tools`, `/help`) are registered with `Instant: true`. `updateActions` checks this flag first: if `cmd.Instant`, it calls `cmd.Handler(msg.Args)` directly without setting the `"queuing…"` status. The `UIBridge.RegisterCommand` signature is updated to `RegisterCommand(name, desc string, instant bool)` so extension-registered commands can also opt in.

**Rationale:** Built-in commands execute synchronously in the bubbletea update loop and do not touch WASM. Setting `"queuing…"` before them caused a brief flicker of the status indicator for commands like `/clear` that complete in the same frame. The `Instant` flag makes the distinction between "executes immediately in Go" and "routes through WASM dispatch" explicit and testable.

**Consequence:** Any command registered with `instant=true` suppresses the "queuing…" status indicator. For WASM-backed instant commands (e.g. `/agents` from the agents extension), the handler still routes through `dispatchOnCommandMsg` → `EventOnCommand` — the flag only affects the UI status, not the dispatch path.

---

## SceneRenderer — synchronous mutation, async redraw (UI P1)

*Added: 2026-06-29*

**Decision:** Add `SceneRenderer` (scene.go), a goroutine-safe holder of the extension-driven UI scene graph, owned by `Model.scene` and shared by pointer with `harnessUIBridge`. The bridge's `CreateArea`/`PatchUI`/`RemoveArea` mutate the renderer **synchronously** (so `host_call` can return validation errors immediately), then send a payload-free `sceneDirtyMsg{}` to the program purely to trigger a re-render.

**Rationale:** A `host_call` must return a synchronous error (e.g. "area already exists", "node not found"). Routing the mutation as a `tea.Msg` and applying it inside `Update` would make errors asynchronous and unobservable to the caller. Because `SceneRenderer` carries its own `sync.RWMutex`, the bridge can safely mutate it off the bubbletea loop while `View` reads it. The dirty message then leverages bubbletea's "re-render after every message" behavior without duplicating the mutation in `Update`.

**Consequence:** `Model` gains a `scene *SceneRenderer` field (constructed in `New`, passed to the bridge in `SetProgram`). `Update` handles `sceneDirtyMsg` as a redraw-only no-op. `View` calls `renderScenes()` in the normal (non-modal, non-picker) branch. The patch protocol is intentionally clone-validated for atomicity, trading an allocation per batch for the guarantee that a failed batch never corrupts the live tree. P1 keeps View integration minimal (stack below chat); placement-aware compositing and moving the chat into a scene area are deferred to later phases.

*Addendum (2026-07-01):* `sceneDirtyMsg` now carries the mutated area ID. Only `sceneDirtyMsg{Area:"chat"}` refreshes the chat viewport from the scene; statusline and other non-chat scene updates simply trigger a re-render. This avoids recomputing the entire transcript viewport for every statusline timer tick.

*Addendum (2026-07-01):* `sceneDirtyMsg` also marks append-only patches. Append-only chat patches schedule one delayed refresh instead of refreshing the whole transcript immediately for every token batch. Structural chat patches still refresh immediately so user messages, notifications, clear/history restore, and resize remain prompt.

*Addendum (2026-07-01):* Append-only chat refreshes now carry the append target and appended text. If the batch targets the trailing assistant text node, the harness renders only that node and replaces the cached viewport suffix. This keeps streaming updates proportional to the current assistant block instead of the full transcript; mixed-target batches fall back to the full render.

---

## Token batcher EventToken dispatch (UI P2)

*Added: 2026-06-29*

**Decision:** `tokenBatcher` gains an optional `dispatch func(string)` hook. `makeBatchedOnToken` takes a `dispatch` argument; `wireMainAgentCallbacks` builds a closure that marshals `sdk.TokenPayload{AgentID: mainID, Text}` and calls `extHost.DispatchEvent(EventToken)` for each flushed batch. The existing `TokenMsg`→`ChatView` path is unchanged; the two coexist.

**Rationale:** Routing streamed assistant text through WASM (so an extension can render it via the scene graph) requires the text to reach the extension host. Reusing the batcher's existing 30ms coalescing keeps the WASM crossing rate bounded (~33/sec) instead of one dispatch per token. Keeping the direct chat path avoids regressing the main transcript while the WASM-driven rendering path is proven incrementally.

**Consequence:** `makeBatchedOnToken`'s signature changed to `(p, dispatch)`. EventToken dispatch only occurs when an extension host is present. The dispatch executes on the agent's streaming goroutine; `DispatchEvent` is safe there (it does not touch the bubbletea loop).

*Addendum (2026-07-01):* Increase `tokenBatchInterval` from 30ms to 75ms. The profile showed terminal layout/rendering dominating perceived streaming latency, so reducing token-driven render frequency is a better tradeoff than pushing ~33 frames/sec through the viewport.

---

## WASM-driven chat transcript — content/viewport split (UI P4)

*Added: 2026-06-29*

**Decision:** Add an opt-in (`WLLR_WASM_CHAT=1`) mode where the main chat transcript *content* is produced by a WASM extension via the `chat` scene area, while the harness keeps the scrollable viewport. `ChatView` gains an external-content mode (`SetExternalContent`/`externalMode`); `Model.refreshWASMChat` feeds `scene.Render("chat", width)` into the viewport on scene changes and resizes; `renderScenes` skips the `chat` area in this mode.

**Rationale:** The vision ("all text goes through the agents wasm; the Go side is just a bridge") requires the transcript to be produced in WASM. But scrolling and key input are inherently bubbletea/harness concerns and must not cross into WASM. Splitting *content production* (WASM) from *viewport/scroll* (harness) achieves the goal without reimplementing scrolling, sizing, or input in the scene graph. Making it opt-in keeps the primary UI unchanged by default — a risky surface to flip — while letting the full pipeline be exercised and tested (see test/wasmchat). The legacy `ChatView` rendering and the direct token/user wiring are intentionally left intact (ignored in external mode) so the feature is fully reversible and notifications/tool state remain available for a future, fuller migration.

**Consequence:** `Model` gains `wasmChat bool` (from env in `New`) and the `wasmChatAreaID` constant. `ChatView` gains `externalMode`/`externalContent` and `SetExternalContent`. No default behavior changes. Sub-agent text is excluded from the transcript by the extension (it filters on agent ID). A future phase could route notifications through an event and make this the default once validated.

---

## Notifications routed through EventNotify (UI P4)

*Added: 2026-06-29*

**Decision:** Introduce `Model.pushNotification(text)` as the single notification choke point. It calls `ChatView.AddNotification` (legacy rendering) and dispatches `sdk.EventNotify` in a goroutine. All in-Model notification sites (`NotifyMsg` handler, model-change, reload, history-restore, extension errors) now call it.

**Rationale:** In WASM-chat mode the transcript is owned by an extension, so notifications rendered only by `ChatView` would be invisible. A notify event lets the transcript owner render them. The dispatch goroutine avoids blocking the bubbletea loop (mirrors the off-loop token dispatch); the SceneRenderer it ultimately mutates is goroutine-safe. Dispatching regardless of mode keeps the choke point simple and is harmless (subscription-gated).

**Consequence:** Notifications now appear in the WASM-driven transcript (agents extension `OnNotify` → scene patch). Extensions must not call `notify` inside an `OnNotify` handler (infinite dispatch loop). The legacy `session.Renderer.AddNotification` seam (currently unwired in the binary) is unchanged.

---

## WASM-driven chat is now the default (opt-out)

*Added: 2026-06-29*

**Decision:** Flip `Model.wasmChat` to default **on** (`os.Getenv("WLLR_WASM_CHAT") != "0"`) and have the agents extension create/drive the `chat` area unless `WLLR_WASM_CHAT=0`. The legacy `ChatView` rendering path is retained as an automatic fallback.

**Rationale:** With user prompts, streamed assistant text, and notifications (EventNotify) all routed through WASM, the WASM transcript reached parity with the built-in renderer (tool boxes are hidden in both). Making it the default realizes the original goal — the transcript is produced by a WASM component and the Go side is a bridge — while the retained `ChatView` path provides a zero-config safety net: if no extension owns the `chat` area, `refreshWASMChat` no-ops and the internal renderer is used. This means the binary still renders a chat even with all extensions removed.

**Consequence:** Default runs now render the transcript from the agents extension's scene area. `WLLR_WASM_CHAT=0` restores the built-in renderer. Both sides read the same env var with the same default. The retained legacy path adds negligible overhead in WASM mode (`refreshContent` early-returns in external mode, so no histContent is built).

---

## Legacy ChatView renderer removed — WASM transcript is the only path

*Added: 2026-06-29*

**Decision:** Remove the built-in `ChatView` message renderer entirely. `ChatView` becomes a thin viewport wrapper fed via `SetExternalContent`; the transcript is always produced by the WASM extension (`chat` scene area). Removed: `messages`/`queued`/`current`/`histContent`/`histDirty`/`afterTool` state, the `render*` functions, `AppendToken`/`FinalizeMessage`/`AddUserMessage`/`AddQueuedUserMessage`/`UnqueueLastMessage`/`AddNotification`/`Clear`/`MessageCount`, the `chatMessage` type, the `wasmChat` flag and `WLLR_WASM_CHAT` opt-out, and the `externalMode` toggle. Added: `Model.streamContent` (accumulates response text for `OnMessageEnd`/logging) and `Model.resetChatArea` (for `/clear` and history-restore).

**Rationale:** With user prompts, streamed text, and notifications all routed through WASM (EventToken/EventNotify) and the WASM transcript at parity, the dual rendering path was redundant maintenance surface. Removing it commits fully to "the transcript is produced by a WASM component; the harness is a bridge that owns only the viewport." The previous commit retained the legacy path as a safety net; this removes it (restorable from history).

**Consequence:** If the `agents` extension is not loaded, the chat viewport is empty (no fallback). `/clear` and history-restore reset the transcript area to empty rather than re-rendering messages (restored history remains in agent context). The skill `display` echo (compact label instead of raw XML) is no longer applied to the transcript, since `before_agent_start` carries the raw prompt. The per-turn tool log is retained on `ChatView` for `/tools`, cleared at turn start. Rendering tests that asserted on `ChatView` internals were removed; transcript behavior is covered end-to-end by `test/wasmchat`.

*Addendum (2026-07-01):* `ChatView.SetExternalContent` now follows the tail only when the viewport was already at bottom. Streaming transcript refreshes preserve manual scrollback instead of forcing the user back to the newest output.

---

## SceneRenderer gains UpdateArea, ConstrainWidth, ConstrainHeight (step 2 of statusline scene)

*Added: 2026-06-30*

**Decision:** `sceneArea` struct gains four constraint fields (`minHeight`, `maxHeight`, `minWidth`, `maxWidth`), populated from the matching `UIArea` fields at `CreateArea` time and modifiable via the new `UpdateArea(UIUpdateAreaParams) error` method. `ConstrainWidth` and `ConstrainHeight` resolve constraint strings against the current terminal dimension and clamp the render width / line count respectively. A package-private `resolveConstraint` helper parses `"N"` (absolute) and `"N%"` (percent) strings.

**Rationale:** The statusline scene design requires areas that can declare their height and width bounds so the harness compositor can subtract the correct space from the chat viewport without each extension managing its own padding/truncation. Placing the logic in `SceneRenderer` rather than in `Model.View` keeps it testable independently of the bubbletea lifecycle and reusable for future placement zones (sidebar, overlay). `UpdateArea` follows the same partial-update pattern used for `UIPatchOp.Update` on nodes: empty string fields are ignored, allowing callers to change a single constraint without serializing the rest.

**Consequence:** `Model` will call `ConstrainWidth` before `Render` and `ConstrainHeight` after counting newlines in the rendered output (step 4 of the statusline plan). The `harnessUIBridge` will forward `ui_update_area` host calls to `SceneRenderer.UpdateArea` (step 3).

---

## StatusBar removed; statusline is now a scene area (step 4 of statusline scene)

*Added: 2026-06-30*

**Decision:** Remove the `StatusBar` struct from `Model`. All status state moves to `liveState.statuses` (accessed via the new `setStatus`/`getStatus` helpers, which are mutex-guarded like the other `liveState` fields). `renderInputBox` now renders a plain `╭──────╮` top border with no embedded status text. `statusBarHeight = 0` constant removed; replaced by `statusLineHeight()` which dynamically measures the rendered height of all `UIAreaStatus` scene areas. The `statusline` area (`statuslineAreaID`) is pre-created in `New()`. `StatusUpdateMsg` routes to `liveState.setStatus` instead of `StatusBar.Update`.

**Rationale:** This is step 4 of the statusline scene design (docs/plans/2026-06-30-statusline-scene-design.md). Removing `StatusBar` eliminates a parallel state path and commits the harness to the scene-graph-only model. The `liveState` struct already held a mirrored subset of this data for the `get_status_info` host call; consolidating there removes the duplication. The `StatusBar` file (`statusbar.go`) is retained for now because `StatusBar.StatusInfo()` and `StatusBar.defaultLine()` are referenced by `harnessUIBridge.GetStatusInfo()` — this will be cleaned up when the `statusline` extension is rewritten in step 5.

**Consequence:** All test assertions on `m.statusBar.statuses[...]` and `m.statusBar.modelName` are migrated to `m.live.getStatus(...)` and `m.live.model`. The `streamTickMsg` handler no longer updates a status entry — the animated working indicator is now produced entirely by the `statusline` WASM extension responding to `EventToken`/`EventAfterProviderResponse`.

---

## statusbar.go deleted — dead after the statusline scene migration

*Added: 2026-06-30*

**Decision:** Delete `modules/harness/statusbar.go` entirely (the `StatusBar` struct, `NewStatusBar`, `Update`, `Line`/`defaultLine`/`View`, `StatusInfo`, `AddTokens`, `formatElapsed`, and `statusBarStyle`).

**Rationale:** The prior entry ("StatusBar removed; statusline is now a scene area") removed all *uses* of `StatusBar` from `Model` but left the file in place, noting it was kept for `get_status_info`. That turned out to be unnecessary — `harnessUIBridge.GetStatusInfo` builds `sdk.StatusInfo` directly from `liveState`, never from `StatusBar.StatusInfo`. `staticcheck` flagged `formatElapsed` as unused (U1000), confirming the whole file had become dead code: nothing outside `statusbar.go` referenced any of its symbols. Keeping a parallel, unreferenced status type is exactly the kind of drift the spec-driven invariant exists to prevent.

**Consequence:** `staticcheck ./...` is clean (was reporting `formatElapsed is unused`). `get_status_info` is served entirely from `liveState`; there is no longer any `StatusBar` type in the harness. SPECS.md §28 updated to state the struct is removed rather than "retained for get_status_info". No behavior change — the file was unreachable.

---

## Model picker — /model opens a selection modal, persists, and actually switches

*Added: 2026-06-30*

**Decision:** `/model` with no argument now opens the interactive picker (populated from `Model.ModelListFn`) instead of printing a usage notice; selecting an entry switches the active model via `Model.SelectModelFn` and persists it. `/model <name>` still sets directly. A reserved picker callback prefix `"__wllr:"` (constant `modelPickerCallback = "__wllr:model"`) routes picker selections to a core `setModelMsg` handler instead of dispatching `EventOnCommand` to a WASM extension. New `ModelChoice` type + `ModelListFn`/`SelectModelFn` callback fields on `Model`; new `showModelPickerMsg`.

**Rationale:** Two problems: (1) `/model` was cosmetic (updated the status bar, never changed the running model — the before-2 gap); (2) there was no way to *discover* available models. The picker solves both — it lists the provider's catalog (core Go, `cmd/modelcatalog.go`, sourced from charmbracelet Catwalk) and `SelectModelFn` genuinely rebuilds the main agent's LM (`Agent.SetModel`), updates the context window, and saves the choice to `~/.config/wllr/config.json` so it sticks next launch. The picker already existed but was WASM-only (selection → `EventOnCommand`); rather than build a second picker, the reserved-callback prefix lets core-owned pickers reuse the same `PickerView`/`ShowPickerMsg` machinery while routing selection back into the harness. The prefix is reserved so an extension command can't be spoofed into the core path.

**Consequence:** `Model` gains `ModelListFn`/`SelectModelFn` (nil-safe: nil list ⇒ "not available", nil select ⇒ display-only). `updateKeyPressPicker` branches on the `"__wllr:model"` callback. `cmd/main.go` wires the two callbacks: list from `modelsForProvider(cfg.Provider)`, select via `pool.LanguageModelForModel` + `main.SetModel` + `pool.SetDefaultModelName` + `pool.SetContextWindow` + `saveModel`. Model precedence at startup is now env `WLLR_MODEL` > persisted `config.json` `wllr.model` > built-in default. `TestBuiltinModel_NoArgs` updated (now expects `showModelPickerMsg`). Auth/OAuth for providers that need login is deliberately NOT included here — that is the separate Phase B (device-code flow + credentials storage) to be designed next.

---

## Thinking-level picker — /thinking sets the reasoning level, persists, applies at runtime

*Added: 2026-06-30*

**Decision:** Add `/thinking` mirroring the `/model` picker: no-arg opens a picker of reasoning levels (`off/minimal/low/medium/high/xhigh`, current marked), `/thinking <level>` sets directly. Selecting a level calls `Model.SelectThinkingFn`, which sets the main agent's provider options via `Agent.SetProviderOptions` and persists the level. New `ThinkingChoice` type + `ThinkingListFn`/`SelectThinkingFn` callback fields on `Model`; new `showThinkingPickerMsg`/`setThinkingMsg`; reserved picker callback `thinkingPickerCallback = "__wllr:thinking"`; new `SetActiveThinking` to reflect a persisted level at startup. `activeThinking` field tracks the current level for the picker's "(current)" marker and the `think` status key.

**Rationale:** Extended thinking previously existed only as `ThinkingBudget int` on sub-agent spawn (Anthropic-only, no user control for the main agent). Users need to dial reasoning up/down at runtime. pi's design is the reference: a provider-agnostic *level* mapped per-provider (Anthropic thinking budget, OpenAI reasoning_effort, Gemini thinking budget) — the mapping lives in core Go (cmd/thinking.go) because it's tied to the provider integrations, exactly like the model catalog. The picker infrastructure and reserved-callback routing built for `/model` are reused verbatim (second reserved callback), so no new picker machinery. Persisting to config.json (`wllr.thinking`) means the level sticks across restarts, and it's applied to the main agent at startup.

**Consequence:** `Model` gains `ThinkingListFn`/`SelectThinkingFn`/`activeThinking` + `SetActiveThinking`; `updateKeyPressPicker` branches on `"__wllr:thinking"`. `cmd/main.go` wires the callbacks (level list from `thinkingLevels`, apply via `providerOptionsForThinking(provider, level)` + `main.SetProviderOptions` + `saveThinkingLevel`) and applies any persisted level at startup. `savedModel`/`saveModel` were refactored to generic `savedWllrField`/`saveWllrField` helpers so model and thinking share the atomic write. Auth/OAuth remains the separate deferred piece. Covered by harness TestBuiltinThinking_* and cmd TestSaveThinkingLevel_*/TestProviderOptionsForThinking_*.

---

## First-run provider auth prompt — ask once, record, don't ask again

*Added: 2026-06-30*

**Decision:** On the first launch with a provider that has no recorded auth choice, show a one-time picker asking how to authenticate ("Set up OAuth / login" vs "Use an API key"). Record the choice; never ask again for that provider. New `RecordAuthFn` callback + `SetPendingAuthProvider` method on `Model`; new `showAuthPromptMsg`/`recordAuthMsg`; reserved picker callback `authPickerCallback = "__wllr:auth"`; `authPromptProvider`/`pendingAuthProvider` fields. The record lives in a dedicated 0600 auth file (`~/.config/wllr/auth.json`, cmd/auth.go), keyed by provider — presence of an entry is the "don't ask again" gate.

**Rationale:** This is the shape the user asked for ("ask if they need to set up OAuth the first time they choose the provider, keep a record, don't ask again"), aligned with pi's proven pattern (reviewed on pi.dev): a per-provider auth.json where credential presence is the record, rather than a separate boolean flag in config.json. Using a dedicated 0600 file (not the plaintext config.json) keeps auth material out of the general config and matches pi's file mode. The prompt reuses the existing picker + reserved-callback routing built for /model and /thinking — a third reserved callback, no new UI machinery. The actual OAuth token-exchange flow is deliberately still deferred: today the record captures the chosen method, and Anthropic already accepts sk-ant-oat… subscription tokens via ANTHROPIC_API_KEY with the Claude-Code beta headers (cmd/provider.go), so "OAuth" is usable by pasting a token while the live login flow is designed later.

**Consequence:** `Model` gains `RecordAuthFn`/`SetPendingAuthProvider` and the two internal fields; `Init()` emits the prompt when a provider is pending; `updateKeyPressPicker` branches on `"__wllr:auth"`. `cmd/main.go` wires `RecordAuthFn` (→ `saveAuthCredential`) and calls `SetPendingAuthProvider` only when `hasAuthRecord(provider)` is false. cmd/auth.go adds the auth-file store (loadAuthCredential/hasAuthRecord/saveAuthCredential, WLLR_AUTH override for tests, atomic 0600 write). Covered by cmd TestSaveAuthCredential_*/TestHasAuthRecord_*/TestLoadAuthFile_MalformedTolerated and harness TestApplyAuthChoice_*/TestSetPendingAuthProvider_DrivesInitPrompt.

---

## OAuth login flow — /login + first-run OAuth choice drives an interactive PKCE login

*Added: 2026-06-30*

**Decision:** When the user picks "Set up OAuth / login" in the auth prompt, or runs the new `/login` command, the harness drives an interactive OAuth login: `BeginOAuthFn(provider)` returns an authorize URL shown in a modal, the model enters code-capture mode (`oauthCaptureProvider`), and the next submitted line is routed to `CompleteOAuthFn(provider, input)` instead of the agent. New `BeginOAuthFn`/`CompleteOAuthFn` callbacks + `oauthCaptureProvider`/`activeProvider` fields; new `loginMsg`; `/login` builtin command.

**Rationale:** This is the deferred Phase B — the actual token-exchange flow behind the earlier "record the auth method" scaffold. The mechanics mirror pi's Anthropic OAuth (reviewed on pi.dev): authorization-code + PKCE, with `code=true` on the authorize URL so Claude shows a paste-back code — no local callback server, which works over SSH/remote terminals. The harness owns only the UI state machine (show URL → capture pasted code → report result); all provider-specific crypto/endpoints live in core `cmd/` (oauth_anthropic.go, oauthwire.go). Reusing the input line for the pasted code (via a capture flag) avoids building a separate blocking text-prompt widget — the TUI has no such primitive, and the flag cleanly diverts one SubmitMsg. `/login` is available any time (not just first run) so users can re-auth or switch methods.

**Consequence:** `Model` gains `BeginOAuthFn`/`CompleteOAuthFn`/`oauthCaptureProvider`/`activeProvider`; `SubmitMsg` checks capture mode first; `recordAuthMsg` for OAuth chains into `beginOAuthLogin`; `/login` → `loginMsg` → `openAuthPrompt(activeProvider)`. `cmd/main.go` wires the callbacks to an `oauthLoginState` (PKCE verifier held between begin/complete) and applies a stored token at startup (`resolveStartupAnthropicOAuth`, with refresh-on-expiry). Capture mode is single-shot (cleared when completion starts) so a failed exchange returns to normal input. Covered by harness TestBeginOAuthLogin_*/TestCompleteOAuthLogin_*/TestBuiltinLogin_EmitsLoginMsg and cmd oauth/oauthwire tests.

---

## OAuth login: local callback server races manual paste

*Added: 2026-07-01*

**Decision:** After the earlier paste-only OAuth login proved confusing (the browser redirect to `localhost:53692` "failed to load" because nothing listened there), add a local callback server that auto-captures the code. The harness gains `AwaitOAuthFn` (nil-safe); `beginOAuthLogin` returns a `tea.Batch` of the clipboard copy plus, when `AwaitOAuthFn` is set, a command that blocks on the server and yields `oauthCallbackMsg`. `completeOAuthFromCallback` finishes the login when a code arrives and the model is still in capture mode. Manual paste remains as the SSH/remote fallback (the two paths race; whichever fires first wins).

**Rationale:** For local runs (the common case) the callback server makes login seamless — approve in the browser and you're done, no copy/paste. But a callback server can't work over SSH/remote (the user's browser redirects to *their* localhost, not the host's), so paste must stay. Racing them gives the best of both without asking the user which mode they're in. The server lives in cmd/ (oauthwire.go) with all the crypto/endpoints; the harness only knows "await a code" via the callback, preserving the [TUI-decoupling](../decisions/tui-decoupled-behind-renderer.md) boundary.

**Consequence:** `Model` gains `AwaitOAuthFn` + `oauthCallbackMsg` + `completeOAuthFromCallback`; `updateActions` handles `oauthCallbackMsg`. The modal wording now distinguishes same-machine (automatic) from another-machine (paste) and notes the localhost page failing is expected. cmd/oauthwire.go's `oauthLoginState` now owns the http.Server, a buffered code channel, and a done channel; completion claims the verifier under a mutex (single-use exchange) and restores it on failure, and validates state==verifier. Covered by harness TestCompleteOAuthFromCallback_* and cmd TestOAuthLogin_CallbackServerCapturesCode / TestOAuthLogin_StateMismatchRejected.

---

## OAuth login: remove the manual-paste fallback — callback server only

*Added: 2026-07-01*

**Decision:** Remove the manual paste-back path for OAuth login. Login now completes *only* via the local callback server (`AwaitOAuthFn` → `oauthCallbackMsg` → `completeOAuthFromCallback`). `beginOAuthLogin` requires both `BeginOAuthFn` and `AwaitOAuthFn` (else "not available"), and a normal `SubmitMsg` is never repurposed as an OAuth code. Removed `completeOAuthLogin` and the capture-mode branch in the `SubmitMsg` handler; `oauthCaptureProvider` now only guards the callback completion.

**Rationale:** The paste flow was the source of the "localhost failed → now what?" confusion, and with the callback server in place it was redundant for the common (local) case. Requiring the callback server makes the UX single-path and unambiguous: approve in the browser, done. SSH/remote users forward the port (`ssh -L 53692:localhost:53692`) rather than paste — a cleaner story than maintaining two completion paths that race. This is a deliberate reversal of the earlier "paste works over SSH" stance (NOTES entry "local callback server races manual paste").

**Consequence:** `Model` keeps `oauthCaptureProvider` (guards the callback completion + modal) but no longer treats input-line submissions as codes. docs/providers.md documents the port-forward requirement for SSH. Covered by TestBeginOAuthLogin_UnavailableWithoutCallback and the retitled TestCompleteOAuthFromCallback_* tests. *Addendum to the prior "races manual paste" entry: the paste path is gone; the callback server is the sole completion mechanism.*

---

## OAuth: uniform begin/await/complete; add Codex device-code

*Added: 2026-07-01*

**Decision:** Generalize the OAuth harness contract so it supports both Anthropic (browser + local callback) and OpenAI Codex (device-code), and add Codex login. `BeginOAuthFn` now returns `(modalBody, clipboard, err)` — the provider owns its sign-in instructions and what to copy — instead of just an authorize URL. `AwaitOAuthFn` blocks until login resolves and returns whatever `CompleteOAuthFn` needs (Anthropic: the redirect query; Codex: authorization code + server verifier). `beginOAuthLogin` shows `modalBody`, copies `clipboard`, and batches the await command.

**Rationale:** The user wanted Codex OAuth (ChatGPT Plus/Pro is subscription auth, like Claude Pro/Max), and pi's reference implementation shows Codex uses a device-code flow. Device-code is also the clean answer to the SSH problem we'd punted on (port-forwarding) — nothing listens locally, so it works anywhere. Rather than special-case two flows in the harness, the harness stays provider-agnostic: it shows a modal body, copies a URL, and awaits a result. All provider specifics (endpoints, PKCE, device polling, JWT account-id extraction, ChatGPT-backend routing) live in cmd/ (oauth_codex.go, devicecode.go, oauthwire.go dispatch).

**Consequence:** `BeginOAuthFn` signature changed (breaking for the callback; only cmd/main.go wires it). cmd gains: `devicecode.go` (RFC 8628 poll primitive), `oauth_codex.go` (Codex device-code + token exchange + `chatGPTAccountID` JWT decode + `refreshCodexToken`), `newCodexProvider` (OpenAI provider pointed at `chatgpt.com/backend-api/codex` with `chatgpt-account-id` header + Responses API). `authCredential` gains `AccountID`. `oauthLoginState` dispatches begin/await/complete by provider and owns both the callback server and the device state; `resolveStartupOAuth` covers both providers; `LoadConfig` seeds the openai key from a stored Codex token. Covered by cmd TestOAuthLogin_CodexDeviceFlow, TestExchangeCodexCode_*, TestChatGPTAccountID, TestParseFlexibleInt, and the retitled Anthropic/round-trip tests. *Addendum to the "callback server only" entry: the callback server remains Anthropic's mechanism; Codex uses device-code, which is the general SSH-friendly path.*

---

## Blank first-run config opens a provider setup wizard

*Added: 2026-07-01*

**Decision:** Add a blank first-run setup wizard instead of assuming Anthropic. `SetPendingSetupWizard` makes `Init()` emit `showLoginProviderPickerMsg`, which opens a core-owned provider picker (`"__wllr:login_provider"`). Selecting a provider calls `SelectProviderFn`, updates active provider/model state, and starts OAuth only when the selected provider requires it. Cloud choices (ChatGPT/OpenAI and Anthropic) start the existing OAuth flow; local choices skip auth.

**Rationale:** On a completely blank setup there is no meaningful reason to prefer Anthropic over ChatGPT or a local model. The user needs an explicit first decision: ChatGPT, Anthropic, or local. Keeping this as a setup wizard avoids changing `/login` or the explicit first-run auth picker used when the user has configured a provider/model and may intentionally want an API-key flow.

**Consequence:** `Model` gains `ProviderListFn`, `SelectProviderFn`, `ProviderChoice`, `SetPendingSetupWizard`, `showLoginProviderPickerMsg`, and `loginProviderSelectedMsg`. Cloud selections record OAuth and reuse `beginOAuthLogin`; local selections do not record auth and do not call OAuth. Covered by `TestSetPendingSetupWizard_DrivesInitWizard`, `TestLoginProviderSelected_CloudRecordsOAuthAndBeginsLogin`, and `TestLoginProviderSelected_LocalDoesNotBeginLogin`.

---

## Statusline layout uses rendered input height

*Added: 2026-07-01*

**Decision:** Replace the fixed `inputAreaHeight` layout constant with `inputBoxHeight()`, which counts the rendered input box lines. The statusline remains a standalone scene area above the input box.

**Rationale:** Once the statusline built-in was loaded, the extra rendered row exposed that the layout was trusting a hardcoded input height. If textarea rendering changes by a line, the total view can exceed the terminal height and crop the input bottom border. Counting the rendered input box keeps `chatHeight()` aligned with what `View()` actually emits.

**Consequence:** `chatHeight()` and modal sizing use the dynamic input-box height. `TestModel_View_WithStatusLineFitsHeight` covers a small terminal with a one-line statusline and asserts the input bottom border remains visible.

*Addendum (2026-07-01):* View padding now uses `renderedLineCount(out)` instead of raw newline counting, so the final rendered frame is padded to exactly the terminal height rather than occasionally leaving the chat/input composition one row off. `chatHeight()` also reserves a one-line bottom gutter so the input bottom border renders above the terminal's final row instead of being clipped at the screen edge.

---

## Provider/model status emits a lifecycle event

*Added: 2026-07-01*

**Decision:** Seed the harness active model from the spawned main agent during `New()`, and dispatch `EventModelChanged` after startup and after runtime provider/model changes.

**Rationale:** The statusline extension previously read `get_status_info` on `session_start`, but `live.model` was empty at startup even when the main agent had a configured model. Polling on `tick` was also a delayed, indirect way to discover model changes.

**Consequence:** Statusline displays the startup model immediately and updates promptly after `/model` or first-run provider selection. `TestNew_SeedsActiveModelFromMainAgent` covers the startup state.

*Addendum (2026-07-01):* The statusline still uses `EventTick` for the working timer/tokens while a provider is silent. `EventModelChanged` replaces tick polling only for provider/model identity.

*Addendum (2026-07-01):* `TokenMsg` now copies `AgentPool.TokenCount()` into `liveState.tokens` during streaming, not only on `StreamDoneMsg`. The statusline extension reads tokens via `get_status_info`, so delaying the snapshot until turn completion made the statusline appear stuck while the model was actively producing text.

---

## Esc cancels active turns; Ctrl+C exits

*Added: 2026-07-01*

**Decision:** Move active-turn cancellation from `Ctrl+C` to `Esc` in the global key handler. `Ctrl+C` now always returns `tea.Quit`; when `m.streaming` is true, `Esc` calls `AgentPool.CancelAll()` and sets the stream status to `cancelling…`.

**Rationale:** The user expectation for this terminal assistant is that `Esc` stops the current agent turn, especially when a provider hangs or fails locally, while `Ctrl+C` should exit the program consistently. Overlay-specific Esc handling still runs first, so pickers, modals, and autocomplete keep their existing close/cancel behavior.

**Consequence:** A streaming turn is cancelled with `Esc`; exiting is done with `Ctrl+C` or `Ctrl+Q`. The harness spec's key handling section is updated, and `TestModel_Esc_DuringStream_SetsCancellingStatus` / `TestModel_CtrlC_DuringStream_Quits` cover the new contract.

*Addendum (2026-07-02):* Esc cancellation now runs before modal, picker, autocomplete, or input Esc handlers when an active turn exists. The active-turn check also consults `Agent.IsRunning()` across the pool, not only `m.streaming`, so Esc still cancels if the UI streaming flag is stale. Covered by `TestModel_Esc_DuringStream_CancelsBeforeModalClose` and `TestModel_Esc_CancelsRunningAgentWhenStreamingStateIsStale`.

---

## Transient tool activity pane

*Added: 2026-07-01*

**Decision:** Render a compact tool activity pane below the chat viewport while a turn is streaming and at least one tool call is pending. The pane shows the latest three tool call rows from the existing per-turn `toolLog` and hides when all tools finish or the stream ends.

**Rationale:** Tool calls should be visible as they happen without becoming permanent transcript content. Reusing `toolLog` keeps `/tools` and live activity consistent, while keeping the pane separate from `ChatView` preserves the WASM-owned transcript boundary.

**Consequence:** `chatHeight()` subtracts `toolActivityPaneLines` only while the pane is visible. `ToolLogEntry` now stores the tool call ID so completions update the matching call instead of the last pending entry. Covered by `TestChatView_ToolActivityLines_ShowsLastThreeAndMatchesDoneByID`, `TestModel_ToolActivityPane_RendersWhilePendingAndHidesWhenDone`, and `TestModel_ToolActivityPane_HidesOnStreamDone`.

*Addendum (2026-07-01):* The tool activity pane is now persistent in the normal layout rather than transient. It always reserves three content rows plus border between the chat viewport and lower UI, rendering blank rows when no tools are active. This keeps the layout stable and makes tool activity easier to notice when calls are fast. Covered by `TestModel_ToolActivityPane_AlwaysRendersAndShowsRecentTools` and `TestModel_ToolActivityPane_RemainsOnStreamDone`.

*Addendum (2026-07-02):* Tool lifecycle messages now carry `AgentID`, and sub-agent tool-call starts are routed through the same pane as main-agent calls. Non-main rows render with the agent ID, and a completion with no matching start creates a completed row when tool metadata is available. This keeps logs and the live pane coherent when sub-agents are doing work while the main stream remains open.

---

## Agent List Runtime State

*Added: 2026-07-02*

**Decision:** The harness agent bridge populates `extension.AgentInfo.IsRunning` from `Agent.IsRunning()` and `PendingMessages` from `Agent.InboxLen()` when serving `agent_list`.

**Rationale:** Runtime state already lives in the agent package; surfacing it through the bridge lets the bundled agents extension distinguish a working child from an idle child without sending a probe message into the child's inbox.

**Consequence:** `list_agents` and `/agents` can show whether an agent is running and whether messages are queued for its next turn.

---

## 30. Agent List Liveness Fields

*Added: 2026-07-04*

**Decision:** Populate the new `AgentInfo` liveness fields from `Agent.Activity()` in `harnessAgentBridge.List`.

**Rationale:** The harness bridge is the boundary between the runtime agent pool and extension-visible agent tools. Keeping age calculations here avoids leaking wall-clock formatting into the agent package while ensuring `list_agents` reports fresh state every time it is called.

**Consequence:** `/agents` and the agents extension receive recent-activity age, turn duration, active/last tool, and graceful shutdown-request state. The data is read-only and does not wake, cancel, or message agents.

---

## LSP guidance in default action prompt

*Added: 2026-07-02*

**Decision:** Add a conditional `Code Intelligence` section to `buildDefaultActionPrompt` whenever primary LSP tools such as `lsp_diagnostics`, `lsp_lint`, `lsp_definition`, or `lsp_references` are registered.

**Rationale:** The model already receives tool schemas, but the default prompt only named available tools. Agents were not reliably choosing code-intelligence tools because nothing explained where they fit into an agentic coding workflow. The new guidance makes diagnostics, linting, navigation, references, and refactor previews first-class tool uses, and points capability checks at backend and output-contract discovery.

**Consequence:** Sessions with the LSP extension loaded include stronger system guidance for the primary LSP tools without adding per-tool descriptions for every registered tool. Covered by `TestBuildDefaultActionPrompt_IncludesLSPGuidance` and `TestBuildDefaultActionPrompt_OmitsLSPGuidanceWithoutDiagnosticsTool`.
