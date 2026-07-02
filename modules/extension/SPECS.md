# extension — Interface Contracts and Behavioral Invariants

Package `extension` implements a wazero-based WASM extension host for the Bob harness.
Extensions are compiled WASM modules that communicate with the host via a JSON-over-linear-memory ABI.

---

## 1. Required WASM Exports

Every `.wasm` extension **must** export the following four symbols. `validateExports` enforces this at load time and returns an error if any are absent.

| Export | Signature (WAT) | Semantics |
|--------|-----------------|-----------|
| `_init` | `() -> i32` | Called once after the module is instantiated. Return 0 on success; any other value is an error and causes load to fail. |
| `_on_event` | `(ptr i32, len i32) -> i32` | Dispatch an event. `ptr`/`len` point to a JSON-encoded `sdk.Event` in WASM linear memory. Returns a pointer to a JSON-encoded `sdk.EventResponse` in WASM memory, or 0 if there is no response. |
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
  ├─ runtime.InstantiateWithConfig        (WithStartFunctions() — no auto _start/_main)
  │   ├─ WithFSConfig: WithDirMount("/", "/")   (full filesystem read access)
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
host calls _on_event(evtPtr, len(evtJSON))  →  respPtr
host calls _free(evtPtr)
if respPtr != 0:
    host reads JSON from respPtr using readNullTerminatedOrJSON
    host calls _free(respPtr)
