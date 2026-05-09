# harness — Interface Contracts and Behavioral Invariants

Package `harness` implements the bubbletea v2 TUI for the bob coding assistant. It owns the
chat view, input area, status bar, autocomplete dropdown, modal overlay, extension dispatch
wiring, and the token batching layer between the agent pool and the TUI event loop.

---

## 1. Model Structure

`Model` is the root bubbletea v2 model. It holds:

- `agentPool *agent.AgentPool` — the pool owning all live agents.
- `mainAgentID string` — the ID of the primary agent the user interacts with.
- `extHost *extension.Host` — the extension host for dispatching events and managing WASM extensions.
- `chat ChatView` — the scrollable conversation viewport.
- `input InputArea` — the multi-line textarea with command detection.
- `statusBar StatusBar` — provider/model/token/status display.
- `commands *Registry` — the registered slash commands.
- `modalContent string` — non-empty when the modal overlay is open.
- `modalScroll int` — scroll offset for the modal.
- `suggestions []Command` — autocomplete candidates (non-empty when dropdown is visible).
- `streaming bool` — true while an agent turn is in progress.

**Invariant:** `Model` is a value type (passed by value through bubbletea). All mutable shared state reaches the TUI via `prog.Send`; no goroutine mutates `Model` fields directly.

---

## 2. Constructor

```go
func New(pool *agent.AgentPool, mainAgentID string, h *extension.Host) Model
```

- `pool` may be nil for tests that do not exercise agent interaction; SubmitMsg is accepted but not forwarded.
- `mainAgentID` is the pool-registered ID of the primary agent.
- `h` may be nil; extension callbacks are skipped when nil.
- Reads `pool.ProviderName()` at construction to initialise the status bar.
- Registers built-in commands (`/help`, `/clear`, `/reload`, `/model`) and a `/prompt` command (when pool is non-nil) that shows the accumulated base system prompt in a modal.
- Wires `h.OnRegisterCommand` immediately (before `SetProgram`) so extensions that register commands during `_init` are captured.

---

## 3. SetProgram

```go
func (m *Model) SetProgram(p *tea.Program)
```

Must be called after creating the bubbletea program and before calling `prog.Run()`. Not thread-safe. Wires all extension host callbacks and the main agent's token/done callbacks.

### Extension host callbacks wired in SetProgram

| Callback                 | Action                                                                 |
|--------------------------|------------------------------------------------------------------------|
| `OnSetStatus`            | `p.Send(StatusUpdateMsg{Key, Value})`                                  |
| `OnNotify`               | `p.Send(NotifyMsg{Text})`                                              |
| `OnSendMessage`          | `p.Send(SubmitMsg{Content})` or with compact display for skill XML     |
| `OnAbort`                | `p.Send(abortStreamMsg{})`                                             |
| `OnAfterToolCall`        | `p.Send(ToolCallDoneMsg{ID, IsError, Output})`                         |
| `OnModal`                | `p.Send(ShowModalMsg{Text})`                                           |
| `OnSetSystemPrompt`      | Calls `pool.SetBaseSystemPrompt(prompt)` (if pool non-nil)             |
| `OnAppendSystemPrompt`   | Calls `pool.AppendBaseSystemPrompt(text)` (if pool non-nil)            |
| `OnAgentSpawn`           | Creates agent in pool, wires batched token callback, wires tool/done   |
| `OnAgentClose`           | Calls `pool.Close(id)`                                                 |
| `OnAgentSendMessage`     | Calls `pool.Send(id, message)`                                         |
| `OnAgentList`            | Returns `pool.ListAgents()` as `[]AgentInfo`                           |
| `OnAgentTokenCount`      | Returns `pool.TokenCount()`                                            |
| `OnTeamCreate`           | Calls `pool.CreateTeam(id)`                                            |
| `OnTeamClose`            | Calls `pool.CloseTeam(ctx, id)`                                        |
| `OnTeamAddMember`        | Calls `pool.GetTeam(teamID).AddMember(agentID)`                        |
| `OnTeamRemoveMember`     | Calls `pool.GetTeam(teamID).RemoveMember(agentID)` (no error on nil)   |

