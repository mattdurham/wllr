# extension — Specifications

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
| `host_log` | `(level i32, ptr i32, len i32)` | Write a log message. `ptr`/`len` point to a UTF-8 string in WASM memory. `level`: 0=debug, 1=info, 2=warn, 3=error. |
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

Host.Load(path)
  ├─ os.ReadFile
  ├─ runtime.InstantiateWithConfig        (WithStartFunctions() — no auto _start/_main)
  ├─ validateExports                      (abort + close if any export missing)
  ├─ Register ext in h.extensions         (before callInit so host_call works during _init)
  └─ callInit
       ├─ optional: _initialize()         (native Go WASM bootstrap, if exported)
       └─ _init() -> i32                  (non-zero return removes ext and fails load)

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

`Host.DispatchEvent(ctx, evt)` iterates all loaded extensions in load order and calls `_on_event` on each that has subscribed to `evt.Type`.

**Rules:**
1. Only extensions whose `subscriptions[evt.Type] == true` receive the call (guarded by `ext.subMu`).
2. If `_alloc` returns 0 for the event buffer, `_on_event` is **NOT** called for that extension. An empty `sdk.EventResponse{}` is returned for that slot (and is silently dropped — no entry added to the responses slice because dispatch returns early before appending).
3. If `_on_event` returns a WASM trap (error from wazero), it is logged at level 2 (warn) and dispatch continues to the next extension. The error is **not** propagated to the caller.
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

---

## 5. Memory Protocol

**Host → Extension (event delivery):**
- Host calls `_alloc(n)` to request `n` bytes from the extension's allocator.
- Host writes the event JSON to the returned pointer.
- If `_alloc` returns 0, the write and `_on_event` call are skipped entirely.
- After reading the response, the host calls `_free(evtPtr)` to release the input buffer.

**Extension → Host (response):**
- The extension allocates its response buffer via its own `_alloc` and stores the JSON there.
- `_on_event` returns the pointer.
- The host reads the JSON using `readNullTerminatedOrJSON` (see §6), then calls `_free(respPtr)`.

**Host → Extension (host_call response):**
- When `host_call` needs to return a response, it calls the extension's `_alloc` to get a buffer in WASM memory.
- If `_alloc` returns 0, the response is silently omitted and `ErrOK` is still returned.
- The host writes the JSON-encoded `sdk.HostCallResponse` to that buffer and stores the pointer/length into the caller-supplied `resp_ptr_ptr` / `resp_len_ptr` slots.

**Invariant (C2):** If `_alloc` returns 0 for an event buffer, `_on_event` is never called for that event delivery.

---

## 6. readNullTerminatedOrJSON

`readNullTerminatedOrJSON(mem, ptr)` reads up to 64 KB from WASM linear memory starting at `ptr` and finds the boundary of the first complete JSON object by tracking brace depth, respecting string literals and escape sequences.

**Invariant:** The function never panics on malformed input; it returns whatever bytes it could read if no `{}`-balanced boundary is found. The ABI v1 design choice (no length prefix) is why this scanner exists — see NOTES.md §4.

---

## 7. Subscription Race Safety

`Extension.subMu` is a `sync.RWMutex` that guards all reads and writes to `Extension.subscriptions`.

- `handleSubscribe` acquires `subMu.Lock()` before setting a subscription.
- `DispatchEvent` acquires `subMu.RLock()` before reading a subscription.

**Invariant:** No goroutine may read or write `subscriptions` without holding the appropriate lock.

---

## 8. Host.mu Guards

`Host.mu` is a `sync.RWMutex` that guards:
- `h.extensions` slice (append in `Load`, nil + replacement in `Reload`, iteration in `DispatchEvent`, removal in `removeExtension`)
- `h.registeredTools` map (read+write in `handleRegisterTool`, snapshot in `GetRegisteredTools` and `RegisteredTools`)
- `h.toolOwners` map (written in `handleRegisterTool`, read in `RegisteredTools`)

`DispatchEvent` copies `h.extensions` under `RLock` before iterating, so the lock is not held during WASM execution.

---

## 9. Store: Per-Extension, Not Shared

Each `Extension` has its own `*Store`. Stores are not shared between extensions.

- `Store.Set(k, v)` is guarded by `Store.mu.Lock()`.
- `Store.Get(k)` is guarded by `Store.mu.RLock()`.
- `handleStoreSet` / `handleStoreGet` route to `ext.store`; if `ext == nil` they return an error.

