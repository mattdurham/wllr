# extension — Interface Contracts and Behavioral Invariants

Package `extension` implements a wazero-based WASM extension host for the Bob harness.
Extensions are compiled WASM modules that communicate with the host via a JSON-over-linear-memory ABI.

---

## 1. Required WASM Exports

Every `.wasm` extension **must** export the following four symbols. `validateExports` enforces this at load time and returns an error if any are absent.

| Export | Signature (WAT) | Semantics |
|--------|-----------------|-----------|
| `_init` | `() -> i32` | Called once after the module is instantiated. Return 0 on success; any other value is an error and causes load to fail. |
| `_on_event` | `(ptr i32, len i32) -> (ptr i32, len i32)` | Dispatch an event (ABI v2). `ptr`/`len` point to a JSON-encoded `sdk.Event` in WASM linear memory. Returns `(respPtr, respLen)` pointing to a JSON-encoded `sdk.EventResponse` in WASM memory, or `(0, 0)` if there is no response. ABI v1 (single i32 return) is accepted for backward compatibility. |
| `_alloc` | `(size i32) -> i32` | Allocate `size` bytes in WASM linear memory and return the pointer. Return 0 to signal allocation failure. |
| `_free` | `(ptr i32)` | Free memory previously allocated by `_alloc`. |

**Invariant:** A missing required export causes `Load` to close the module and return an error. No partial registration occurs.

---

## 2. Host Module "env" Imports

The host registers a module named `"env"` that extensions may import. All four functions are available; extensions that do not import them will still load successfully.

| Import | Signature (WAT) | Semantics |
|--------|-----------------|-----------|
| `host_log` | `(level i32, ptr i32, len i32)` | Write a log message. `ptr`/`len` point to a UTF-8 string in WASM memory. `level`: 0=debug, 1=info, 2=warn, 3=error. Logged via slog with the extension's friendly display name (base filename without `.wasm`). |
| `host_alloc` | `(size i32) -> i32` | Reserved for ABI v2. In ABI v1 this is a no-op that always returns 0. |
| `host_free` | `(ptr i32)` | Reserved for ABI v2. In ABI v1 this is a no-op. |
| `host_call` | `(req_ptr i32, req_len i32, resp_ptr_ptr i32, resp_len_ptr i32) -> i32` | Synchronous host RPC. `req_ptr`/`req_len` point to a JSON-encoded `sdk.HostCallRequest`. On success writes the response pointer and length into `resp_ptr_ptr` / `resp_len_ptr` (if both are non-zero) and returns 0 (`sdk.ErrOK`). Returns a non-zero error code on failure. |

**Invariant:** The "env" module is instantiated once at `NewHost` time; all subsequently loaded extensions share the same host bindings.

---

## 3. Extension Lifecycle

```
NewHost
  └─ wazero.NewRuntime
  └─ wasi_snapshot_preview1.Instantiate   (WASI support for native Go WASM)
  └─ installEnvModule                     (registers "env" host bindings)
  └─ h.dispatch = h.buildDispatch()       (builds method→handler map once)

Host.Load(path)
  ├─ os.ReadFile
  ├─ loadManifestPermissions              (reads <basename>.json for permissions)
  ├─ h.loadMu.Lock()                      (serializes concurrent loads — wazevo JIT is not concurrency-safe)
  ├─ runtime.InstantiateWithConfig        (WithStartFunctions() — no auto _start/_main)
  ├─ h.loadMu.Unlock()
  │   ├─ WithFSConfig: WithDirMount("/", "/")   (full filesystem read/write access)
  │   └─ WithEnv: all host env vars passed through
  ├─ validateExports                      (abort + close if any export missing)
  ├─ Register ext in h.extensions         (before callInit so host_call works during _init)
  └─ callInit
       ├─ optional: _initialize()         (native Go WASM bootstrap, if exported)
       └─ _init() -> i32                  (non-zero return removes ext and fails load)

Host.LoadBytes(ctx, name, data, trusted)
  └─ Same as Load but skips manifest loading; trusted=true grants all permissions

DispatchEvent loop  (called repeatedly by the harness)
  └─ see §4

Host.Reload(paths)
  ├─ Close all current extensions
  └─ Load(path) for each path

Host.Close
  └─ runtime.Close                        (closes all modules)
```

