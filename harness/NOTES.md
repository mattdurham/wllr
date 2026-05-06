# harness — Design Decisions

## 1. cancelStream stored as *context.CancelFunc

*Added: 2026-05-06*

**Decision:** `cancelStream` is typed as `*context.CancelFunc` (a pointer to a cancel function) rather than `context.CancelFunc` (a plain function value).

**Rationale:** Bubbletea calls `Update` on a copy of the `Model` struct (value semantics). If `cancelStream` were stored as a plain `context.CancelFunc`, each `Update` call would operate on a fresh copy of the function value. Setting `cancelStream = &cancel` ensures that even after the model is copied, all copies share the same underlying pointer, so whichever copy's `Update` receives `ctrl+c` or `abortStreamMsg` can dereference the pointer to reach the original cancel function.

**Consequence:** All callers must dereference before invoking: `(*m.cancelStream)()`. A nil check is always required before dereferencing.

---

## 2. program stored as *tea.Program

*Added: 2026-05-06*

**Decision:** `m.program` is `*tea.Program`, set via `SetProgram` after the bubbletea program is created but before `prog.Run()`.

**Rationale:** The streaming goroutine launched by `startStream` lives outside the bubbletea event loop and needs to send messages back (`TokenMsg`, `addAssistantMsgToHistoryMsg`, `StreamDoneMsg`). `(*tea.Program).Send` is the only thread-safe way to inject messages into the event loop. The pointer is captured as a local variable inside `startStream` to avoid data races on `m.program` itself.

**Consequence:** `SetProgram` must be called before `prog.Run()`. Goroutines must not access `m.program` directly — they use the captured `prog` local.

---

## 3. OnAbort sends abortStreamMsg instead of calling cancelStream directly

*Added: 2026-05-06*

**Decision:** `extHost.OnAbort` sends `abortStreamMsg{}` via `prog.Send` rather than invoking `cancelStream` directly.

**Rationale:** The `OnAbort` callback is registered in `SetProgram` via a closure over the `*Model` receiver. However, bubbletea continually replaces the live model with the return value of `Update` — the `*Model` captured in the closure is the initial address and is never updated. If `OnAbort` tried to call `m.cancelStream` through that stale pointer, it would always read the original (nil) value. By instead sending `abortStreamMsg{}` through the program, the message is processed by the current live model in `Update`, which has the correct non-nil `cancelStream`.

**Consequence:** `abortStreamMsg` is an internal message type. External callers (extensions) must use the `OnAbort` callback and must never attempt to cancel the stream directly.

---

## 4. History snapshot taken in startStream

*Added: 2026-05-06*

**Decision:** `startStream` takes an immutable copy of `m.history` *excluding* the current user message before launching the goroutine: `priorHistory := append([]sdk.Message(nil), m.history[:len(m.history)-1]...)`. The current user message is passed separately as `Prompt: content` to `AgentStreamCall`; only prior turns go in `Messages`.

**Rationale:** The goroutine passes `priorHistory` to the fantasy agent as the conversation context (`Messages`), while the current user turn is the `Prompt`. This matches the fantasy API contract. If the goroutine referenced `m.history` directly, a subsequent `clearMsg` processed on the main loop could mutate the slice while the goroutine is reading it, causing a data race. The snapshot is safe to read from the goroutine without synchronization.

**Consequence:** Messages submitted after the stream starts are not included in that stream's context. This is the correct and expected behaviour.

---

## 5. addAssistantMsgToHistoryMsg exists as a separate message

*Added: 2026-05-06*

**Decision:** The streaming goroutine sends `addAssistantMsgToHistoryMsg` as a distinct `prog.Send` call *before* returning `StreamDoneMsg`, rather than bundling the assistant content inside `StreamDoneMsg`.

**Rationale:** The `after_provider_response` extension event is dispatched in `cmdDispatchAfterProviderResponse`, which is returned as a command from the `StreamDoneMsg` handler. Extensions subscribing to `after_provider_response` reasonably expect `m.history` to already contain the completed assistant turn. By sending `addAssistantMsgToHistoryMsg` first (processed before `StreamDoneMsg` due to the single-threaded event loop), history is correct at the time the event fires.

**Consequence:** There is a brief window between processing `addAssistantMsgToHistoryMsg` and `StreamDoneMsg` where `m.streaming` is still `true` but the assistant message is already in `m.history`. This is intentional and harmless.

---

## 6. AltScreen set on View not as ProgramOption

*Added: 2026-05-06*

**Decision:** AltScreen is enabled by setting `v.AltScreen = true` on the `tea.View` returned from `Model.View()`, rather than passing `tea.WithAltScreen()` as a program option at startup.

**Rationale:** The bubbletea v2 API changed how AltScreen is controlled. In v2, the `tea.View` struct carries the AltScreen flag and the renderer honours it on each render cycle. The v1 `tea.WithAltScreen()` program option does not exist in v2. Setting it on `View` is the idiomatic v2 approach.