**Invariant:** Extension A cannot read or write Extension B's store through any host_call.

---

## 10. Tool Registration: First Registration Wins (Override Supported)

`handleRegisterTool` uses `h.mu.Lock()` to atomically check-then-set `h.registeredTools[tool.Name]`.

- If the name is not yet registered, it is added and `OnRegisterTool` is called.
- If the name already exists and `tool.Override == false`, the method returns an error response (`"tool already registered: <name>"`).
- If the name already exists and `tool.Override == true`, the existing entry is replaced and `OnRegisterTool` is called.
- The `OnRegisterTool` callback is invoked for the first successful registration and for each successful override.
- When `ext != nil` (i.e. a WASM extension is making the call), `h.toolOwners[tool.Name]` is set to `ext.name` on registration or override.

**Invariant:** Duplicate tool names without `override: true` are rejected. With `override: true` the new registration replaces the old one.

**Invariant:** `RegisteredToolInfo.OwnerName` is the `ext.name` of the last extension to successfully register the tool. It is empty if the tool was not registered via a WASM extension `host_call`.

`Host.RegisteredTools()` returns a snapshot of `[]RegisteredToolInfo` pairing each tool with its owner name under `h.mu.RLock()`. `Host.GetRegisteredTools()` returns a `[]sdk.Tool` snapshot without ownership information (retained for backward compatibility).

---

## 11. Permission Model

Each `Extension` carries:
- `trusted bool` — set to `true` for extensions loaded via `Host.LoadBytes(ctx, name, data, true)`.
- `permissions map[sdk.Permission]bool` — for untrusted extensions, holds the declared permissions.

`Extension.HasPermission(p Permission) bool`:
- Returns `true` if `ext.trusted`.
- Returns `ext.permissions[p]` otherwise.

**Invariant:** Trusted extensions always pass `HasPermission` for any permission value. Untrusted extensions pass only for permissions explicitly granted in their manifest.

`Host.Load` loads the companion manifest (`<basename>.json`) alongside the WASM file and populates `ext.permissions` from `ExtensionManifest.Permissions`. Missing or malformed manifests are logged at warn level and result in zero permissions.

`Host.LoadBytes(ctx, name, data, trusted)` skips manifest loading entirely. When `trusted=true` the extension bypasses all permission checks.

## 12. ExecuteTool: Synchronous Tool Dispatch

`Host.ExecuteTool(ctx, toolCallID, toolName, input)` provides synchronous tool execution from Fantasy's perspective:

1. Creates a `chan toolResult{1}` buffered channel keyed by `toolCallID` in `h.pendingTools`.
2. Dispatches `EventBeforeToolCall` with `BeforeToolCallPayload` to subscribed extensions.
3. Blocks on `select { case result := <-ch: ...; case <-ctx.Done(): ... }`.
4. After receiving the tool result from the channel, dispatches `EventAfterToolCall` to all subscribed extensions with `AfterToolCallPayload{ToolCallID, ToolName, Result, IsError}`. If `OnAfterToolCall` is set, it is called immediately after the event dispatch.

**Invariants:**
- The channel is registered in `pendingTools` **before** dispatching the event, so a synchronous extension call to `tool_result` within `_on_event` will succeed.
- On context cancellation, the pending entry is cleaned up before returning.
- `handleToolResult` always calls `OnToolResult` in addition to signalling any pending channel (backward compatibility).
- `EventAfterToolCall` is dispatched only on the success path (result received); it is **not** dispatched when `ctx` is cancelled.
- `OnAfterToolCall(toolCallID, toolName, result, isError)` is invoked after `EventAfterToolCall` dispatch completes, if the callback is set.

## 13. Reload Semantics

`Host.Reload(ctx, paths)` performs a full replacement:

1. Under `h.mu.Lock()`, the current `h.extensions` slice is captured, set to `nil`, `h.registeredTools` is reset to an empty map, and `h.toolOwners` is reset to an empty map.
2. Each old extension's module is closed (errors logged at warn level).
3. `Load(path)` is called for each new path; individual failures are logged but do not abort the reload.

**Invariant:** After `Reload`, previously loaded modules are always closed regardless of whether reloading any new module succeeds.

**Invariant:** `h.registeredTools` and `h.toolOwners` are both cleared on reload. Extensions re-register their tools during `_init` on the reload path.
