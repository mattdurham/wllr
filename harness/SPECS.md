# harness — Interface Contracts and Behavioral Invariants

## 1. Model Structure — Pool-Based Agent Runtime

`Model` no longer owns a `langModel` or manages streaming state directly. Instead it holds:

- `agentPool *agent.AgentPool` — the pool owning all live agents.
- `mainAgentID string` — the ID of the primary agent the user interacts with.

Streaming state (`streaming bool`, `cancelStream`, `streamStart`) is **removed**. Turn lifecycle
is managed by the pool; the TUI receives `TokenMsg` and `StreamDoneMsg` via pool callbacks.

## 2. SubmitMsg Handling

- `SubmitMsg` is **never dropped**. Every submit is accepted and forwarded to the pool.
- On `SubmitMsg`:
  1. User message is appended to `m.history` (synchronously).
  2. User message is displayed in the chat view (synchronously).
  3. `m.agentPool.Send(m.mainAgentID, msg.Content)` is called — starts a non-blocking goroutine.
- If `agentPool` is nil (tests without a pool), the user message is still recorded in history/chat.
  A notification is shown if `Send` returns an error.

## 3. History Invariant

- **User messages** are appended to `m.history` synchronously in the `SubmitMsg` handler.
- **Assistant messages** are appended to `m.history` via `addAssistantMsgToHistoryMsg`, sent by the agent goroutine before `StreamDoneMsg`.
- `clearMsg` resets `m.history` to `nil`.
- History is not mutated concurrently — all mutations occur in the bubbletea event loop.

## 4. ctrl+c Behaviour

- `ctrl+c` **always returns `(m, tea.Quit)`** — there is no streaming guard.
  To cancel an active agent turn, use `abortStreamMsg` (sent via the `OnAbort` extension callback
  or double-Esc from the input area).
- `ctrl+q` always returns `(m, tea.Quit)`.

## 5. abortStreamMsg Behaviour

- `abortStreamMsg` calls `m.agentPool.Cancel(m.mainAgentID)` to cancel the active turn.
- If `agentPool` is nil, the message is a no-op.
- Returns `(m, nil)` — no bubbletea command is issued.

## 6. AltScreen

- `View()` always enables AltScreen by setting `v.AltScreen = true` on the returned `tea.View`.
- This is set on the `View` struct (bubbletea v2 API), not via a `tea.EnterAltScreen` program option.

## 7. ChatView

- `AppendToken(token)`: concatenates `token` to `current`, calls `refreshContent()`, scrolls to bottom.
- `FinalizeMessage()`: if `current == ""` it is a no-op; otherwise appends `chatMessage{role: RoleAssistant, content: current}` to `messages`, resets `current = ""`, calls `refreshContent()`.
- `Clear()`: sets `messages = nil` and `current = ""`, calls `refreshContent()`.
- `refreshContent()`: rebuilds the viewport content from `messages` followed by the in-progress `current` (if non-empty).
- Message order in `messages` reflects insertion order (append-only).

## 8. InputArea

- **Enter key** submits the trimmed content:
  - Empty content → no-op.
  - `/prefix` (starts with `/`) → parses as command: emits `CommandMsg{Name, Args}`.
  - Plain text → emits `SubmitMsg{Content}`.
  - In both cases the textarea is reset before emitting.
- **Shift+Enter** inserts a newline (overriding the default Enter binding in the textarea).
- **Esc key (first press):** resets the textarea (clears content), sets internal `lastWasEsc = true`. Does not emit any message.
- **Esc key (second consecutive press):** resets the textarea, clears `lastWasEsc`, and emits `abortStreamMsg{}` to cancel any active stream.
- **Any non-Esc keypress:** clears `lastWasEsc` (no abort will fire on the next Esc).

## 9. CommandRegistry