**Invariant:** `ext` is added to `h.extensions` before `callInit` so that `host_call` requests made during `_init` (e.g. `subscribe`, `register_tool`) can resolve the calling extension via `findExtensionByModule`.

**Invariant:** If `_init` returns a non-zero code or traps, `removeExtension` is called and the module is closed. `Load` returns an error.

---

## 4. DispatchEvent Contract

`Host.DispatchEvent(ctx, evt)`:
1. Publishes `evt` to the `EventBus` (`h.Bus.Publish`) — fire-and-forget.
2. Iterates all loaded extensions in load order and calls `_on_event` on each that has subscribed to `evt.Type`.

**Rules:**
1. Only extensions whose `subscriptions[evt.Type] == true` receive the WASM call (guarded by `ext.subMu`).
2. If `_alloc` returns 0 for the event buffer, `_on_event` is **NOT** called for that extension.
3. If `_on_event` returns a WASM trap (error from wazero), it is logged at warn level and dispatch continues to the next extension. The error is **not** propagated to the caller.
4. Responses are collected in load order (same order as `h.extensions`).
5. `DispatchEvent` itself only returns a non-nil error if `json.Marshal(evt)` fails; individual extension errors do not bubble up.

**Memory flow during dispatch (per extension):**
```
host calls _alloc(len(evtJSON))  →  evtPtr
host writes evtJSON to evtPtr
host calls _on_event(evtPtr, len(evtJSON))  →  (respPtr, respLen)  [ABI v2]
                                            or →  respPtr             [ABI v1 fallback]
host calls _free(evtPtr)              ← host owns evtPtr; extensions must NOT free it
if respPtr != 0:
    if respLen > 0 (ABI v2): host reads respLen bytes from respPtr
    else (ABI v1): host uses readNullTerminatedOrJSON to find JSON boundary (up to 64KB)
    host calls _free(respPtr)         ← host frees respPtr after reading
```

**ABI v2 (C5 fix):** `_on_event` should return `(ptr i32, len i32)` — two i32 values. This allows responses larger than 64KB and eliminates the JSON boundary scan. ABI v1 (single i32 return) is still accepted by the host for backward compatibility with extensions compiled before ABI v2.

**Memory ownership invariant (H-abi1):**
- `evtPtr` is allocated by the host (via `ext._alloc`) and freed by the host (via `ext._free`) after `_on_event` returns. Extensions must NOT free `evtPtr` inside `_on_event` — doing so would double-free the buffer.
- `respPtr` is allocated by the extension inside `_on_event` and returned as the result value. The host frees `respPtr` after reading the response JSON. Extensions must NOT free `respPtr` after returning it.
- The host always frees `evtPtr` via `defer` regardless of whether `_on_event` succeeds or traps.

---

## 5. EventBus

`EventBus` is the shared in-process event stream exposed on `Host.Bus`. All `DispatchEvent` calls publish to it in addition to calling WASM extensions.

```go
type EventBus struct { ... }

func NewEventBus() *EventBus
func (b *EventBus) Subscribe(eventType sdk.EventType, h Handler)
func (b *EventBus) Unsubscribe(eventType sdk.EventType)
func (b *EventBus) HasSubscribers(eventType sdk.EventType) bool
func (b *EventBus) Publish(ctx context.Context, evt sdk.Event)
```

```go
type Handler func(ctx context.Context, evt sdk.Event) error
```

**Invariant:** `Publish` is a no-op when no handlers are registered for `evt.Type`. The `HasSubscribers` check is O(1) via the separate `counts` map.

**Invariant:** Handlers are invoked asynchronously in a single goroutine spawned by `Publish`. Errors returned by handlers are silently ignored (fire-and-forget).

