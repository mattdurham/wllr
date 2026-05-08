# extension — Design Notes

Append-only design decision log. Never delete entries; add an `*Addendum (date):*` if a decision is reversed.

---

## 1. Why wazero over wasmer/wasmtime

*Added: 2026-05-06*

**Decision:** Use [wazero](https://github.com/tetratelabs/wazero) as the WASM runtime.

**Rationale:** wazero is a pure-Go, zero-dependency WASM runtime. It requires no CGo, no system libraries, and no external toolchain. wasmer and wasmtime both require CGo bindings and native shared libraries, which complicate cross-compilation, static linking, and distribution as a single binary. wazero's API is idiomatic Go (context propagation, standard error returns) and its module configuration model maps cleanly onto the host/extension separation needed here.

**Consequence:** The extension host is fully self-contained in a pure-Go binary. The trade-off is that wazero's interpreter/compiler is slower than Cranelift (wasmtime) for CPU-intensive workloads, but extensions here are I/O and event-driven, so raw throughput matters less than startup latency and operational simplicity.

---

## 2. WithStartFunctions() — Avoiding auto-run of _start/_main

*Added: 2026-05-06*

**Decision:** Pass `WithStartFunctions()` (empty variadic — no start functions) to `wazero.NewModuleConfig` when instantiating extension modules.

**Rationale:** By default, wazero runs `_start` (WASI convention) or `main` automatically during `InstantiateWithConfig`. Most WASM modules compiled with TinyGo or native Go export `_start`/`main` to run the program entry point. For extension modules this is wrong: the module should only execute code when the host explicitly calls `_init` or `_on_event`. If `_start` were auto-run it would execute before `ext` is registered in `h.extensions`, so any `host_call` made during startup (e.g. `subscribe`) would fail to find the calling extension.

**Consequence:** Extensions must not rely on `_start` or `main` for initialization. All setup must happen inside `_init`. Native Go WASM modules that need runtime bootstrap use `_initialize` instead (see §3).

---

## 3. Why _initialize is called before _init

*Added: 2026-05-06*

**Decision:** `callInit` checks for an exported `_initialize` function and calls it before calling `_init`.

**Rationale:** Native Go modules compiled with `GOOS=wasip1` export `_initialize` as the Go runtime bootstrap entry point. This function initializes the garbage collector, goroutine scheduler, and global variables. Without calling `_initialize` first, any Go code in `_init` will crash or behave incorrectly because the runtime is not yet set up. TinyGo modules do not export `_initialize`; the check is a no-op for them.

**Consequence:** The host is compatible with both TinyGo (`-target wasi`) and native Go (`GOOS=wasip1 GOARCH=wasm`) compiled extensions. The WASI snapshot preview1 module must also be instantiated (done in `NewHost`) to satisfy native Go's WASI imports.

---

## 4. Why readNullTerminatedOrJSON scans for JSON object boundary

*Added: 2026-05-06*

**Decision:** `readNullTerminatedOrJSON` reads up to 64 KB from WASM memory and finds the end of the JSON object by tracking brace depth and string-literal state, rather than using a length prefix.

**Rationale:** ABI v1 has no length prefix on the response returned by `_on_event`. `_on_event` returns only a single `i32` pointer; there is no second return value for length (WASM MVP supports multiple return values but TinyGo/C extensions typically return one). Adding a length to the return value would require changing the ABI for all existing extensions. Reading until a null byte is fragile if the JSON contains embedded nulls (impossible in valid JSON but tolerated by some serializers). Scanning for brace balance is reliable for well-formed JSON and avoids ABI breakage.

**Consequence:** The scanner is O(n) in the response size and correctly handles strings containing `{` and `}`. The 64 KB cap is a safety limit; extensions producing larger responses should be redesigned. Malformed JSON falls through to `json.Unmarshal`, which returns an error that is logged and treated as an empty response.

---

## 5. Why removeExtension uses make() not append with [:0]

*Added: 2026-05-06*

**Decision:** `removeExtension` builds a new slice with `make([]*Extension, 0, len(h.extensions))` rather than reslicing the existing backing array with `h.extensions[:0]`.

**Rationale:** Reslicing to `[:0]` and then appending would reuse the original backing array. The old slice header (held by `DispatchEvent`'s local copy taken under `RLock`) continues to reference the same array. After reslicing, the host could overwrite array elements that the dispatching goroutine is still iterating, creating a data race even though the slice header itself was replaced. Allocating a new backing array ensures the old slice header and its elements remain valid for the lifetime of any concurrent iteration.

**Consequence:** One small allocation per `removeExtension` call. This is acceptable given that remove only occurs on `_init` failure (rare path) and during `Reload` (intentional).

---

## 6. subMu added to Extension for concurrent DispatchEvent safety

*Added: 2026-05-06*

**Decision:** `Extension` carries its own `sync.RWMutex` (`subMu`) to guard the `subscriptions` map.

**Rationale:** `DispatchEvent` reads `subscriptions` while holding only `h.mu.RLock()` (which it releases before calling WASM). If the host dispatches events from multiple goroutines simultaneously, and one goroutine's `_init` or `host_call/subscribe` is writing to `subscriptions` concurrently, the map access is a data race. The Go race detector flags this. A per-extension lock is narrower than extending `h.mu` coverage over the entire dispatch loop (which would serialize all extensions).

**Consequence:** Subscribe and subscription-check are independently locked per extension. Concurrent dispatches to different extensions are fully parallel; concurrent dispatches to the same extension are serialized only at the subscription read, not at the WASM call level.

---

## 7. WASI instantiation added for native Go WASM support

*Added: 2026-05-06*

**Decision:** `NewHost` calls `wasi_snapshot_preview1.Instantiate` on the wazero runtime before any extensions are loaded.

**Rationale:** Native Go modules compiled with `GOOS=wasip1` import WASI snapshot preview1 functions (`fd_write`, `proc_exit`, etc.) for I/O and process control. Without the WASI module present in the runtime, attempting to instantiate a native Go extension fails with "missing import" errors. TinyGo modules compiled with `-target wasi` also use WASI. Installing WASI once at host creation time makes the runtime compatible with both extension types without per-extension configuration.

**Consequence:** The WASI module occupies a small amount of runtime state. Extensions that do not use WASI incur no runtime overhead from this — unused WASI functions are never called. If WASI instantiation fails, the error is logged at level error and `NewHost` continues.

---

## 8. Permission Model: Trusted vs. Untrusted Extensions

*Added: 2026-05-06*

**Decision:** Extensions are classified as either trusted (built-in) or untrusted (user-supplied). Trusted extensions bypass all permission checks. Untrusted extensions declare required permissions in a companion `<basename>.json` manifest.

**Rationale:** Built-in extensions are compiled into the binary and vetted at development time — shipping them with a manifest and requiring permission review would add friction without security benefit. User extensions, by contrast, are arbitrary code loaded from disk at runtime; restricting them to declared permissions limits the blast radius of a malicious or buggy extension. Separating the two classes with a `trusted bool` flag is the simplest design that achieves this split.

**Consequence:** `Host.Load` reads the manifest alongside the WASM file. `Host.LoadBytes` takes an explicit `trusted bool` parameter; the manifest loading path is skipped entirely for built-ins. The `MethodRequestPermission` host_call lets extensions query whether they hold a permission before attempting a restricted operation.

---

## 9. ExecuteTool: Channel-Based Synchronous Tool Dispatch

*Added: 2026-05-06*

**Decision:** `Host.ExecuteTool` dispatches `EventBeforeToolCall` to extensions and blocks on a per-call buffered channel until the extension calls `tool_result` via `host_call`. The channel is registered in `pendingTools` before the event is dispatched.

**Rationale:** Fantasy's `AgentTool.Run` is a synchronous `func(ctx, ToolCall) (ToolResponse, error)`. WASM extensions are single-threaded: when `DispatchEvent` calls `_on_event`, the extension runs synchronously and can call `tool_result` within the same `_on_event` invocation. Registering the pending channel before dispatch ensures the `handleToolResult` path finds the channel even when `_on_event` calls `tool_result` synchronously before `DispatchEvent` returns. A buffered channel (capacity 1) prevents deadlock if the extension and the host race.

**Consequence:** Extensions that handle tool calls must call `tool_result` within their `_on_event` handler for `before_tool_call`. Failing to do so blocks the host goroutine until context cancellation.

---

## 10. Tool Override via override: true Flag

*Added: 2026-05-06*

**Decision:** `handleRegisterTool` allows a second registration to replace an existing tool if `Tool.Override == true`.

**Rationale:** Without override support, built-in extensions load first and register their tools (e.g. `read_file`). A user extension that wants to provide a custom `read_file` implementation (with different behavior or permissions) would be unable to register the same name. The override flag gives user extensions an explicit opt-in to replace built-in tools. The flag is off by default to preserve the "first registration wins" invariant for accidental duplicates.

**Consequence:** The `OnRegisterTool` callback is fired on each successful registration, including overrides. Callers that aggregate tools for Fantasy must refresh the tool list after any override registration.

---

## 11. Tool Ownership Tracking via toolOwners Map

*Added: 2026-05-06*

**Decision:** `Host` carries a `toolOwners map[string]string` alongside `registeredTools`. When `handleRegisterTool` is called from a WASM extension context (`ext != nil`), it records `h.toolOwners[tool.Name] = ext.name`. `Host.RegisteredTools()` returns `[]RegisteredToolInfo` pairing each tool with its owner.

**Rationale:** The harness and operator tooling need to know which extension registered a given tool — for logging, permission checks, and diagnostics. Storing ownership in a parallel map avoids embedding the owner name in `sdk.Tool` (which is an ABI-visible type shared with WASM extensions) and keeps the ABI unchanged.

**Consequence:** `toolOwners` must be reset together with `registeredTools` in `Reload`. Tools registered without an extension context (`ext == nil`) have an empty `OwnerName`.

---

## 12. EventBus Alongside WASM Dispatch

*Added: 2026-05-08*

**Decision:** `Host` exposes a public `Bus *EventBus` field. `DispatchEvent` publishes to `Bus` first, then calls WASM extensions. Go-native components (the harness, MCP bridge) can subscribe to the bus without needing to be WASM modules.

**Rationale:** Some event consumers are pure Go (e.g. a future metrics collector or the MCP bridge reacting to `EventBeforeToolCall`). Requiring them to compile to WASM just to receive events is impractical. The bus provides a lightweight in-process pub/sub without altering the WASM ABI. Publishing to the bus before WASM dispatch ensures Go subscribers see events in the same order as WASM subscribers.

**Consequence:** Bus handlers run asynchronously (in a goroutine spawned by `Publish`). If a handler needs to respond synchronously (e.g. cancel a tool call), it cannot do so via the bus — it must be a WASM extension that calls `tool_result` synchronously.

---

## 13. dispatch Map Built Once in NewHost

*Added: 2026-05-08*

**Decision:** The method-to-handler dispatch map is built exactly once in `NewHost` via `buildDispatch()` and stored on `h.dispatch`. `routeHostCall` performs a single map lookup.

**Rationale:** Before this change, `routeHostCall` was a large `switch` statement in a single function. As the number of methods grew (agent, team, MCP management), the function became hard to read and modify. Moving to a map allows each handler to be a self-contained closure and makes the complete list of supported methods visible at a glance in `buildDispatch`. The map is constructed once and never modified, so there are no concurrent access concerns.

**Consequence:** Adding a new host_call method requires adding a single entry to `buildDispatch`. Handlers that need access to `ext` receive it as a parameter from the closure.

---

## 14. AgentID in BeforeToolCallPayload and AfterToolCallPayload

*Added: 2026-05-08*

**Decision:** Both `BeforeToolCallPayload` and `AfterToolCallPayload` include an `AgentID string` field identifying which agent issued the tool call.

**Rationale:** With multiple sub-agents each running tool calls concurrently, an extension that monitors tool activity (e.g. a task tracker or a security auditor) needs to know which agent originated the call. The `ToolCallID` uniquely identifies the call, but without `AgentID` the extension cannot associate it with the agent context without maintaining its own mapping. Adding `AgentID` to both payloads is a non-breaking addition (new JSON field, existing extensions ignore it).

**Consequence:** `ExecuteTool` now takes an `agentID string` parameter. All callers (`sdkToolAdapter.Run` in harness/tools.go) must supply the agent ID.

---

## 15. OnModal, OnSetSystemPrompt, OnAppendSystemPrompt — New Harness Callbacks

*Added: 2026-05-08*

**Decision:** Three new callback fields were added to `Host`: `OnModal`, `OnSetSystemPrompt`, and `OnAppendSystemPrompt`.

**Rationale:**
- `OnModal`: Extensions (notably the `context` extension displaying AGENTS.md) need to surface text to the user in a focused overlay, not just a chat notification. A dedicated modal host_call avoids cluttering the chat stream with large blocks of text.
- `OnSetSystemPrompt` / `OnAppendSystemPrompt`: The `context` extension sets AGENTS.md as the system prompt; the `skills` extension appends skill descriptions. These must propagate to the agent pool via `AgentPool.SetBaseSystemPrompt` / `AppendBaseSystemPrompt`. Rather than wiring the pool directly into the host (creating a circular dependency), the host exposes callbacks that the harness wires in `SetProgram`.

**Consequence:** `handleModal`, `handleSetSystemPrompt`, and `handleAppendSystemPrompt` return error responses if the corresponding callback is nil, rather than silently dropping the call.

---

## 16. MCP Bridge Callbacks — Why Separate from Agent Callbacks

*Added: 2026-05-08*

**Decision:** MCP bridge operations (`OnMCPSpawn`, `OnMCPClose`, `OnMCPSend`, `OnMCPRead`) are separate callback fields rather than being folded into the agent management callbacks.

**Rationale:** MCP servers are subprocesses with a JSON-RPC protocol, not agents. They have a different lifecycle (spawned by the MCP bridge extension, not by the agents extension), a different communication pattern (bidirectional JSON-RPC vs. one-shot messages), and different permission requirements (exec permission required for spawn). Keeping them separate in both the host and the SDK constants preserves clarity and allows the MCP bridge to be disabled independently from agent management.

**Consequence:** The `mcp_spawn` host_call checks `PermExec`. Any extension that wants to manage MCP servers must declare `exec` in its manifest.

---

## 17. WASM Filesystem Mount and Environment Passthrough

*Added: 2026-05-08*

**Decision:** All WASM extensions receive a full host filesystem mount (`WithDirMount("/", "/")`) and all host environment variables at load time.

**Rationale:** Extensions such as `readfile`, `writefile`, and `exec` need to access the host filesystem. Rather than maintaining a per-extension allowlist of paths (which would require a new manifest field and host-side path-prefix enforcement), all extensions receive the full mount. Permission checks (`PermFileRead`, `PermFileWrite`, `PermExec`) at the host_call layer provide access control at the operation level; the WASM module itself cannot call host functions without going through `host_call`. Environment variables are passed through so extensions can read `HOME`, `PATH`, config directories, and API keys without a separate `get_env` call for each.

**Consequence:** There is no filesystem isolation between WASM extension code paths. An extension with `PermExec` could use the `exec` host_call to read or write any file. Trust is enforced at the permission and trusted/untrusted boundary, not at the filesystem level.