**Invariant:** `OnSetSystemPrompt` and `OnAppendSystemPrompt` both route through the `AgentPool`, which propagates the prompt to all current and future agents. They do NOT set the prompt on the extension host or the harness model itself.

### Main agent callbacks wired in SetProgram

- `SetOnToken`: a batched token callback (see §5).
- `SetOnDone`: calls flush on the batcher, then `p.Send(StreamDoneMsg{Err})`.
- `SetToolsFn`: returns `BuildFantasyTools(extHost, "main", logFn)`.
- `SetOnToolCall`: `p.Send(ToolCallStartMsg{ID, ToolName, Input})`.

Sub-agents spawned via `OnAgentSpawn` receive identical wiring except `agentID` is the spawned agent's ID.

---

## 4. Update Sub-Handlers

`Model.Update(msg tea.Msg)` delegates to six ordered sub-handlers. Each returns `(Model, tea.Cmd, bool)`; the first handler to return `true` short-circuits the chain.

| Sub-handler        | Messages handled                                                                  |
|--------------------|-----------------------------------------------------------------------------------|
| `updateWindow`     | `tea.WindowSizeMsg`, `ShowModalMsg`                                               |
| `updateKeyPress`   | `tea.KeyPressMsg` — modal keys, dropdown navigation, Ctrl+C / Ctrl+Q, pgup/pgdown |
| `updateStream`     | `TokenMsg`, `streamTickMsg`, `StreamDoneMsg`, `addAssistantMsgToHistoryMsg`       |
| `updateTools`      | `ToolCallStartMsg`, `ToolCallDoneMsg`                                             |
| `updateActions`    | `SubmitMsg`, `CommandMsg`, `clearMsg`, `setModelMsg`, `abortStreamMsg`, `dispatchOnCommandMsg` |
| `updateExtension`  | `sessionStartDoneMsg`, `ExtensionEventResultMsg`, `ReloadMsg`, `NotifyMsg`, `StatusUpdateMsg` |

After all sub-handlers return false, remaining messages are forwarded to `m.input` (all messages) and `m.chat` (non-key messages only, to prevent viewport key bindings from stealing typed characters).

---

## 5. Token Batching (tokenBatcher)

```go
type tokenBatcher struct {
    mu       sync.Mutex
    buf      strings.Builder
    lastSend time.Time
    p        *tea.Program
}
const tokenBatchInterval = 30 * time.Millisecond
```

`makeBatchedOnToken(p)` returns `(onToken func(string), flush func())`. Tokens are coalesced and sent as a single `TokenMsg` at most every 30ms. `flush()` drains any buffered tail tokens immediately and must be called from `onDone`.

**Invariant:** The batcher uses time-based coalescing with no goroutines or channels — it is safe to call `flush()` multiple times across turns without panics. Locking is purely `sync.Mutex` on the buffer.

**Invariant:** Tokens emitted faster than 30ms are batched together; the TUI renders at most ~33 frames per second regardless of LLM streaming speed, preventing O(n²) viewport reflow work.

---

## 6. submitToAgent

`submitToAgent(content, display string)` is called from the `SubmitMsg` handler:

1. Adds `display` (or `content` if display is empty) to `m.chat` as a user message.
2. Appends `sdk.Message{Role: RoleUser, Content: content}` to `m.history`.
3. Sets `m.streaming = true`, records `m.streamStart`.
4. Dispatches `EventBeforeAgentStart` and `EventBeforeProviderRequest` to extensions.
5. Calls `pool.Send(mainAgentID, content)` (non-blocking).
6. Fires an immediate `streamTickMsg` and a 100ms tick to start the animated "working." indicator.

**Invariant:** `pool.Send` is non-blocking. The agent runs in a goroutine; results arrive via `TokenMsg` and `StreamDoneMsg`.

---

## 7. ChatView

`ChatView` renders the conversation history in a scrollable viewport.

### Fields

| Field            | Purpose                                                                         |
|-----------------|---------------------------------------------------------------------------------|
| `current`       | Accumulates in-progress assistant text during streaming                         |
| `lastDoneToolID`| Last completed tool call ID; reset by `FinalizeMessage`. Present but unused for routing (see §8) |
| `histContent`   | Cached rendered HTML of all finalized messages; rebuilt only when `histDirty`   |
| `histDirty`     | Set when `messages` changes; cleared after `refreshContent` rebuilds the cache  |
| `messages`      | Append-only slice of finalized `chatMessage` entries                            |
| `vp`            | The bubbles/viewport model                                                      |

