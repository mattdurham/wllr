# harness — Interface Contracts and Behavioral Invariants

Package `harness` implements the bubbletea v2 TUI for the bob coding assistant. It owns the
chat view, input area, autocomplete dropdown, modal overlay, extension dispatch wiring, and
the token batching layer between the agent pool and the TUI event loop. Status display is
fully scene-driven via the `statusline` WASM extension (see §28).

---

## 1. Model Structure

`Model` is the root bubbletea v2 model. It holds:

- `agentPool *agent.AgentPool` — the pool owning all live agents.
- `mainAgentID string` — the ID of the primary agent the user interacts with.
- `extHost *extension.Host` — the extension host for dispatching events and managing WASM extensions.
- `chat ChatView` — the scrollable conversation viewport.
- `input InputArea` — the multi-line textarea with command detection.
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
- `h` may be nil; extension bridge installation is skipped when nil.
- Reads `pool.ProviderName()` at construction to initialise the status bar.
- Reads the spawned main agent's `ModelName()` at construction, when present, to initialise active model/status state before extension `session_start`.
- Registers built-in commands (`/help`, `/clear`, `/reload`, `/model`, `/models`) and a `/prompt` command (when pool is non-nil) that shows the accumulated base system prompt in a modal.
- Immediately installs an `earlyUIBridge` on the extension host so that extensions can register commands during `_init` (before `SetProgram`).
- Immediately installs an `earlyAgentBridge` stub on the extension host so that extensions calling `agent_spawn` during `_init` receive a clear error instead of a nil-pointer dereference.

---

## 3. SetProgram

```go
func (m *Model) SetProgram(p *tea.Program)
```

Must be called after creating the bubbletea program and before calling `prog.Run()`. Not thread-safe. Constructs the three harness bridge implementations and installs them on the extension host, then wires the main agent's token/done callbacks.

### Bridge construction in SetProgram

`SetProgram` creates an `agent.Spawner` and constructs three bridge structs that implement the extension host's bridge interfaces:

| Bridge struct           | Installed via            | Replaces          |
|------------------------|--------------------------|-------------------|
| `harnessAgentBridge`   | `h.SetAgentBridge`       | `earlyAgentBridge` stub |
| `harnessTeamBridge`    | `h.SetTeamBridge`        | (none; first install)   |
| `harnessUIBridge`      | `h.SetUIBridge`          | `earlyUIBridge` stub    |

`harnessAgentBridge` wraps an `agent.Spawner` (which owns sub-agent lifecycle) and the pool (for close/send/list). Its list operation returns each agent's ID, display name, running state, `working`/`liveness` state, pending inbox message count, and intra-turn liveness fields (recent activity age, turn duration, active/last tool, last tool completion, shutdown-request state). `harnessTeamBridge` delegates directly to the pool. `harnessUIBridge` sends bubbletea messages to `p` for all UI operations.

`CapabilityProvider` and `MCPBridge` are NOT installed by SetProgram — `CapabilityProvider` is installed by `cmd/main.go` via `h.SetCapabilities`, and `MCPBridge` is not currently used.

`SetProgram` also wires three pool callbacks (when `pool != nil`): `SetContextUsageDispatcher` (forwards `EventContextUsage`), `SetWakeNotifier` (sends `agentWakeupMsg` when a `Deliver` wakes the main agent), and `SetProviderRequestInterceptor` (routes the `before_provider_request` transform chain to `extHost.DispatchEventChain` via the package helper `interceptProviderRequest`). The interceptor lets extensions redact messages, reroute the model, or block the request just before each turn streams; a malformed transformed payload is tolerated (original messages/model kept). `submitToAgent` no longer dispatches `before_provider_request` — interception happens inside the agent turn where the real messages + model exist (see agent SPECS §9 Provider-Request Interception).

**Invariant:** `harnessUIBridge.SetSystemPrompt` and `harnessUIBridge.AppendSystemPrompt` both route through the `AgentPool`, which propagates the prompt to all current and future agents. They do NOT set the prompt on the extension host or the harness model itself.

The host reserves `/history` and dispatches `EventOnCommand` with
`Name: "history"`; the bundled history extension owns the picker behavior.
This keeps the command invocable when a generated extension artifact is stale
or unavailable.

### Main agent callbacks wired in SetProgram

- `SetOnToken`: a batched token callback (see §5).
- `SetOnDone`: calls flush on the batcher, then `p.Send(StreamDoneMsg{Err})`.
- `SetToolsFn`: returns `tools.BuildFantasyTools(extHost, "main", logFn)`.
- `SetOnToolCall`: `p.Send(ToolCallStartMsg{AgentID: mainID, ID, ToolName, Input})`.
- If the main agent is recovered after an `ErrAgentNotFound`, these callbacks
  and the dynamic tool function are wired onto the replacement before the user
  turn is retried.

Sub-agents spawned via `harnessAgentBridge.Spawn` (delegated to `agent.Spawner`) receive similar wiring with the spawned agent's ID. Their token output is not routed to the main chat, but their tool-call starts are routed to the tool activity pane/log with `AgentID` populated.

---

## 4. Update Sub-Handlers

`Model.Update(msg tea.Msg)` delegates to six ordered sub-handlers. Each returns `(Model, tea.Cmd, bool)`; the first handler to return `true` short-circuits the chain.

| Sub-handler        | Messages handled                                                                  |
|--------------------|-----------------------------------------------------------------------------------|
| `updateWindow`     | `tea.WindowSizeMsg`, `ShowModalMsg`                                               |
| `updateKeyPress`   | `tea.KeyPressMsg` — modal keys, dropdown navigation, Ctrl+C / Ctrl+Q, pgup/pgdown |
| `updateStream`     | `agentWakeupMsg`, `TokenMsg`, `streamTickMsg`, `StreamDoneMsg`                    |
| `updateTools`      | `ToolCallStartMsg`, `ToolCallDoneMsg`, `ConsoleMsg`                               |
| `updateActions`    | `SubmitMsg`, `CommandMsg`, `clearMsg`, `setModelMsg`, `abortStreamMsg`, `dispatchOnCommandMsg` |
| `updateExtension`  | `sessionStartDoneMsg`, `ExtensionEventResultMsg`, `ReloadMsg`, `NotifyMsg`, `StatusUpdateMsg` |