- **Unknown command:** `Dispatch` returns a `tea.Cmd` that produces `NotifyMsg{Text: "unknown command: /<name>"}`.
- **Duplicate registration:** silently overwrites the existing entry with the same name.
- **`/help` handling:** intercepted in `Model.Update` before reaching `Registry.Dispatch` — displays `commands.HelpText()` as a notification in the chat. The `/help` command is also registered in the registry (returns a generic message), but the Model-level intercept takes precedence.
- **`/clear`:** emits `clearMsg{}`.
- **`/reload`:** emits `ReloadMsg{}`.
- **`/model <name>`:** emits `setModelMsg{Model: name}`; with no args emits `NotifyMsg` with usage hint.

## 10. StatusBar

- Statuses are stored in a `map[string]string` keyed by an arbitrary string key.
- **"stream" key:** deleted from the map on `StreamDoneMsg` (success or `context.Canceled`); set to `"error"` on non-canceled errors.
  The `streamTickMsg` tick and animated indicator are removed — the pool manages turn lifecycle.
- `StatusUpdateMsg` sets or overwrites the value for `Key`.
- Keys are rendered in sorted order.

## 11. Extension Callbacks (SetProgram)

`SetProgram` wires four callbacks on `extHost`:

| Callback | Action |
|---|---|
| `OnSetStatus` | Sends `StatusUpdateMsg{Key, Value}` via `prog.Send` |
| `OnNotify` | Sends `NotifyMsg{Text}` via `prog.Send` |
| `OnSendMessage` | Sends `SubmitMsg{Content}` via `prog.Send` |
| `OnAbort` | Sends `abortStreamMsg{}` via `prog.Send` |

`abortStreamMsg` causes `Model.Update` to invoke `m.agentPool.Cancel(m.mainAgentID)`, cancelling the active agent turn.

## 12. SetProgram Threading Contract

- `SetProgram` **must be called before** `prog.Run()`.
- It is **not thread-safe** — no synchronization is provided.
- After `prog.Run()` starts, only `prog.Send` is safe to call from goroutines.

## 13. Agent Pool Contract (New Signature)

- `New(pool *agent.AgentPool, mainAgentID string, h *extension.Host) Model` is the sole constructor.
- `pool` may be `nil` for tests that do not exercise agent interaction; SubmitMsg is accepted but
  not forwarded to any agent.
- `mainAgentID` is the pool-registered ID of the primary (user-facing) agent.
- Language model and provider details are owned by the pool; `harness.New` does not receive them.

## 14. Token and Done Delivery via Pool Callbacks

- `TokenMsg` and `StreamDoneMsg` arrive in the TUI via the agent's `SetOnToken` and `SetOnDone` callbacks, wired in `SetProgram` (for the main agent) or in `cmd/main.go`'s `OnAgentSpawn` handler (for sub-agents).
- `TokenMsg{Token: text}` is sent via `prog.Send` for each non-empty text delta.
- `StreamDoneMsg{Err: err}` is sent via `prog.Send` when the agent turn completes.
- The harness itself does not launch goroutines — all async work is delegated to `agent.Agent.Submit`.

## 15. Tool Wiring (Delegated to Agent)

- Tool wiring is now handled by the `bob/agent` package via `SpawnOpts.Tools`.
- The harness `tools.go` helpers (`buildFantasyTools`, `sdkToolAdapter`) remain for use by the
  extension host, but the harness model no longer calls `buildFantasyTools` directly.
- Extension-registered tools are passed to the main agent's `SpawnOpts` in `cmd/main.go`.

## 19. Tool Call and Result Event Dispatching

- `OnToolCall` callback in `AgentStreamCall` dispatches `EventOnToolCall` with `OnToolCallPayload`.
- `OnToolResult` callback in `AgentStreamCall` dispatches `EventOnToolResult` with `OnToolResultPayload`.
- Both events use the same `toolCallID` so extensions can correlate call and result.
- `dispatchToolCallEvent` and `dispatchToolResultEvent` in `tools.go` handle the JSON marshalling and event dispatch.

## 20. SetLogFn

- `SetLogFn(fn func(int, string))` sets the logger used for internal harness warnings (tool schema parse failures).
- If `fn == nil`, warnings are silently dropped.
- Must be called before `prog.Run()`. Not thread-safe.