### Key Methods

- `AppendToken(token)`: appends to `c.current`, calls `refreshContent()`, scrolls to bottom.
- `FinalizeMessage()`: if `c.current == ""` is a no-op; otherwise seals current into messages and resets.
- `AddUserMessage(content)`: appends user message, invalidates cache.
- `AddToolCall(id, toolName, input)`: seals any in-progress text first, then appends a pending tool entry.
- `UpdateToolCall(id, isError, output)`: marks the named tool call done; sets `lastDoneToolID = id`.
- `Clear()`: resets messages, current, lastDoneToolID, and histContent.

### histContent Cache

`refreshContent()` maintains a two-level cache:
1. `histContent` — the rendered string of all finalized messages. Rebuilt only when `histDirty` is true.
2. The live viewport content — `histContent + renderMessage(current)` when streaming, otherwise just `histContent`.

**Invariant:** `histContent` never includes `c.current`. On every token, only `c.current` changes, so only a string append is needed (not a full O(n) rebuild of all messages).

### renderToolGroup is a no-op

`renderToolGroup` is defined but intentionally empty — tool calls are completely hidden from the chat UI. Only the LLM's text responses (rendered as assistant boxes) are visible. Tool calls are stored in `messages` for internal state tracking but produce no output.

**Invariant:** Tool call visibility: none. Tool activity is reflected only indirectly when the LLM generates text that follows a tool call.

---

## 8. Token Routing — All Tokens Go to c.current

`AppendToken` always appends to `c.current` regardless of `lastDoneToolID`. The `lastDoneToolID` field is set by `UpdateToolCall` and reset by `FinalizeMessage` but is not used to route tokens to tool boxes.

**Invariant:** All streaming text from the LLM appears in the in-progress assistant message box (`c.current`), never inside a tool call box. This is correct because the LLM response following a tool result is still the assistant's reply, not a property of the tool call.

---

## 9. Color Rules for Chat Messages

| Message type          | Current turn                    | Historical turn              |
|-----------------------|---------------------------------|------------------------------|
| User message border   | Green (`#00AA00`)               | Grey (`#444444`)             |
| User message text     | Soft green (`#CCFFCC`)          | Grey (`#555555`)             |
| Assistant message border | Blue (`#89CFF0`)             | Grey (`#444444`)             |
| Assistant message text | White (`#FFFFFF`)              | Grey (`#555555`)             |
| System/notification   | Italic grey (`#555555`)         | (same)                       |
| Tool call boxes       | Not rendered                    | Not rendered                 |

"Current turn" means the message belongs to the most recent user turn (at or after `recentStart`, the last user message index).

---

## 10. renderUserMessage / renderAssistantMessage

Both functions draw a rounded Unicode box:

```
╭──────────────────────────────╮
│ content line                 │
╰──────────────────────────────╯
```

- Minimum width: 14 characters.
- Content is word-wrapped via `lipgloss.Wrap` to `width - 4` (accounting for borders and padding).
- Lines longer than `contentWidth` are hard-truncated at the rune boundary.
- Two blank lines (`\n\n`) follow each box to separate messages.

---

## 11. renderModal

`renderModal(height int)` renders an 80%-width centered modal popup:

- Width: `m.width * 8 / 10`, minimum 20.
- Left padding: `(m.width - modalW) / 2` spaces.
- Top border contains: `╭─ ↑↓ scroll · esc close ──────╮` (with scroll percentage when content overflows).
- Content lines: `height - 2` (room for top + bottom border).
- `modalScroll` offsets the visible window into the content lines.
- Bottom border: `╰──────────────────────────────╯`.

The caller (`View`) adds top and bottom blank-line margins to vertically center the modal in the chat area.

**Invariant:** `modalScroll` is clamped to `[0, max(0, len(lines) - contentLines)]` on render.

---

## 12. renderInputBox

`renderInputBox()` wraps the textarea in a white-bordered box with the status line embedded in the top border:

```
╭─ anthropic  claude-sonnet  tokens:1234  working....  1m 42s ────╮
│ cursor line                                                       │
╰───────────────────────────────────────────────────────────────────╯
```