After all sub-handlers return false, remaining messages are forwarded to `m.input` (all messages) and `m.chat` (non-key messages only, to prevent viewport key bindings from stealing typed characters).

`Esc` is special-cased before picker/modal/input handling: it always sends a best-effort cancellation request to the agent pool. If a turn was active at the time the key was pressed, the event is consumed and the stream status is set to `cancelling…`; otherwise normal idle `Esc` behavior continues (picker/modal close, dropdown close, or input clear). Agent-pool cancellation is a no-op when no turn is running.

---

## 5. Token Batching (tokenBatcher)

```go
type tokenBatcher struct {
    mu       sync.Mutex
    buf      strings.Builder
    lastSend time.Time
    p        *tea.Program
    dispatch func(string) // optional: forward each batch to WASM (EventToken)
}
const tokenBatchInterval = 75 * time.Millisecond
```

`makeBatchedOnToken(p, dispatch)` returns `(onToken func(string), flush func())`. Tokens are coalesced and sent as a single `TokenMsg` at most every 75ms. `flush()` drains any buffered tail tokens immediately and must be called from `onDone`. When `dispatch` is non-nil, each flushed batch is also passed to it; `wireMainAgentCallbacks` supplies a closure that marshals a `sdk.TokenPayload` and calls `Host.DispatchEvent(EventToken)` so streamed text reaches WASM extensions. The dispatch runs on the agent goroutine, not the bubbletea loop.

**Invariant:** The batcher uses time-based coalescing with no goroutines or channels — it is safe to call `flush()` multiple times across turns without panics. Locking is purely `sync.Mutex` on the buffer.

**Invariant:** Tokens emitted faster than 75ms are batched together; the TUI renders at most ~13 token-driven frames per second regardless of LLM streaming speed, preventing O(n²) viewport reflow work.

---

## 6. submitToAgent

`submitToAgent(content, display string)` is called from the `SubmitMsg` handler:

1. Adds `display` (or `content` if display is empty) to `m.chat` as a user message.
2. Sets `m.streaming = true`, records `m.streamStart`.
3. Dispatches `EventBeforeAgentStart` and `EventBeforeProviderRequest` to extensions.
4. Calls `pool.Send(mainAgentID, content)` (non-blocking). If the primary agent
   is missing, it attempts one `EnsureMainAgent` recovery and retries the send.
5. Fires an immediate `streamTickMsg` and a 100ms tick to start the animated "working." indicator.

Note: `m.history` was removed (see NOTES.md §20). The canonical conversation history lives in `pool.Get(mainID).History()`.

**Invariant:** `pool.Send` is non-blocking. The agent runs in a goroutine; results arrive via `TokenMsg` and `StreamDoneMsg`.

---

## 7. ChatView — Externally-Driven Viewport

`ChatView` is a thin wrapper around a `bubbles/viewport` whose transcript content
is produced externally (by a WASM extension driving the `chat` scene area) and
fed in via `SetExternalContent`. `ChatView` no longer renders messages itself;
there is no built-in message renderer.

### Fields

| Field             | Purpose                                                  |
|-------------------|----------------------------------------------------------|
| `externalContent` | Last content set via `SetExternalContent` (re-applied on resize) |
| `toolLog`         | Per-turn tool-call log (independent of the transcript; shown by `/tools`) |
| `vp`              | The bubbles/viewport model                               |

### Key Methods

- `SetExternalContent(content)`: replaces the viewport content; scrolls to bottom only if the viewport was already at bottom, preserving manual scrollback during streaming.
- `SetSize(width, height)`: resizes the viewport and re-applies `externalContent`; if the viewport was at the tail before resize, it remains at the tail afterward.
- `ScrollUp(n)` / `ScrollDown(n)`: scroll the viewport.
- `AddToolCall(id, name, input)` / `UpdateToolCall(id, isError, output)` / `ClearToolLog()`: maintain the per-turn tool log.
- `ToolActivityLines(width, height)`: renders the most recent tool log entries as compact single-line status rows, truncated to `width` runes and capped at `height` rows.
- `ToolLogModal()`: renders the tool log for the `/tools` modal.
- `View()`: returns the viewport view.

**Invariant:** The viewport (scroll, size, tail-follow behavior) is harness-owned; transcript content is always external. The tool log is independent of the transcript and never appears in it. The harness always renders a separate tool activity pane below the transcript in the normal view.

---

## 11. renderModal

`renderModal(height int)` renders an 80%-width centered modal popup:

- Width: `m.width * 8 / 10`, minimum 20.
- Left padding: `(m.width - modalW) / 2` spaces.
- Top border contains: `╭─ ↑↓ scroll · esc close ──────╮` (with scroll percentage when content overflows).
- Content lines: `height - 2` (room for top + bottom border).
- `modalScroll` offsets the visible window into the content lines.
- Bottom border: `╰──────────────────────────────╯`.

Content lines are hard-wrapped to the content width by `wrapModalLines(lines, width)` before the scroll window is applied, so long unbreakable tokens (e.g. the OAuth authorize URL) wrap across rows and stay fully visible rather than being truncated. Blank lines are preserved.

The caller (`View`) adds top and bottom blank-line margins to vertically center the modal in the chat area.

**Invariant:** `modalScroll` is clamped to `[0, max(0, len(lines) - contentLines)]` on render, where `lines` is the post-wrap line count.

**Invariant:** `wrapModalLines` preserves content exactly — concatenating the output rows for a given input line reproduces that line.

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

InputArea only receives key events that the model-level overlay/global handlers do not consume.

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

## 16. Key Handling: Esc, Ctrl+C

- If `m.streaming == true` or any agent in the pool reports `IsRunning()`, `Esc` calls `m.agentPool.CancelAll()` (cancels main and all sub-agents), sets status to "cancelling…", and does not quit.
- Active-turn cancellation has priority over modal, picker, autocomplete, and input Esc handling.
- If `m.streaming == false`, `Esc` is not a global hotkey. It may still be consumed by active pickers, modals, autocomplete, or the input component.
- `Ctrl+C` always returns `tea.Quit`, regardless of streaming state.
- `Ctrl+Q` always quits regardless of streaming state.