```

### DispatchEventChain Contract (transform-capable interception)

`Host.DispatchEventChain(ctx, evt)` is the **transform** counterpart to the
observe-only `DispatchEvent`. It threads `evt`'s payload through subscribed
extensions in **priority order** (ascending `Priority`, then name ascending —
identical ordering to `DispatchEvent`) and returns
`(final sdk.Event, blocked bool, reason string, err error)`.

For each subscribed extension it calls `_on_event` and applies the returned
`sdk.EventResponse` via `applyInterceptorResponse`:

- **observe** (`nil`/zero response): payload unchanged, chain continues.
- **transform** (`EventResponse.Payload` non-empty): that payload replaces the
  event payload for all subsequent interceptors and the final result.
- **block** (`Block` or `Cancel` true): the chain stops immediately; `blocked`
  is true and `reason` is `EventResponse.Error` (falling back to
  `"blocked by extension <name>"` when empty). Later interceptors do not run.

**Invariants:**

- **Backward compatible.** An extension that returns no `Payload` and no
  `Block`/`Cancel` leaves the event unchanged — existing observe/veto handlers
  behave identically under the chain.
- **First block wins.** The chain short-circuits at the first blocking response.
- **The host does not validate transformed payload shape.** Each seam unmarshals
  the final payload into its expected type and tolerates a malformed transform
  (keeps the prior value, logs a warning). See `runBeforeToolCall`.
- **Re-marshal per hop.** The event is re-marshalled before each extension so
  each interceptor sees the prior interceptor's transform. A `json.Marshal`
  failure is the only error `DispatchEventChain` returns.
- **Bounded crossings.** Chains run once per interaction (per tool call), never
  per render frame.

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

| Method constant                | Handler                                         |
|-------------------------------|--------------------------------------------------|
| `MethodSubscribe`             | `handleSubscribe`                                |
| `MethodRegisterTool`          | `handleRegisterTool`                             |
| `MethodRegisterCommand`       | `handleRegisterCommand`                          |
| `MethodSendMessage`           | `handleSendMessage`                              |
| `MethodSetStatus`             | `handleSetStatus`                                |
| `MethodNotify`                | `handleNotify`                                   |
| `MethodToolResult`            | `handleToolResult`                               |
| `MethodStoreSet`              | `handleStoreSet`                                 |
| `MethodStoreGet`              | `handleStoreGet`                                 |
| `MethodAbort`                 | calls `h.ui.Abort()` via UIBridge                |
| `MethodRequestPermission`     | `handleRequestPermission`                        |
| `MethodModal`                 | `handleModal`                                    |
| `MethodSetSystemPrompt`       | `handleSetSystemPrompt`                          |
| `MethodAppendSystemPrompt`    | `handleAppendSystemPrompt`                       |
| `MethodExec`                  | `handleExec`                                     |
| `MethodGetEnv`                | `handleGetEnv`                                   |
| `MethodReadFile`              | `handleReadFile`                                 |
| `MethodWriteFile`             | `handleWriteFile`                                |
| `MethodAppendFile`            | `handleAppendFile` (requires `file_write`)       |
| `MethodHTTPPost`              | `handleHTTPPost`                                 |
| `MethodConfigRead`            | `handleConfigRead`                               |
| `MethodAgentSpawn`            | `handleAgentSpawn`                               |
| `MethodAgentClose`            | `handleAgentClose`                               |
| `MethodAgentSendMessage`      | `handleAgentSendMessage`                         |
| `MethodAgentDeliver`          | `handleAgentDeliver`                             |
| `MethodAgentRun`              | `handleAgentRun`                                 |
| `MethodAgentList`             | `handleAgentList`                                |
| `MethodAgentTokenCount`       | `handleAgentTokenCount`                          |
| `MethodAgentResetHistory`     | `handleAgentResetHistory`                        |
| `MethodTeamCreate`            | `handleTeamCreate`                               |
| `MethodTeamClose`             | `handleTeamClose`                                |
| `MethodTeamAddMember`         | `handleTeamAddMember`                            |
| `MethodTeamRemoveMember`      | `handleTeamRemoveMember`                         |
| `MethodTeamGetInfo`           | `handleTeamGetInfo`                              |
| `MethodTeamList`              | `handleTeamList`                                 |
| `MethodShowPicker`            | `handleShowPicker`                               |
| `MethodMCPSpawn`              | `handleMCPSpawn`                                 |
| `MethodMCPClose`              | `handleMCPClose`                                 |
| `MethodMCPSend`               | `handleMCPSend`                                  |
| `MethodMCPRead`               | `handleMCPRead`                                  |
| `MethodGetOS`                 | `handleGetOS`                                    |
| `MethodGetStatusInfo`         | `handleGetStatusInfo`                            |
| `MethodSetStatusLine`         | `handleSetStatusLine`                            |
| `MethodGetContextUsage`       | `handleGetContextUsage`                          |
| `MethodUICreateArea`          | `handleUICreateArea` (requires `ui`)             |
| `MethodUIPatch`               | `handleUIPatch` (requires `ui`)                  |
| `MethodUIRemoveArea`          | `handleUIRemoveArea` (requires `ui`)             |
| `MethodUIUpdateArea`          | `handleUIUpdateArea` (requires `ui`)             |

---

## 7. Host Interface Bridges

`Host` exposes five interface fields, each set once at startup via a corresponding `Set*` method. All `Set*` methods acquire `h.mu.Lock()` to protect against concurrent WASM dispatch. Dispatch handlers snapshot each field under `h.mu.RLock()` before use. A nil bridge results in an error response to the extension.

| Field          | Interface           | Setter                  | Purpose                                                        |
|---------------|---------------------|-------------------------|----------------------------------------------------------------|
| `agents`      | `AgentBridge`       | `SetAgentBridge`        | Spawn, close, message, run, list agents and manage history     |
| `teams`       | `TeamBridge`        | `SetTeamBridge`         | Create, close, add/remove members, list teams                  |
| `ui`          | `UIBridge`          | `SetUIBridge`           | Notify, modal, picker, status, system prompt, scene-graph areas |
| `capabilities`| `CapabilityProvider`| `SetCapabilities`       | Exec, GetEnv, ReadFile, WriteFile, AppendFile, HTTPPost, ConfigRead |
| `mcp`         | `MCPBridge`         | `SetMCPBridge`          | Spawn, close, send, read MCP server subprocesses               |

**Invariant:** All `Set*` methods must be called before loading extensions (before `Load` or `LoadBytes`). The `earlyUIBridge` and `earlyAgentBridge` stubs installed in `harness.New()` satisfy this for command registration and agent calls that arrive during `_init`.

**Invariant:** Dispatch handlers snapshot the bridge field under `h.mu.RLock()` via internal getter methods (`h.agentBridge()`, `h.uiBridge()`, etc.) so that the field transition from early stub to full implementation is race-free.

**Invariant:** `PermExec` is required for `agent_spawn` (via `AgentBridge.Spawn`), `exec`, `read_file`, `write_file`, `http_post`, and `mcp_spawn`. If the extension is nil or lacks the required permission, the call returns a permission-denied error response.

**Invariant:** `PermUI` is required for `ui_create_area`, `ui_patch`, `ui_remove_area`, and `ui_update_area`. The `UIBridge` exposes four scene-graph methods: `CreateArea(sdk.UIArea) error`, `PatchUI(sdk.UIPatchParams) error`, `RemoveArea(string)`, and `UpdateArea(sdk.UIUpdateAreaParams) error`. `CreateArea`, `PatchUI`, and `UpdateArea` return errors (duplicate area, missing area/node, unknown area) forwarded to the extension as an error response; `RemoveArea` is a no-op for a missing area.

**Invariant:** `get_context_usage` (`MethodGetContextUsage`) requires no permission. It is a
read-only observability call. When the `AgentBridge` is nil or not yet installed, the handler
returns a zero-valued `sdk.ContextUsage` (all fields zero) rather than an error, consistent
with how `get_status_info` behaves when the `UIBridge` is unavailable.

`AgentBridge.MainAgentContextUsage()` is the sole method that returns context window usage.
It must return a zero-valued `sdk.ContextUsage` before the first turn completes or when no
context window has been configured — it must never panic or block.

`AgentBridge.List()` returns `AgentInfo` snapshots for `agent_list`. Each entry includes
`id`, `name`, `is_running`, and `pending_messages`. `is_running` reports whether the
agent is currently mid-turn; `pending_messages` reports queued inbox messages that will
be processed by the next turn. These fields are read-only liveness/status signals and
must not enqueue work.

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

**Invariant:** The function never panics on malformed input; it returns whatever bytes it could read if no `{}`-balanced boundary is found. The ABI v1 design choice (no length prefix) is why this scanner exists — see NOTES.md §4.

---

## 10. Subscription Race Safety

`Extension.subMu` is a `sync.RWMutex` that guards all reads and writes to `Extension.subscriptions`.

- `handleSubscribe` acquires `subMu.Lock()` before setting a subscription.
- `DispatchEvent` acquires `subMu.RLock()` before reading a subscription.

**Invariant:** No goroutine may read or write `subscriptions` without holding the appropriate lock.

---

## 11. Host.mu Guards

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

`Host.Load` loads the companion manifest (`<basename>.json`) alongside the WASM file and populates `ext.permissions` from `ExtensionManifest.Permissions`. Missing or malformed manifests are logged at warn level and result in zero permissions.

`Host.LoadBytes(ctx, name, data, trusted)` skips manifest loading entirely.

---

## 15. ExecuteTool: Synchronous Tool Dispatch

`Host.ExecuteTool(ctx, agentID, toolCallID, toolName, input)` provides synchronous tool execution.

**Both paths run the `before_tool_call` interceptor chain** via
`runBeforeToolCall` (a wrapper over `DispatchEventChain`): interceptors may
**rewrite the tool input** or **block** the call with a reason. A blocked call
returns `toolResult{Result: blockReason(reason), IsError: true}` without executing
the tool. A malformed transformed payload is tolerated (original input kept,
warning logged).

**Both paths also run the `after_tool_call` interceptor chain** via
`runAfterToolCall`: interceptors may **rewrite/redact the tool's result** (e.g.
strip secrets from command output) or **block** it. The output side is the
symmetric partner of the input side. A block on the output replaces the result
wholesale with `blockReason(reason)` and forces `IsError=true`. A malformed
transform keeps the original result. `EventAfterToolCall` is therefore a
transform chain, not observe-only — but an extension that returns no `Payload`
still observes the result exactly as before.

**Native tool fast path (checked first):**

1. Acquires `nativeToolsMu.RLock()` and looks up `toolName` in `nativeTools`.
2. Runs `runBeforeToolCall`; if blocked, returns the block result.
3. If found, calls the native function with the **final (possibly rewritten) input** (no `pendingTools` channel).
4. Runs `runAfterToolCall` on the result so interceptors can rewrite/redact/block the output.
5. Calls `h.ui.AfterToolCall(agentID, toolCallID, toolName, result, isError)` via UIBridge (if set) with the **final result**, then returns.

**WASM dispatch path (when no native handler is registered):**

1. Registers a `chan toolResult{1}` buffered channel keyed by `toolCallID` in `h.pendingTools` **first** (the implementing extension is itself a `before_tool_call` subscriber and calls `tool_result` synchronously during the chain).
2. Runs `runBeforeToolCall` (the interceptor chain). The implementing extension, running later in priority order, sees the threaded final input and calls `tool_result`. If a lower-priority interceptor blocks, the chain stops before the implementer runs, the pending entry is removed, and the block result is returned.
3. Blocks on `select { case result := <-ch: ...; case <-ctx.Done(): ... }`.
4. On result: runs `runAfterToolCall` (the output transform chain), then calls `h.ui.AfterToolCall(agentID, toolCallID, toolName, result, isError)` via UIBridge (if set) with the final result.

**Invariants:**

- Native tools are checked before WASM; a tool registered via `RegisterNativeTool` is never dispatched through WASM.
- `EventBeforeToolCall` **is** dispatched (as a transform chain) for both native and WASM tools — interceptors can rewrite input or block either. (Previously native tools skipped it; the interceptor contract applies uniformly.)
- `EventAfterToolCall` **is** dispatched (as a transform chain) for both native and WASM tools — interceptors can rewrite/redact/block the result, and observers (no `Payload`) work uniformly.
- The WASM channel is registered in `pendingTools` **before** running the chain, so a `tool_result` called synchronously by the implementing extension during the chain is never dropped.
- A blocked tool call returns an error `toolResult` and the implementing tool never executes; for the WASM path the pending channel entry is removed.
- The UIBridge `AfterToolCall` is always called with the originating `agentID` and the **post-interception** result, so the TUI shows what the model actually received and can attribute the call to the correct agent.
- `AgentID` is included in both `BeforeToolCallPayload` and `AfterToolCallPayload` so extensions can correlate tool calls with the originating agent.
- On context cancellation (WASM path), the pending entry is cleaned up before returning.
- `handleToolResult` always calls `h.ui.ToolResult(toolCallID, result, isError)` via UIBridge in addition to signalling any pending channel.
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

## 18. Reload Semantics

`Host.Reload(ctx, paths)` performs a full replacement:

1. Snapshots native tool names under `h.nativeToolsMu.RLock()` (before acquiring `h.mu`) to avoid lock-order inversion with `RegisterNativeTool`.
2. Under `h.mu.Lock()`, captures the current `h.extensions`, sets it to `nil`, resets `h.registeredTools` to preserve only native tools, and clears `h.toolOwners`.
3. Closes each old extension's module (errors logged at warn level).
4. Calls `Load(path)` for each new path; individual failures are logged but do not abort the reload.

**Invariant:** After `Reload`, previously loaded modules are always closed regardless of whether reloading any new module succeeds.

**Invariant:** `h.registeredTools` and `h.toolOwners` are both cleared on reload (native tools are preserved).

**Invariant:** Lock order is always `nativeToolsMu` before `h.mu`. Snapshot native tool names under `nativeToolsMu` before acquiring `h.mu` in `Reload` to prevent deadlock with `RegisterNativeTool`.

---

## 19. ABI Documentation Invariant

`docs/extensions.md` is the authoritative public reference for the WASM extension author API. It must be kept in sync with the code.

**Invariant:** Any change to the host↔extension ABI — adding, removing, or modifying a `host_call` method, lifecycle event, event payload field, required WASM export, or permission type — must be reflected in `docs/extensions.md` in the same commit.

Files that trigger this requirement when modified:

- `extension/host.go` — `host_call` dispatch map and method implementations
- `sdk/types.go` — event types, payload structs, permission constants
- Any file that adds or removes constants under `sdk.Method*` or `sdk.Event*`