- Always reads `m.agentPool.TokenCount()` live (so sub-agent tokens are reflected immediately).
- Uses `lipgloss.Width` (not `len([]rune)`) for padding to correctly account for ANSI escape sequences in the textarea output.

---

## 13. Autocomplete (Slash Commands)

### slashWordAt

`slashWordAt(val string) int` finds the index of the last `/` that starts an incomplete command word — it must be at position 0 or preceded by a space, and there must be no space between it and the end of the string. Returns -1 if no such position exists.

### updateSuggestions

Called after every key event. Computes `slashWordAt(input.Value())`. If a slash word is found, filters `commands.List()` to entries whose names have the typed prefix. Updates `m.suggestions`.

**Invariant:** When `slashWordAt` returns -1, the dropdown is closed (`closeSuggestions`). When it returns a valid index, all commands with matching prefix are shown.

### Dropdown Layout

- Maximum 8 entries visible at a time.
- `dropdownOffset` tracks the first visible entry; adjusted when `suggestionIdx` moves out of the visible window.
- Keyboard: Up/Down navigates; Tab completes the selected entry (replaces the slash word in the input); Enter dispatches the selected command; Esc closes.
- Selected entry is highlighted with a blue background (`#1A4A8A`).
- The dropdown renders between the chat area and the input box; its height is subtracted from `chatHeight()`.

---

## 14. InputArea

`InputArea` wraps a `bubbles/textarea` with command detection.

- **Enter**: submits trimmed content. Empty → no-op. `/word...` → `CommandMsg`. Plain text → `SubmitMsg`.
- **Shift+Enter**: inserts a newline (overrides default Enter binding in textarea).
- **Esc (first press)**: clears the textarea, sets `lastWasEsc = true`. No message emitted.
- **Esc (second consecutive press)**: clears textarea, emits `abortStreamMsg{}`.
- **Any other key**: clears `lastWasEsc`.

---

## 15. Key Handling: Modal Takes Priority

When `m.modalContent != ""`, all key events are consumed by the modal handler:

| Key        | Action                                  |
|------------|-----------------------------------------|
| esc/enter/q | Close modal (`modalContent = ""`)      |
| up         | Scroll modal up one line                |
| down       | Scroll modal down one line (clamped)    |

No key is forwarded to the input area while the modal is open.

---

## 16. Key Handling: Ctrl+C

- If `m.streaming == true`: calls `m.agentPool.Cancel(m.mainAgentID)`, sets status to "cancelling…". Does NOT quit.
- If `m.streaming == false`: returns `tea.Quit`.
- `Ctrl+Q` always quits regardless of streaming state.

---

## 17. CommandRegistry

```go
type Registry struct { commands map[string]Command }

func (r *Registry) Register(cmd Command)
func (r *Registry) Dispatch(name string, args []string) tea.Cmd
func (r *Registry) List() []Command
func (r *Registry) HelpText() string
```

- **Unknown command:** `Dispatch` returns a cmd that produces `NotifyMsg{Text: "unknown command: /<name>"}`.
- **Duplicate registration:** silently overwrites the existing entry with the same name.
- **`/help`:** intercepted in `updateActions` before reaching `Registry.Dispatch` — displays `commands.HelpText()` via `ShowModalMsg`.

Built-in commands registered at startup:

| Command   | Effect                                              |
|-----------|-----------------------------------------------------|
| `/help`   | Shows `ShowModalMsg{Text: commands.HelpText()}`     |
| `/clear`  | Emits `clearMsg{}`                                  |
| `/reload` | Emits `ReloadMsg{}`                                 |
| `/model <name>` | Emits `setModelMsg{Model: name}`             |
| `/prompt` | Shows accumulated base system prompt in a modal     |

---

## 18. StatusBar

- `providerName`, `modelName`, `totalTokens` set at construction or via messages.
- `statuses map[string]string` keyed by arbitrary string; rendered in sorted order.
- `Line()` returns unstyled text; used in `renderInputBox` top border.
- `View()` returns lipgloss-styled text; not currently used in the main view (status is embedded in the input box border).
- Token count is updated live from `m.agentPool.TokenCount()` on every render; the status bar's own `totalTokens` field is also updated from `StreamDoneMsg`.