---

## 17. CommandRegistry

```go
type Registry struct { commands map[string]Command }

func (r *Registry) Register(cmd Command)
func (r *Registry) Dispatch(name string, args []string) tea.Cmd
func (r *Registry) Get(name string) (Command, bool)
func (r *Registry) List() []Command
func (r *Registry) HelpText() string
```

- **Unknown command:** `Dispatch` returns a cmd that produces `NotifyMsg{Text: "unknown command: /<name>"}`.
- **Duplicate registration:** silently overwrites the existing entry with the same name.
- **`/help`:** intercepted in `updateActions` before reaching `Registry.Dispatch` — displays `commands.HelpText()` via `ShowModalMsg`.

### Command.Instant — Fast Path for Built-in Commands

```go
type Command struct {
    Handler func(args []string) tea.Cmd
    Name    string
    Desc    string
    Instant bool
}
```

**Invariant:** Commands with `Instant=true` bypass the "queuing..." UI indicator in `updateActions`. When a `CommandMsg` for an Instant command arrives, `updateActions` invokes `cmd.Handler(msg.Args)` directly without setting `statusBar.statuses["stream"] = "queuing…"`.

**Invariant:** All built-in commands (`/help`, `/clear`, `/reload`, `/model`, `/models`, `/thinking`, `/login`, `/status`, `/tools`) have `Instant=true`. The zero value of `Command.Instant` is `false`. (The `/prompt` command is registered without `Instant=true` because it executes synchronously in the update loop via `ShowModalMsg`, not via WASM dispatch — it is intentionally excluded from the instant list.)

**Invariant:** Extension-registered commands set `Instant` from the `instant bool` parameter passed to `UIBridge.RegisterCommand(name, desc, instant bool)`. When `instant=true`, the flag is stored on the `Command`, suppressing the "queuing…" status. The handler still routes through `dispatchOnCommandMsg` → `EventOnCommand`.

Built-in commands registered at startup:

| Command         | Instant | Effect                                                       |
|-----------------|---------|--------------------------------------------------------------|
| `/help`         | true    | Shows `ShowModalMsg{Text: commands.HelpText()}`              |
| `/clear`        | true    | Emits `clearMsg{}`                                           |
| `/reload`       | true    | Emits `ReloadMsg{}`                                          |
| `/model`        | true    | No arg → `showModelPickerMsg{}` (opens model picker); `/model <name>` → `setModelMsg{Model: name}` |
| `/models`       | true    | Alias for `/model` with no args; opens model picker |
| `/thinking`     | true    | No arg → `showThinkingPickerMsg{}` (opens level picker); `/thinking <level>` → `setThinkingMsg{Level: level}` |
| `/login`        | true    | No args → `showLoginProviderPickerMsg{}` (opens the install-style provider wizard); `/login auth` → `loginMsg{}` (authenticates the active provider) |
| `/status`       | true    | Emits `StatusUpdateMsg{Key: "_override", Value: text}`       |
| `/tools`        | true    | Emits `showToolsMsg{}`                                       |
| `/prompt`       | false   | Shows accumulated base system prompt in a modal              |

---

## 18. liveState — Shared Harness State

`liveState` is a goroutine-safe struct (mutex-guarded) shared between the bubbletea
Update loop and WASM bridge goroutines. It holds:

- `streaming bool` — true while an agent turn is in progress.
- `streamStart time.Time` — when the current turn started.
- `tokens int` — latest total token count.
- `model string` — active model name.
- `provider string` — active provider name.
- `statuses map[string]string` — keyed status values that the `statusline` extension
  and `get_status_info` read. Updated via `setStatus(key, value)`; empty value deletes.
- `tokens int` — latest total token count copied from `AgentPool.TokenCount()` on
  each `TokenMsg` and `StreamDoneMsg`.
- `width int` — terminal width.
- `hasError bool` — true when the last turn completed with an error.

The `StatusBar` struct has been removed. All state that was in `StatusBar` is now in
`liveState`; the statusline scene area (owned by the bundled WASM extension) reads
it via `get_status_info`.

### Context Usage (`ctx rem`)

On each `StreamDoneMsg`, the handler calls `m.agentPool.MainAgentContextUsage()` and
updates `liveState.statuses["ctx rem"]` via `setStatus`:

- When `cu.ContextWindow > 0`: `"ctx rem"` = `fmt.Sprintf("%.0f%%", rem)` where `rem`
  is `thresholdPct*100 - cu.Percent`.
- When `cu.ContextWindow == 0`: `"ctx rem"` is deleted (empty string to `setStatus`).

**Invariant:** The `ctx rem` key is only present when a context window is configured
and at least one turn has completed. Updated once per `StreamDoneMsg`.

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
| `ToolCallStartMsg{AgentID,ID,...}` | agent → TUI  | Agent dispatched a tool call                                    |
| `ToolCallDoneMsg{AgentID,ID,...}`  | OnAfterToolCall → TUI | Tool call completed                                     |
| `ShowModalMsg{Text}`        | any → TUI          | Open the modal overlay                                          |
| `abortStreamMsg`            | OnAbort/Esc → TUI  | Cancel the active agent turn                                    |
| `dispatchOnCommandMsg`      | command → TUI      | Dispatch EventOnCommand for an extension-registered command     |
| `streamTickMsg`             | internal timer     | Drive the "working." animated indicator                         |

---

## 20. Prompt assembly

Prompt rendering is implemented by the bundled `prompt.wasm` extension. The
harness includes registered tool names and slash-command metadata in the
`SessionStartPayload`; it does not construct prompt text.

The extension produces the markdown base prompt via `SetSystemPrompt`, loads
configured prompt files and AGENTS/CLAUDE context, and permits other
extensions to append sections with `AppendSystemPrompt`:

```
## Action Rules

You are an action-taking agent...

### Project Scope

- Treat the directory where wllr was launched as the project root.
- Scope reads, searches, edits, tests, and shell commands to that directory and descendants by default.
- Use relative paths and omit `exec.dir` unless another directory is explicitly requested.
- Do not inspect parent directories, home directories, sibling repositories, or unrelated folders without an explicit user request or task requirement.

### Editing Files

- Use `edit_file` for source-code edits with exact `oldText`/`newText` replacements.
- Do not use `sed`, `perl`, Python, or shell redirection to modify files; `rg` and `read_file` are for inspection.
- `apply_patch` is not a wllr runtime command; use `edit_file` within wllr.

Available tools: exec, lsp_diagnostics, lsp_lint, read_file, ...

### Code Intelligence

- Prefer LSP tools for diagnostics, linting, code navigation...

### Slash commands

- **/command** — description
...
```

**Invariant:** Tool names are sorted alphabetically before rendering. Tool descriptions are not duplicated in the prompt because schemas/descriptions are already sent through the provider tool API. Commands appear in the order returned by `Registry.List()` (sorted by name). Empty descriptions are replaced with `"(no description)"`.

**Invariant:** The prompt always includes `Project Scope` guidance that treats the launch working directory as the default project root and prohibits unrelated filesystem exploration unless explicitly requested or required.

**Invariant:** If any primary LSP tool is registered (`lsp_diagnostics`, `lsp_lint`, `lsp_definition`, or `lsp_references`), the prompt includes a `Code Intelligence` section that makes LSP tools the primary path for diagnostics, linting, code navigation, finding references, and refactor reconnaissance. The guidance names `lsp_symbols`, `lsp_definition`, `lsp_references`, `lsp_refactor_preview`, `lsp_diagnostics`, `lsp_lint`, and `lsp_capabilities`; tells agents to call `lsp_capabilities` at the start of repo/code work unless backend/output-contract details are already known for the session; tells agents to use symbol/definition/reference tools before broad shell search or large file sweeps; instructs agents to apply refactor edits with normal file-editing tools after reviewing preview output; and treats `exec`/manual search as fallback when LSP output is unavailable, incomplete, or unrelated to the task.