**Invariant:** `Subscribe` and `Unsubscribe` are thread-safe via `b.mu sync.RWMutex`. `Publish` copies the handler slice under `RLock` before dispatching, so handlers registered or removed after `Publish` is called are not affected by that invocation.

---

## 6. Dispatch Map (routeHostCall)

`Host` uses a pre-built `dispatch map[string]func(...)` (constructed once in `NewHost` via `buildDispatch`) to route incoming `host_call` requests. `routeHostCall` performs a single map lookup and delegates to the matched handler. Unknown methods return `{Error: "unknown method: <name>"}`.

The full set of dispatched methods is:

| Method constant                | Handler                        |
|-------------------------------|--------------------------------|
| `MethodSubscribe`             | `handleSubscribe`              |
| `MethodRegisterTool`          | `handleRegisterTool`           |
| `MethodRegisterCommand`       | `handleRegisterCommand`        |
| `MethodSendMessage`           | `handleSendMessage`            |
| `MethodSetStatus`             | `handleSetStatus`              |
| `MethodNotify`                | `handleNotify`                 |
| `MethodToolResult`            | `handleToolResult`             |
| `MethodStoreSet`              | `handleStoreSet`               |
| `MethodStoreGet`              | `handleStoreGet`               |
| `MethodAbort`                 | calls `OnAbort` directly       |
| `MethodRequestPermission`     | `handleRequestPermission`      |
| `MethodModal`                 | `handleModal`                  |
| `MethodSetSystemPrompt`       | `handleSetSystemPrompt`        |
| `MethodAppendSystemPrompt`    | `handleAppendSystemPrompt`     |
| `MethodExec`                  | `handleExec`                   |
| `MethodGetEnv`                | `handleGetEnv`  (requires `PermEnvRead`) |
| `MethodConfigRead`            | `handleConfigRead`             |
| `MethodAgentSpawn`            | `handleAgentSpawn`             |
| `MethodAgentClose`            | `handleAgentClose`             |
| `MethodAgentSendMessage`      | `handleAgentSendMessage`       |
| `MethodAgentList`             | `handleAgentList`              |
| `MethodAgentTokenCount`       | `handleAgentTokenCount`        |
| `MethodTeamCreate`            | `handleTeamCreate`             |
| `MethodTeamClose`             | `handleTeamClose`              |
| `MethodTeamAddMember`         | `handleTeamAddMember`          |
| `MethodTeamRemoveMember`      | `handleTeamRemoveMember`       |
| `MethodReadFile`              | `handleReadFile` (requires `PermFileRead`) |
| `MethodWriteFile`             | `handleWriteFile` (requires `PermFileWrite`) |
| `MethodHTTPPost`              | `handleHTTPPost` (requires `PermNetworkWrite`) |
| `MethodAgentRun`              | `handleAgentRun`               |
| `MethodShowPicker`            | `handleShowPicker`             |
| `MethodAgentResetHistory`     | `handleAgentResetHistory`      |
| `MethodGetOS`                 | `handleGetOS`                  |
| `MethodGetStatusInfo`         | `handleGetStatusInfo`          |
| `MethodSetStatusLine`         | `handleSetStatusLine`          |
| `MethodTeamGetInfo`           | `handleTeamGetInfo`            |
| `MethodTeamList`              | `handleTeamList`               |
| `MethodMCPSpawn`              | `handleMCPSpawn`               |
| `MethodMCPClose`              | `handleMCPClose`               |
| `MethodMCPSend`               | `handleMCPSend`                |
| `MethodMCPRead`               | `handleMCPRead`                |

---

## 7. Host Callback Fields

`Host` exposes a set of function fields that the harness wires at startup. Each field is nil-checked before invocation; missing callbacks result in an error response.

### Harness callbacks