---

## 19. Message Types

| Type                         | Direction          | Purpose                                                         |
|-----------------------------|--------------------|-----------------------------------------------------------------|
| `TokenMsg{Token}`           | agent → TUI        | Streaming text delta                                            |
| `StreamDoneMsg{Err, AgentID}` | agent → TUI      | Agent turn completed                                            |
| `sessionStartDoneMsg`       | async cmd → TUI    | session_start dispatch complete; triggers default prompt injection |
| `ExtensionEventResultMsg`   | async cmd → TUI    | Results from dispatching non-session_start events to extensions |
| `ReloadMsg`                 | command → TUI      | Trigger hot-reload of all extensions                            |
| `NotifyMsg{Text}`           | any → TUI          | Show notification in chat                                       |
| `StatusUpdateMsg{Key,Value}`| extension → TUI    | Update keyed status in the status bar                           |
| `SubmitMsg{Content,Display}`| input/ext → TUI    | Submit content to the agent; Display shown in chat if non-empty |
| `CommandMsg{Name,Args}`     | input → TUI        | User typed a slash command                                      |
| `ToolCallStartMsg{ID,...}`  | agent → TUI        | Agent dispatched a tool call                                    |
| `ToolCallDoneMsg{ID,...}`   | OnAfterToolCall → TUI | Tool call completed                                          |
| `ShowModalMsg{Text}`        | any → TUI          | Open the modal overlay                                          |
| `abortStreamMsg`            | OnAbort/Esc → TUI  | Cancel the active agent turn                                    |
| `dispatchOnCommandMsg`      | command → TUI      | Dispatch EventOnCommand for an extension-registered command     |
| `streamTickMsg`             | internal timer     | Drive the "working." animated indicator                         |

---

## 20. buildDefaultActionPrompt

```go
func buildDefaultActionPrompt(tools []sdk.Tool, commands []Command) string
```

Called once from `updateExtension` when `sessionStartDoneMsg` arrives — after all `session_start` extension handlers have run and all tool/command registrations are complete.

Produces a markdown section injected via `pool.AppendBaseSystemPrompt`:

```
## Capabilities

Act immediately on user requests using your tools. Read files, run commands, edit code — don't describe what you plan to do, just do it.

### Tools

- **tool_name** — description
...

### Slash commands

- **/command** — description
...
```

**Invariant:** Tools are sorted alphabetically before rendering. Commands appear in the order returned by `Registry.List()` (sorted by name). Empty descriptions are replaced with `"(no description)"`.

**Invariant:** If `tools` is empty, the Tools section is omitted. If `commands` is empty, the Slash commands section is omitted.

**Invariant:** Called at most once per session (from `sessionStartDoneMsg`). Subsequent tool/command registrations (e.g. from hot-reload) do not re-inject the prompt.

---

## 21. BuildFantasyTools

```go
func BuildFantasyTools(extHost *extension.Host, agentID string, logFn func(int, string)) []fantasy.AgentTool
```

Returns the current set of registered tools from `extHost.RegisteredTools()` as `[]fantasy.AgentTool`. Each tool is wrapped in an `sdkToolAdapter` that calls `extHost.ExecuteTool(ctx, agentID, ...)` when the fantasy agent invokes the tool.

**Invariant:** Returns nil if `extHost` is nil. Returns nil if no tools are registered.

**Invariant:** `agentID` is forwarded to `ExecuteTool`, which includes it in `BeforeToolCallPayload.AgentID` and `AfterToolCallPayload.AgentID`.

---

## 22. View Layout

```
┌────────────────────────────────────┐
│  chat viewport (scrollable)        │  height = m.height - inputAreaHeight - dropdownHeight
│  [optional: suggestion dropdown]   │  dropdownHeight = min(8, len(suggestions)) + 2 borders (0 when hidden)
│  [or: modal overlay (centered)]    │  height = chatHeight * 8/10, vertically centered
├────────────────────────────────────┤
│  input box (5 lines)               │  top border + 3 textarea lines + bottom border
└────────────────────────────────────┘
```

- `AltScreen = true` is set on every `tea.View` returned from `View()`.
- The output is padded to exactly `m.height` lines to prevent old content bleeding through on resize.