**Invariant:** If `tools` is empty, the available-tool list is omitted. If `commands` is empty, the Slash commands section is omitted.

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
┌──────────────────────────────────────┐
│  chat viewport (scrollable)          │  area: "chat", height = m.height - statusLineHeight - inputBoxHeight - dropdownHeight - bottomGutterHeight
│  queued message pane (optional)      │  `Queued` header; pending main-agent inbox messages stay outside history
│  tool activity pane                  │  3 content lines + border
│  [optional: suggestion dropdown]     │  dropdownHeight = min(8, len(suggestions)) + 2 borders (0 when hidden)
│  [or: modal overlay (centered)]      │  height = chatHeight * 8/10, vertically centered
├──────────────────────────────────────┤
│  statusline (1–N lines)              │  area: "statusline", placement: status; height = statusLineHeight()
├──────────────────────────────────────┤
│  input box (dynamic, usually 5 lines)│  rendered input box height (top border + textarea + bottom border)
│  bottom gutter (1 line)              │  keeps the input bottom border off the terminal edge
└──────────────────────────────────────┘
```

- `AltScreen = true` is set on every `tea.View` returned from `View()`.
- The output is padded to exactly `m.height` lines to prevent old content bleeding through on resize.
- `statusLineHeight()` sums the constrained heights of all `UIAreaStatus` areas; it is called on every `View()` and `chatHeight()` invocation.
- `inputBoxHeight()` counts the rendered input box lines instead of relying on a fixed constant, so the statusline cannot push the input bottom border off-screen if textarea rendering changes.
- `toolActivityHeight()` returns 5 rows in the normal layout; those 5 rows are subtracted from `chatHeight()`.
- Before a normal `View()` render, `ChatView.height` equals the layout computed from the same stable main-agent inbox snapshot used to render the queued-message pane, including its current five-row height when non-empty. Layout synchronization runs before updates and rendering; an inbox transition is reflected on the next render without allowing height/render decisions from one frame to disagree.
- If the terminal cannot fit the queue pane while preserving the fixed lower UI, the queue pane is omitted from the render as well as from the height calculation; otherwise the chat viewport yields space to the queue and may shrink to zero while the input remains visible.
- `bottomGutterHeight()` reserves one trailing row when the terminal has more than one row, so the input bottom border is not rendered on the final terminal line.
- `chatHeight()` bottoms out at 0 in very small terminals; when collapsed, `View()` omits the chat viewport row so the fixed lower UI can still fit.
- `UIAreaStatus` areas are NOT included in `renderScenes()` — they are rendered by `renderStatusLine()` between the console pane and the input box.

## 23. ConsoleView

`ConsoleView` is a ring-buffer live tail pane for subprocess output lines.

- Ring buffer capacity: 200 lines (`consoleRingSize`).
- Always renders the most-recent lines (live tail, no scroll).
- `Append(line)` evicts the oldest line when the buffer is full.
- `Clear()` resets the buffer to empty.
- `IsEmpty()` returns true when no lines have been appended since the last `Clear`.
- `View(width, height)` renders the last `min(height, count)` lines, each truncated to `width` runes.

**Invariant:** the `ChatView` transcript and `ConsoleView` are independent display components.

**Invariant:** `consoleVisible` is set to `true` on `ConsoleMsg{Line: ...}` and to `false` on `StreamDoneMsg`.

**Invariant:** `ConsoleMsg` never adds to `m.chat.messages`. Console lines are ephemeral and not part of the conversation history.

## 25. Session Persistence Hooks

`harness.Model` exposes two exported callback fields for session persistence:

| Field | Type | Called when |
|-------|------|-------------|
| `OnUserMessage` | `func(content string)` | User submits input (in `submitToAgent`) |
| `OnMessageEnd` | `func(role, content string)` | Assistant turn completes (after `FinalizeMessage`) |

**Invariant:** Both callbacks are nil by default; nil callbacks are no-ops (guarded in model).

**Invariant:** `OnMessageEnd` is called only when `responseContent != ""` (tool-only turns are excluded).

**Invariant:** `OnUserMessage` is called only when `content != ""`.

### Model Selection Hooks

`harness.Model` exposes two callback fields for the `/model` picker (set by `cmd/main.go`):

| Field | Type | Purpose |
|-------|------|---------|
| `ModelListFn` | `func() []ModelChoice` | Returns the active provider's selectable models for the picker. Nil ⇒ selection unavailable. |
| `SelectModelFn` | `func(modelID string) error` | Switches the active model: rebuilds the main agent's LM (`Agent.SetModel`), updates the context window, and persists the choice. Nil ⇒ display-only. |

Flow: `/model` with no arg (or `/models`) emits `showModelPickerMsg` → `openModelPicker()` builds picker items from `ModelListFn` (marking the current model) and opens the picker with the reserved `modelPickerCallback` (`"__wllr:model"`). On selection, `updateKeyPressPicker` recognises the core callback and emits `setModelMsg{Model: id}` (rather than dispatching `EventOnCommand` to a WASM extension); the `setModelMsg` handler calls `applyModelSelection` → `SelectModelFn` + status update + `EventModelChanged`. `/model <name>` skips the picker and emits `setModelMsg` directly.

**Invariant:** picker callbacks prefixed `"__wllr:"` are core-owned and route to harness handlers, never to `EventOnCommand`. Extension command names cannot collide (the prefix is reserved). Reserved callbacks: `"__wllr:model"`, `"__wllr:thinking"`.

**Invariant:** `SelectModelFn` errors surface as a notification and leave the active model unchanged; `activeModel`/status update only after a successful switch.

### Thinking-Level Hooks

`harness.Model` exposes two callback fields for the `/thinking` picker (set by `cmd/main.go`):

| Field | Type | Purpose |
|-------|------|---------|
| `ThinkingListFn` | `func() []ThinkingChoice` | Returns the selectable reasoning levels for the picker. Nil ⇒ selection unavailable. |
| `SelectThinkingFn` | `func(levelID string) error` | Applies a reasoning level: sets the main agent's provider options (`Agent.SetProviderOptions`) and persists the choice. Nil ⇒ display-only. |

Flow mirrors the model picker: `/thinking` with no arg emits `showThinkingPickerMsg` → `openThinkingPicker()` builds items from `ThinkingListFn` (marking the current level) and opens the picker with the reserved `thinkingPickerCallback` (`"__wllr:thinking"`). On selection, `updateKeyPressPicker` emits `setThinkingMsg{Level: id}`; the handler calls `applyThinkingSelection` → `SelectThinkingFn` + `activeThinking`/status (`think` key) update. `/thinking <level>` skips the picker. `SetActiveThinking(level)` reflects a persisted level at startup without changing agent options.

**Invariant:** `SelectThinkingFn` errors surface as a notification and leave the active level unchanged; `activeThinking`/status update only after a successful apply.

### First-Run Provider Auth Prompt

`harness.Model` supports a one-time, per-provider auth-method prompt shown at startup:

| Field / Method | Type | Purpose |
|----------------|------|---------|
| `ProviderListFn` | `func() []ProviderChoice` | Returns providers selectable in the blank first-run setup wizard. Nil ⇒ selection unavailable. |
| `SelectProviderFn` | `func(provider string) (model string, requiresLogin bool, err error)` | Applies a wizard provider and its default model. Returns whether OAuth should start. Nil ⇒ display-only. |
| `RecordAuthFn` | `func(provider, method string) error` | Persists the chosen auth method for a provider (so the prompt is not shown again). Nil ⇒ choice not persisted. |
| `SetPendingAuthProvider(provider)` | method | Marks a provider as needing the first-run prompt. Called by `cmd/main.go` only when no auth choice is recorded for the provider. Must be called before `prog.Run()`. |
| `SetPendingSetupWizard()` | method | Marks startup as needing the blank first-run setup wizard. Used by `cmd/main.go` when provider/model are both defaults and credentials are unavailable. Must be called before `prog.Run()`. |
| `SetPendingModelPicker()` | method | Marks startup as needing confirmation after a configured local model is unavailable and has been replaced. Must be called before `prog.Run()`. |

Flow: when `pendingAuthProvider != ""`, `Init()` emits `showAuthPromptMsg{Provider}` → `openAuthPrompt()` opens a two-item picker ("Set up OAuth / login" = `"oauth"`, "Use an API key" = `"api_key"`) with the reserved `authPickerCallback` (`"__wllr:auth"`). On selection, `updateKeyPressPicker` emits `recordAuthMsg{Provider, Method}`; the handler calls `applyAuthChoice` → `RecordAuthFn` + a notification, and clears `authPromptProvider`.

Provider setup flow: when `pendingSetupWizard` is true, or when `/login` is run, the harness emits `showLoginProviderPickerMsg{}`. `openLoginProviderPicker()` displays provider choices from `ProviderListFn` with the reserved `loginProviderPickerCallback` (`"__wllr:login_provider"`). Cloud selection calls `SelectProviderFn`, updates active provider/model state, dispatches `EventModelChanged`, and starts OAuth. Local selection always opens `showLocalModelSetupMsg{}` so the endpoint/model wizard can add or switch a local model; `/model` remains the shortcut for choosing among already configured local models.

Local model replacement flow: when the configured local model is not advertised by its endpoint, startup selects an available replacement so provider construction can continue, then sets `pendingModelPicker`. `Init()` emits `showModelPickerMsg{}` and the existing model picker opens with an explicit unavailable-model message so the user can confirm or choose another available model; selecting a model uses `SelectModelFn` and persists the choice. The pending flag is consumed when that picker opens.

**Invariant:** the prompt is shown at most once per provider — `cmd/main.go` gates `SetPendingAuthProvider` on the absence of a recorded auth choice (credential presence in the auth file is the record). Cancelling the picker records nothing, so the prompt reappears next launch.

**Invariant:** the setup wizard is only selected by `cmd/main.go` for blank first-run config and missing credentials. Cloud choices use the same OAuth completion path as `/login`; local choices do not record an OAuth auth choice and do not call `BeginOAuthFn`.

**Invariant:** `"__wllr:auth"` and `"__wllr:login_provider"` join `"__wllr:model"`/`"__wllr:thinking"` as reserved core-owned picker callbacks; they never dispatch `EventOnCommand`.

### show_text_input Overlay

`TextInputView` is a fullscreen overlay text input shown instead of the chat, mirroring `PickerView`'s structure but backed by `charm.land/bubbles/v2/textinput.Model` instead of a list.

| Method | Behavior |
|--------|----------|
| `Open(title, placeholder, initialValue, callback string)` | Resets and configures the underlying `textinput.Model` (`Placeholder`, `SetValue(initialValue)`, cursor to end), calls `Focus()`, and activates the overlay. |
| `Close()` | Deactivates the overlay and clears `Callback`. |
| `IsActive() bool` | Reports whether the overlay is currently shown. |
| `SetSize(width, height int)` | Updates overlay dimensions and the inner `textinput.Model` width. |
| `HandleKey(kp tea.KeyPressMsg) (submitted bool, value string, cancelled bool, cmd tea.Cmd)` | Enter submits with the current value; Esc cancels; every other key is forwarded to `textinput.Model.Update` and its `tea.Cmd` is returned so cursor blink/paste commands keep working. |
| `View() string` | Bordered box matching `PickerView`'s visual style (`pickerBorderStyle`/`pickerTitleStyle`/`pickerLabelStyle`), rendering the title, the input line, and a footer hint (` enter submit · esc cancel `). |

`ShowTextInputMsg{Title, Placeholder, InitialValue, Callback}` is the bubbletea message `harnessUIBridge.ShowTextInput` sends to open the overlay; the `Model.Update` `ShowTextInputMsg` case opens `m.textInput` and sizes it against `m.chatHeight()`, matching the `ShowPickerMsg` case.

`updateKeyPressTextInput` mirrors `updateKeyPressPicker`: on cancel it closes the overlay and returns; on submit it closes the overlay, then — before falling through to the generic extension dispatch — checks core-owned `"__wllr:"`-prefixed callbacks (`localModelBaseURLCallback`, `localModelManualFieldCallback`; see "Local-Model Setup Flow" below), the same structural slot `updateKeyPressPicker` uses for `modelPickerCallback`/`thinkingPickerCallback`/`authPickerCallback`/`loginProviderPickerCallback`. Any callback not recognized as core-owned dispatches `EventOnCommand` with `sdk.OnCommandPayload{Name: callback, Args: []string{value}}` to `m.extHost`, exactly like the picker's extension-owned fallback.

**Invariant:** `m.textInput.IsActive()` is checked before `m.picker.IsActive()` in both key routing (`updateKeyPress`) and rendering (`View`), so the two overlays are mutually exclusive and text input takes priority if both were somehow opened.

**Invariant:** `show_text_input` (`MethodShowTextInput`) requires no permission, matching `show_picker`.

### Local-Model Setup Flow

When the local provider has no configured/discoverable model, `SelectProviderFn` returns the sentinel `ErrLocalModelSetupNeeded` instead of an error, and `applyLoginProviderSelection` (in `providerpicker.go`) checks `errors.Is(err, ErrLocalModelSetupNeeded)` *before* its generic error-notification path, redirecting into an interactive setup flow (`localmodelsetup.go`) rather than failing. `/login` opens this provider picker on demand; choosing Local therefore reaches the same setup flow as the install wizard. `/login auth` remains the explicit active-provider authentication path.

| Field | Type | Purpose |
|-------|------|---------|
| `ProbeLocalModelsFn` | `func(baseURL string) (models []LocalModelChoice, resolvedBaseURL string, status LocalModelProbeStatus)` | Probes for an OpenAI-compatible model list, trying a small set of common path conventions against `baseURL` (bare `/models`, then `/v1/models`, then `/api/v1/models`) so a user typing a bare host:port still resolves. `resolvedBaseURL` is the base URL that actually worked — it may differ from `baseURL` and is what gets persisted, not necessarily the literal input. Nil ⇒ treated as `LocalModelProbeUnreachable`. |
| `SaveLocalModelFn` | `func(entry LocalModelEntry) (modelID string, err error)` | Persists the chosen/entered model to disk (`wllr.local_models`) and rebuilds the active provider. Nil ⇒ setup surfaces a "not available" notification instead of saving. |
| `HasLocalModelFn` | `func() bool` | Reports whether the local provider currently has a usable model. Nil ⇒ assume true (no `/login` redirect). |

`LocalModelProbeStatus` classifies why a probe did not yield models, distinguishing a wrong/unreachable endpoint from one that responded with nothing usable: `LocalModelProbeOK` (at least one model found), `LocalModelProbeUnreachable` (the request itself failed — bad URL, connection refused, timeout, DNS failure), `LocalModelProbeEmpty` (the endpoint responded but returned no usable models).

Discovery-success flow: base-URL prompt (`openLocalModelBaseURLPrompt`, callback `localModelBaseURLCallback`) → `localModelBaseURLEnteredMsg` stores `localSetupBaseURL` and returns `probeLocalModelsCmd` as an async `tea.Cmd` → `localModelProbeResultMsg{Status: LocalModelProbeOK, Models, BaseURL: resolved}` re-assigns `localSetupBaseURL` to the message's (possibly path-corrected) `BaseURL` before opening a picker of discovered models (`openModelPickerFromProbe`, callback `localModelPickerCallback`) built from the stashed `localSetupModels` → picker selection resolves the full choice from `localSetupModels` by ID and emits `localModelPickedMsg` → `applyLocalModelPick` calls `SaveLocalModelFn`, notifies, and on success updates active provider/model state via `setActiveProviderModel`.

Unreachable-endpoint flow: `localModelProbeResultMsg{Status: LocalModelProbeUnreachable}` clears `localSetupBaseURL`, pushes an error notification naming the unreachable URL, and re-opens the base-URL prompt (`openLocalModelBaseURLPrompt`) — it does **not** fall into manual entry, since the failure is almost always a wrong host/port rather than a server with no models.

Manual-fallback flow: `localModelProbeResultMsg{Status: LocalModelProbeEmpty}` resets `localSetupManualStep` to 0, seeds `localSetupManualEntry.BaseURL`, and opens the first of four sequential text-input prompts (`localModelManualFields`: model ID, display name, context window, optional API key) via `localModelManualFieldCallback`. Each `localModelManualFieldEnteredMsg` fills the corresponding `localSetupManualEntry` field (empty model ID re-prompts the same step instead of advancing; empty display name defaults to the model ID; context window is parsed best-effort via `parseContextWindowLoose`, defaulting to 0 rather than erroring; API key is always optional) and advances to the next field, or — after the fourth — calls `applyLocalModelPick` exactly as the discovery path does.

**Invariant:** cancelling (`Esc`) the base-URL prompt or any manual field calls `resetLocalModelSetupState`, zeroing all `localSetup*` fields so a later attempt starts fresh; cancelling the discovered-models picker does the same. Cancelling any *other* picker/text-input does not touch this state.

**Invariant:** an unreachable endpoint (`LocalModelProbeUnreachable`) always re-prompts for the base URL and never falls back to manual entry; only a reachable endpoint with no usable models (`LocalModelProbeEmpty`) triggers manual entry. This keeps a mistyped/wrong-port URL from silently turning into a request to hand-type model metadata for a server that was never actually reached.

**Invariant:** `providerLocal` is duplicated as a harness-local constant (`localmodelsetup.go`) since `harness` cannot import `cmd/` (which imports `harness`); it must be kept in sync with `cmd/provider.go`'s constant of the same name.

### OAuth Login Flow

When the user chooses **OAuth** in the auth prompt (or runs `/login auth`), the harness drives an interactive OAuth login via two callbacks (set by `cmd/main.go`):

| Field | Type | Purpose |
|-------|------|---------|
| `BeginOAuthFn` | `func(provider string) (modalBody, clipboard string, err error)` | Starts login and returns the modal body to show (provider-specific sign-in instructions) plus a string to copy to the clipboard (the URL). Nil ⇒ login unavailable. |
| `CompleteOAuthFn` | `func(provider, input string) error` | Exchanges the awaited material (per provider) for tokens, persists and applies them. Nil ⇒ completion unavailable. |
| `AwaitOAuthFn` | `func() (input string, ok bool)` | Blocks until login resolves and returns the material `CompleteOAuthFn` needs (+ true), or false on cancel/error. Backed by the local callback server (Anthropic) or the device-code poll (Codex). Nil ⇒ login unavailable. |

Flow: selecting OAuth (or `/login auth` → `loginMsg` → `openAuthPrompt(activeProvider)`) leads to `beginOAuthLogin` → `BeginOAuthFn` returns the modal body + URL. The body is shown in a modal and the URL copied to the clipboard (`tea.SetClipboard`/OSC52). `beginOAuthLogin` returns a `tea.Batch` of a command that blocks on `AwaitOAuthFn` (yielding `oauthCallbackMsg`) and the clipboard copy. Login completes automatically when `AwaitOAuthFn` resolves: `oauthCallbackMsg{OK:true}` → `completeOAuthFromCallback` closes the modal and runs `CompleteOAuthFn` off-loop, reporting success/failure as a `NotifyMsg`. There is **no manual-paste fallback**. The two provider styles:

- **Anthropic** — browser + local callback server on `127.0.0.1:53692`; the browser must reach it (same machine, or tunnel the port).
- **Codex (openai)** — device-code: the modal shows a verification URL + user code; `AwaitOAuthFn` polls until approval. Works anywhere (incl. SSH), no local server.

`/login` is available any time, not just first run. It opens the provider wizard; use `/login auth` for direct authentication of the active provider.

**Invariant:** OAuth login requires both `BeginOAuthFn` and `AwaitOAuthFn`; if either is nil, `/login auth`'s OAuth path reports "not available" and does not enter capture mode. A normal `SubmitMsg` is never repurposed as an OAuth code.

**Invariant:** OAuth login is provider-scoped; `BeginOAuthFn`/`CompleteOAuthFn` return an error for providers without a supported flow (today Anthropic and OpenAI/Codex), surfaced as a notification.

---

## 26. SceneRenderer — Declarative, Extension-Driven UI (UI P1)

`harness.SceneRenderer` is a goroutine-safe, data-driven renderer of the `sdk` scene graph (`UINode`/`UIPatchOp`/`UIArea`). It lets any WASM extension paint the TUI without touching harness rendering code.

| Method | Behavior |
|--------|----------|
| `CreateArea(sdk.UIArea) error` | Registers a named area; errors on empty or duplicate ID. |
| `RemoveArea(id string)` | Deletes an area and its tree; no-op if missing. |
| `UpdateArea(sdk.UIUpdateAreaParams) error` | Updates constraints/weight of an existing area; errors if ID not found. Omitted fields leave current values unchanged. |
| `ApplyPatch(sdk.UIPatchParams) error` | Applies an op batch atomically to an area; rejects the whole batch (live tree unchanged) if the area or any referenced node is missing. |
| `Render(areaID string, width int) string` | Renders an area's tree to a string via lipgloss; `""` for unknown/empty areas. |
| `RenderNode(areaID, nodeID string, width int, textOverride *string) (string, bool)` | Renders one node with optional text override for append fast paths; does not mutate the live scene. |
| `RenderAppendTextNode(areaID, nodeID string, width int, appendedText string) (previous, current string, ok bool)` | Renders previous/current states of one appended text node with one scene lookup; does not mutate the live scene. |
| `AreasByPlacement(p) []string` | Area IDs with a placement, in creation order. |
| `ConstrainWidth(id string, termWidth int) int` | Returns the render width clamped to the area's MinWidth/MaxWidth constraints resolved against `termWidth`. Passthrough for unknown areas or absent constraints. |
| `ConstrainHeight(id string, lines int, termHeight int) int` | Returns `lines` clamped to the area's MinHeight/MaxHeight constraints resolved against `termHeight`. Passthrough for unknown areas or absent constraints. |

**Constraint resolution:** values accept `"N"` (absolute cells/lines) or `"N%"` (percentage of the terminal dimension). Empty string is unconstrained. Percentages are integer division: `termDim * N / 100`.

**Invariants:**

- `ApplyPatch` validates against a working clone; a mid-batch failure leaves the live tree untouched (atomicity).
- `UIOpAppendText` targets only `UINodeText` nodes; appending to a non-text node errors.
- An unknown `UINodeType` renders as an empty box (forward-compatibility).
- Colour props resolve through `themeColor` (named tokens → hex); unknown non-hex tokens yield no colour, so the host keeps theming control.
- The `harnessUIBridge` shares the `Model.scene` pointer, mutates it synchronously off the bubbletea loop (the renderer is mutex-guarded), and sends `sceneDirtyMsg{Area, AppendOnly, AppendID, AppendText}` to force a re-render. `sceneDirtyMsg` refreshes the chat viewport immediately only when `Area == "chat"` and the change is structural, or when the area is unknown/empty. Append-only chat patches schedule a delayed coalesced refresh; other areas only trigger `View`.
- `ConstrainWidth` and `ConstrainHeight` never return negative values.
- `UpdateArea` with an empty string for a constraint field leaves that constraint unchanged (does not clear it). To clear a constraint, use `"0%"` or remove the area and re-create it.
- `UIAreaStatus` areas are NOT rendered by `renderScenes()`; they are composited by `renderStatusLine()` between the console pane and the input box (see §28).
- `renderScenes()` covers `UIAreaMain`, `UIAreaSidebar`, and `UIAreaOverlay` only.

---

## 27. WASM-Driven Chat Transcript (UI P4)

---

## 28. Scene-Driven Statusline (statusline area)

The statusline is a standalone row rendered above the input box, driven entirely by the
scene graph. The `statusline` scene area (`statuslineAreaID = "statusline"`, placement
`UIAreaStatus`) is pre-created by the harness in `New()` so the slot is reserved before
`session_start` fires. The bundled `statusline` WASM extension owns the content.

### Layout methods

| Method | Behavior |
|--------|----------|
| `statusLineHeight() int` | Sums constrained heights of all `UIAreaStatus` areas. Called by `chatHeight()` and `View()`. |
| `renderStatusLine() string` | Renders all `UIAreaStatus` areas; each clamped to `MaxHeight`. Inserted between console pane and input box in `View()`. |

### Input box border

The input box top border is now a plain ruled line `╭──────╮` with no embedded
status text. All status content lives in the statusline scene area above it.

### `StatusUpdateMsg` routing

`StatusUpdateMsg{Key, Value}` calls `m.live.setStatus(key, value)`. `get_status_info`
is served entirely from `liveState` via `harnessUIBridge.GetStatusInfo` (provider,
model, tokens, statuses, working, elapsed, active-agent count). The `StatusBar` struct
has been removed; no parallel status state exists.

`New()` seeds `activeProvider`/`live.provider` from `AgentPool.ProviderName()` and seeds
`activeModel`/`live.model` from the spawned main agent's `ModelName()` when available.
`Init()` dispatches `EventModelChanged` with that initial state. Runtime provider/model
selection paths update live state first, then dispatch `EventModelChanged`.

**Invariants:**

- The `statusline` area is always present from `New()` onwards (empty tree until the
  extension sets a root on `session_start`).
- `statusLineHeight()` returns 0 for empty areas, so layout is unaffected before the
  extension initialises.
- `UIAreaStatus` areas are excluded from `renderScenes()` to prevent double-rendering.
- There is no `StatusBar` struct; all status state lives in `liveState` and the
  `statusline` scene area. `get_status_info` reads `liveState` directly.

The main chat transcript content is produced by a WASM extension (the bundled `agents` extension) that owns the `wasmChatAreaID` (`"chat"`) scene area. The harness owns the scrollable viewport; the *content* is always external. There is no built-in message renderer.

- `Model.refreshWASMChat()` is called immediately on structural `sceneDirtyMsg{Area:"chat"}` and on `WindowSizeMsg`; append-only chat dirty messages coalesce into a delayed refresh. When the append batch targets one trailing text node, `refreshWASMChatAppend()` renders only that node with previous/current text and splices the cached viewport suffix; mixed targets or non-suffix layouts fall back to full `scene.Render("chat", width)`. Once the `chat` area exists, refresh feeds content into the chat viewport via `ChatView.SetExternalContent`. Non-chat scene updates such as the statusline must not refresh the chat viewport.
- **Invariant (#30):** On `StreamDoneMsg`, the handler clears any pending append-only refresh state and calls `refreshWASMChat()` to force a structural full render. This guarantees the visible transcript contains the complete response (including the final tail) at stream completion even if the WASM extension emits no structural op (empty content, malformed extension, or a race with a pending append refresh). The scene tree and session file may be complete while the viewport lags; this force-refresh closes that gap.
- `Model.resetChatArea()` (used by `/clear` and history-restore) patches the `chat` area root back to an empty `vstack` (`"chat-root"`, matching the structure the extension expects) and clears the viewport.
- `Model.streamContent` accumulates streamed assistant text from `TokenMsg` so the completed response can be captured for `OnMessageEnd`/logging; it is reset on `StreamDoneMsg` and `/clear`.
- `Model.pushNotification(text)` dispatches `sdk.EventNotify` (in a goroutine) so the transcript-owning extension renders notifications; it no longer writes to `ChatView`.
- `renderScenes` always skips the `chat` area (it is rendered inside the viewport, not stacked below it). Non-chat scene areas are rendered above the chat viewport; their height is subtracted by `chatHeight()` so the chat history viewport is what shrinks when extra UI appears.
- `renderToolActivity()` renders a persistent pane below the chat viewport with the latest three tool call rows. When no tools have run this turn, the pane renders as three empty content rows. Rows for non-main agents include the agent ID so sub-agent activity is distinguishable from main-agent tool calls.

**Invariants:**

- The viewport (scroll, size, tail-follow behavior) is harness-owned; input/scroll never route to WASM.
- If no extension creates the `chat` area (e.g. the `agents` extension is not loaded), `refreshWASMChat` no-ops and the viewport is empty — there is no fallback renderer.
- `/clear` and history-restore reset the transcript area to empty; restored history remains in agent context but is not re-rendered into the transcript.
- The per-turn tool log is cleared at turn start (`submitToAgent`) and surfaced via `/tools`; it is independent of the transcript and also feeds the persistent tool activity pane.