| Field                  | Signature                                            | Purpose                                                  |
|-----------------------|------------------------------------------------------|----------------------------------------------------------|
| `OnSendMessage`       | `func(msg sdk.Message)`                              | Inject a message into the conversation                   |
| `OnSetStatus`         | `func(key, value string)`                            | Update a keyed status in the TUI status bar              |
| `OnRegisterTool`      | `func(tool sdk.Tool) error`                          | Called when an extension registers a tool                |
| `OnRegisterCommand`   | `func(name, desc string)`                            | Called when an extension registers a slash command       |
| `OnNotify`            | `func(text string)`                                  | Show a notification in the chat view                     |
| `OnAbort`             | `func()`                                             | Cancel the current agent turn                            |
| `OnToolResult`        | `func(toolCallID, result string, isError bool)`      | Called when `tool_result` is received (all paths)        |
| `OnAfterToolCall`     | `func(toolCallID, toolName, result string, isError bool)` | Called after `EventAfterToolCall` dispatch           |
| `OnModal`             | `func(text string)`                                  | Display text in a modal overlay                          |
| `OnSetSystemPrompt`   | `func(prompt string)`                                | Replace the base system prompt on all agents             |
| `OnAppendSystemPrompt`| `func(text string)`                                  | Append to the base system prompt on all agents           |
| `OnExec`              | `func(ctx context.Context, command, dir string, onLine func(string)) (string, error)` | Execute a shell command (requires PermExec). `ctx` propagates cancellation. `onLine` is called for each output line as it arrives; may be nil. |
| `OnConsoleOutput`     | `func(line string)`                                  | Called for each output line streamed from OnExec; nil-safe.          |
| `OnConsoleClear`      | `func()`                                             | Called at the start of each OnExec to signal the console should be cleared. |
| `OnGetEnv`            | `func(name string) (string, error)`                  | Read a host environment variable (requires PermEnvRead for untrusted extensions) |
| `OnReadFile`          | `func(path string) (string, error)`                  | Read a file from the host filesystem (requires PermFileRead) |
| `OnWriteFile`         | `func(path, content string) error`                   | Write a file to the host filesystem (requires PermFileWrite) |
| `OnHTTPPost`          | `func(url string, headers map[string]string, body []byte) (int, []byte, error)` | HTTP POST via host (requires PermNetworkWrite) |
| `OnConfigRead`        | `func(group string) (json.RawMessage, error)`        | Read config for the named extension group                |

### Agent management callbacks

| Field                  | Signature                                                    | Purpose                                         |
|-----------------------|--------------------------------------------------------------|-------------------------------------------------|
| `OnAgentSpawn`        | `func(id, name, systemPrompt, modelName, initialPrompt string, thinkingBudget int) error` | Create and register a new sub-agent; if initialPrompt is non-empty, calls `pool.Send` to start the first turn immediately |
| `OnAgentClose`        | `func(id string) error`                                      | Cancel and remove a sub-agent                   |
| `OnAgentSendMessage`  | `func(id, message string) error`                             | Send a message to a named agent                 |
| `OnAgentRun`          | `func(id string) error`                                      | Trigger an immediate turn for an existing agent |
| `OnAgentList`         | `func() ([]AgentInfo, error)`                                | Return all live agent IDs and names             |
| `OnAgentTokenCount`   | `func() int64`                                               | Return total token count across all agents      |
| `OnAgentResetHistory` | `func(messages []sdk.Message) error`                         | Replace the main agent's conversation history and rebuild the chat view |
| `OnGetStatusInfo`     | `func() sdk.StatusInfo`                                      | Return a snapshot of the current status bar state |
| `OnShowPicker`        | `func(title string, items []sdk.ShowPickerItem, callback string)` | Open the interactive TUI list picker          |

### Team management callbacks