**Consequence:** AltScreen is re-asserted on every render, which is harmless. Any code that removes `v.AltScreen = true` from `View()` will silently disable AltScreen.

---

## 7. Provider replaced by fantasy.LanguageModel

*Added: 2026-05-06*

**Decision:** The `bob/provider.Provider` interface and the entire `bob/provider/` package
are deleted. `Model.langModel` is now `fantasy.LanguageModel` from `charm.land/fantasy`.
Streaming uses `fantasy.NewAgent(langModel).Stream(ctx, AgentStreamCall{...})` with the
`OnTextDelta` callback delivering tokens to the TUI.

**Rationale:** `charm.land/fantasy` is already vendored (via local replace directive) into
this module. It provides Anthropic, OpenAI, and Google providers out of the box, with
retry logic, tool call support, and streaming abstractions. Maintaining a custom `Provider`
interface and Anthropic implementation in `bob/provider` duplicates work that fantasy
already does correctly. Replacing the custom provider eliminates ~300 lines of provider
code and gives multi-provider support (Anthropic, OpenAI, Gemini) for free, while
`bob/harness` itself becomes simpler: it no longer needs to understand streaming internals.

**Consequence:** `harness.New` now takes `fantasy.LanguageModel` and `provName string`
instead of `provider.Provider`. Tests use a `mockLM` struct (implementing
`fantasy.LanguageModel`) defined in `mock_lm_test.go`. The `bob/provider` directory is
permanently deleted. Any future provider can be added in `bob/cmd/main.go` by adding a
new `case` in the provider switch and creating a `fantasy.LanguageModel` from it.

---

## 8. Tool Wiring: sdkToolAdapter bridges sdk.Tool to fantasy.AgentTool

*Added: 2026-05-06*

**Decision:** Extension-registered tools (`sdk.Tool`) are wrapped by `sdkToolAdapter` which implements `fantasy.AgentTool`. The adapter's `Run` method calls `Host.ExecuteTool`, which dispatches `EventBeforeToolCall` and blocks until the extension calls `tool_result`. The adapted tools are passed to `fantasy.NewAgent` via `WithTools(...)` at each stream start.

**Rationale:** Fantasy's agent loop handles tool call dispatch, retries, and multi-turn conversations internally. Wiring extension tools as `AgentTool` implementations lets us reuse all of that infrastructure without reimplementing tool call handling in the harness. The adapter is constructed fresh at each stream start (via `buildFantasyTools`) to reflect any new tools registered since the last call.

**Consequence:** Tools registered by extensions after the last stream started are not visible to the current in-flight stream; they appear on the next stream. This is acceptable because extensions register tools during `_init`, which runs at load time before any stream starts.

---

## 9. OnToolCall and OnToolResult Callbacks Wire Extension Events

*Added: 2026-05-06*

**Decision:** `startStream` wires `OnToolCall` and `OnToolResult` callbacks on `AgentStreamCall`. These callbacks dispatch `EventOnToolCall` and `EventOnToolResult` to extension subscribers respectively.

**Rationale:** Extensions that monitor (but don't implement) tool calls need to observe the tool call and result flow. Using the fantasy callbacks is the natural integration point: they fire with fully parsed tool call and result data, avoiding the need for the harness to parse streamed JSON.

**Consequence:** `EventOnToolCall` and `EventOnToolResult` are now dispatched for every tool call the fantasy agent processes, regardless of whether the tool is extension-backed. Extensions that subscribe to these events will see all tool calls including any provider-executed ones.

---

## 10. Pool-Based Agent Runtime — harness.Model Refactor

*Added: 2026-05-06*

**Decision:** `harness.Model` no longer owns a `fantasy.LanguageModel` or manages streaming state (`streaming bool`, `cancelStream *context.CancelFunc`, `startStream` method). Instead it holds `agentPool *agent.AgentPool` and `mainAgentID string`. Token and completion events arrive via pool callbacks wired in `SetProgram`.

**Rationale:** The `bob/agent` package now manages agent lifecycle, conversation history, and token delivery. The harness should be a thin TUI layer — it owns the chat view, input area, status bar, and extension dispatch, but not the LLM call itself. Removing streaming management from the model eliminates the `streamTickMsg` tick, the `cancelStream` pointer-to-pointer pattern, and the `startStream` goroutine closure. The model becomes simpler: `SubmitMsg` appends to history, shows the user message in the chat, and delegates to the pool.

**Consequence:** `harness.New` signature changed from `New(langModel, provName, h)` to `New(pool, mainAgentID, h)`. `SetActiveModel` is removed (model name is set via `setModelMsg`). `ctrl+c` no longer cancels an active stream — use double-Esc or the `OnAbort` extension callback to cancel. Tests no longer set `m.streaming` directly; they test the synchronous state changes (history, chat messages) and verify token/done delivery via manual callback invocation.
