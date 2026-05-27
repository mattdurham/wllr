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