| Field                  | Signature                                        | Purpose                                      |
|-----------------------|--------------------------------------------------|----------------------------------------------|
| `OnTeamCreate`        | `func(id, name string) error`                    | Create a new named team                      |
| `OnTeamClose`         | `func(id string) error`                          | Cancel all members and remove the team       |
| `OnTeamAddMember`     | `func(teamID, agentID string) error`             | Add an agent to a team                       |
| `OnTeamRemoveMember`  | `func(teamID, agentID string) error`             | Remove an agent from a team (no cancel); no-op if team does not exist |
| `OnTeamGetInfo`      | `func(teamID string) ([]string, error)`          | Return member agent IDs for a team           |
| `OnTeamList`         | `func() ([]string, error)`                       | Return all registered team IDs               |

### MCP bridge callbacks

| Field          | Signature                                                      | Purpose                                      |
|---------------|----------------------------------------------------------------|----------------------------------------------|
| `OnMCPSpawn`  | `func(id, command string, args []string, env map[string]string) error` | Spawn an MCP server subprocess    |
| `OnMCPClose`  | `func(id string) error`                                        | Terminate an MCP server subprocess           |
| `OnMCPSend`   | `func(id string, data []byte) error`                           | Write JSON-RPC data to MCP stdin             |
| `OnMCPRead`   | `func(id string) (json.RawMessage, error)`                     | Read a JSON-RPC response from MCP stdout     |

**Invariant:** `OnMCPSpawn` requires the calling extension to hold `PermExec`. If the extension is nil or lacks `PermExec`, the call returns a permission-denied error.

**Invariant:** `OnExec` requires the calling extension to hold `PermExec`. Executions by trusted extensions always pass the permission check.

---

## 8. Memory Protocol

**Host → Extension (event delivery):**
- Host calls `_alloc(n)` to request `n` bytes from the extension's allocator.
- Host writes the event JSON to the returned pointer.
- If `_alloc` returns 0, the write and `_on_event` call are skipped entirely.
- After reading the response, the host calls `_free(evtPtr)` to release the input buffer.

**Extension → Host (response):**
- The extension allocates its response buffer via its own `_alloc` and stores the JSON there.
- `_on_event` returns the pointer.
- The host reads the JSON using `readNullTerminatedOrJSON` (see §9), then calls `_free(respPtr)`.

**Host → Extension (host_call response):**
- When `host_call` needs to return a response, it calls the extension's `_alloc` to get a buffer in WASM memory.
- If `_alloc` returns 0, the response is silently omitted and `ErrOK` is still returned.
- The host writes the JSON-encoded `sdk.HostCallResponse` to that buffer and stores the pointer/length into the caller-supplied `resp_ptr_ptr` / `resp_len_ptr` slots.

**Invariant (C2):** If `_alloc` returns 0 for an event buffer, `_on_event` is never called for that event delivery.

---

## 9. readNullTerminatedOrJSON

`readNullTerminatedOrJSON(mem, ptr)` reads up to 64 KB from WASM linear memory starting at `ptr` and finds the boundary of the first complete JSON object by tracking brace depth, respecting string literals and escape sequences.

**Invariant:** The function never panics on malformed input; it returns whatever bytes it could read if no `{}`-balanced boundary is found.

**Note (C5):** This function is only used on the ABI v1 fallback path. Extensions compiled with the ABI v2 SDK return `(ptr, len)` from `_on_event`, and the host uses `memory.Read(ptr, len)` directly instead of scanning. New extensions should target ABI v2 to avoid the 64KB limit and eliminate the scan.

---

## 10. Subscription Race Safety

`Extension.subMu` is a `sync.RWMutex` that guards all reads and writes to `Extension.subscriptions`.

- `handleSubscribe` acquires `subMu.Lock()` before setting a subscription.
- `DispatchEvent` acquires `subMu.RLock()` before reading a subscription.

**Invariant:** No goroutine may read or write `subscriptions` without holding the appropriate lock.

---

## 11. Host.mu Guards

`Host.loadMu` is a `sync.Mutex` that serializes calls to `loadExtension`. wazero's wazevo JIT engine is not safe for concurrent compilation of the same module; `loadMu` prevents data races in `InstantiateWithConfig`.

`Host.mu` is a `sync.RWMutex` that guards:
- `h.extensions` slice (append in `Load`, nil + replacement in `Reload`, iteration in `DispatchEvent`, removal in `removeExtension`)
- `h.registeredTools` map (read+write in `handleRegisterTool`, snapshot in `GetRegisteredTools` and `RegisteredTools`)
- `h.toolOwners` map (written in `handleRegisterTool`, read in `RegisteredTools`)

`DispatchEvent` copies `h.extensions` under `RLock` before iterating, so the lock is not held during WASM execution.

---

## 12. Store: Per-Extension, Not Shared

Each `Extension` has its own `*Store`. Stores are not shared between extensions.

- `Store.Set(k, v)` is guarded by `Store.mu.Lock()`.
- `Store.Get(k)` is guarded by `Store.mu.RLock()`.
- `handleStoreSet` / `handleStoreGet` route to `ext.store`; if `ext == nil` they return an error.

**Invariant:** Extension A cannot read or write Extension B's store through any host_call.

---

## 13. Tool Registration: First Registration Wins (Override Supported)

`handleRegisterTool` uses `h.mu.Lock()` to atomically check-then-set `h.registeredTools[tool.Name]`.

- If the name is not yet registered, it is added and `OnRegisterTool` is called.
- If the name already exists and `tool.Override == false`, the method returns an error response (`"tool already registered: <name>"`).
- If the name already exists and `tool.Override == true`, the existing entry is replaced and `OnRegisterTool` is called.
- When `ext != nil` (i.e. a WASM extension is making the call), `h.toolOwners[tool.Name]` is set to `ext.name` on registration or override.

**Invariant:** Duplicate tool names without `override: true` are rejected.

`Host.RegisteredTools()` returns a snapshot of `[]RegisteredToolInfo` pairing each tool with its owner name under `h.mu.RLock()`. `Host.GetRegisteredTools()` returns a `[]sdk.Tool` snapshot without ownership information (retained for backward compatibility).

---

## 14. Permission Model

Each `Extension` carries:
- `trusted bool` — set to `true` for extensions loaded via `Host.LoadBytes(ctx, name, data, true)`.
- `permissions map[sdk.Permission]bool` — for untrusted extensions, holds the declared permissions.

`Extension.HasPermission(p Permission) bool`:
- Returns `true` if `ext.trusted`.
- Returns `ext.permissions[p]` otherwise.

**Invariant:** Trusted extensions always pass `HasPermission` for any permission value.

**Invariant:** `get_env` (host_call `MethodGetEnv`) requires `PermEnvRead`. Untrusted extensions without this permission receive a permission-denied error and cannot read any host environment variable. Trusted extensions bypass this check.

`Host.Load` loads the companion manifest (`<basename>.json`) alongside the WASM file and populates `ext.permissions` from `ExtensionManifest.Permissions`. Missing or malformed manifests are logged at warn level and result in zero permissions.

`Host.LoadBytes(ctx, name, data, trusted)` skips manifest loading entirely.

---

## 15. ExecuteTool: Synchronous Tool Dispatch

`Host.ExecuteTool(ctx, agentID, toolCallID, toolName, input)` provides synchronous tool execution.

**Native tool fast path (checked first):**

1. Acquires `nativeToolsMu.RLock()` and looks up `toolName` in `nativeTools`.
2. If found, calls the native function directly (no WASM dispatch, no `pendingTools` channel).
3. Dispatches `EventAfterToolCall` so WASM extensions that observe results stay informed.
4. Calls `OnAfterToolCall` if set, then returns.

**WASM dispatch path (when no native handler is registered):**

1. Creates a `chan toolResult{1}` buffered channel keyed by `toolCallID` in `h.pendingTools`.
2. Dispatches `EventBeforeToolCall` with `BeforeToolCallPayload{AgentID, ToolCallID, ToolName, Input}`.
3. Blocks on `select { case result := <-ch: ...; case <-ctx.Done(): ... }`.
4. On result: dispatches `EventAfterToolCall` with `AfterToolCallPayload{AgentID, ToolCallID, ToolName, Result, IsError}`, then calls `OnAfterToolCall` if set.

**Invariants:**
- Native tools are checked before WASM; a tool registered via `RegisterNativeTool` is never dispatched through WASM.
- `EventBeforeToolCall` is **not** dispatched for native tools (C8 — native tools are trusted built-ins that cannot be intercepted or cancelled by WASM extensions).
- `EventAfterToolCall` **is** dispatched for both native and WASM tools so extensions that observe results work uniformly.
- The WASM channel is registered in `pendingTools` **before** dispatching the event.
- `AgentID` is included in both `BeforeToolCallPayload` and `AfterToolCallPayload` so extensions can correlate tool calls with the originating agent.
- On context cancellation (WASM path), the pending entry is cleaned up before returning.
- `handleToolResult` always calls `OnToolResult` in addition to signalling any pending channel.
- `EventAfterToolCall` is dispatched only on the success path; it is **not** dispatched when `ctx` is cancelled (WASM path).

`Host.RegisterNativeTool(tool sdk.Tool, fn func(ctx, input) (string, bool))` registers a tool schema in `registeredTools` (so the LLM sees it) and stores the handler in `nativeTools` under `nativeToolsMu`. Calls `OnRegisterTool` if set.

`Host.SendToolResult(toolCallID, result string, isError bool)` is the native Go path for tool results (used by the MCP bridge) — it bypasses the WASM host_call mechanism and delivers directly to the pending channel.

---

## 16. WASM Filesystem and Environment Passthrough

When instantiating a WASM module, the host configures:
- `WithFSConfig(wazero.NewFSConfig().WithDirMount("/", "/"))` — mounts the full host filesystem at `/` read/write.
- All host environment variables are passed through via `WithEnv` (iterated from `os.Environ()`).

**Invariant:** Every extension module receives the same filesystem mount and environment at load time. There is no per-extension filesystem isolation in ABI v1.

---

## 17. extensionDisplayName

`extensionDisplayName(path string) string` returns the base filename without the `.wasm` suffix. This is the friendly name used in slog logs (`"extension"` attribute). The full path is used as the wazero module name (for uniqueness); the display name is for human-readable output only.

---

## 19. ABI Documentation Invariant

`docs/extensions.md` is the authoritative public reference for the WASM extension author API. It must be kept in sync with the code.

**Invariant:** Any change to the host↔extension ABI — adding, removing, or modifying a `host_call` method, lifecycle event, event payload field, required WASM export, or permission type — must be reflected in `docs/extensions.md` in the same commit.

Files that trigger this requirement when modified:
- `extension/host.go` — `host_call` dispatch map and method implementations
- `sdk/types.go` — event types, payload structs, permission constants
- Any file that adds or removes constants under `sdk.Method*` or `sdk.Event*`

---

## 18. Reload Semantics

`Host.Reload(ctx, paths)` performs a full replacement:

1. Under `h.mu.Lock()`, captures the current `h.extensions`, sets it to `nil`, resets `h.registeredTools` and `h.toolOwners` to empty maps.
2. Closes each old extension's module (errors logged at warn level).
3. Calls `Load(path)` for each new path; individual failures are logged but do not abort the reload.

**Invariant:** After `Reload`, previously loaded modules are always closed regardless of whether reloading any new module succeeds.

**Invariant:** `h.registeredTools` and `h.toolOwners` are cleared of WASM-owned entries on reload. Native tool registrations (from `RegisterNativeTool`) are preserved across reload; native tools are registered once at startup and are not managed by the WASM lifecycle.

**Lock-ordering invariant (C6):** `Reload` must acquire `nativeToolsMu` before `mu` to avoid deadlock with `RegisterNativeTool` (which takes `nativeToolsMu` then `mu`). The native tool name snapshot is taken under `nativeToolsMu.RLock()` before `mu.Lock()` is acquired.
