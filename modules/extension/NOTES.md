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

## 3. Why _initialize is called before_init

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

---

## 18. Native Tool Fast Path — Stateless Tools Bypass WASM

*Added: 2026-05-09*

**Decision:** `Host` gains a `nativeTools map[string]func(ctx, input) (string, bool)` alongside a dedicated `nativeToolsMu sync.RWMutex`. `ExecuteTool` checks this map first, before creating a `pendingTools` channel or dispatching `EventBeforeToolCall`. The four stateless I/O tools (`read_file`, `write_file`, `exec`, `get_env`) are registered as native functions in `cmd/main.go` and their WASM extension counterparts (`readfile.wasm`, `writefile.wasm`, `exec.wasm`, `env.wasm`) are removed from the build.

**Rationale:** Stateless tools (file I/O, exec, env) have no state in WASM linear memory; they only delegate to host OS calls. Running them through WASM adds: (a) two full WASM dispatch round-trips (`_alloc` + `_on_event`), (b) serialization through the `host_call` JSON protocol, and (c) contention on `Extension.callMu` which serializes all calls into each WASM module. There is no correctness benefit from going through WASM for these tools — they do not subscribe to events, hold extension state, or require isolation. Registering them as native functions in `ExecuteTool` eliminates all of that overhead. `EventAfterToolCall` is still fired so WASM extensions that observe tool results (e.g. the `history` extension) continue to work.

**Consequence:** `RegisterNativeTool` must be called before the harness processes any tool call. Native tools bypass `EventBeforeToolCall` — WASM extensions cannot intercept or short-circuit them. If interception is needed in the future the tool must be moved back to WASM (or a native interception hook added). The `nativeToolsMu` is separate from `h.mu` to avoid lock ordering issues between tool registration and the existing extension/tool registration paths.

## 19. OnExec Signature Change — Context and Line Streaming Callback

*Added: 2026-05-27*

**Decision:** `OnExec` signature changed from `func(command, dir string) (string, error)` to
`func(ctx context.Context, command, dir string, onLine func(string)) (string, error)`.
Two new callback fields added: `OnConsoleOutput func(line string)` and `OnConsoleClear func()`.

**Rationale:** Two independent improvements are bundled into one breaking change to avoid
two separate signature bumps. First, `ctx context.Context` is added as the first parameter
(Go convention) so that a cancelled agent turn can kill the subprocess via `exec.CommandContext`.
Without this, `cmd.CombinedOutput()` blocks indefinitely even after the user presses Ctrl+C.
Second, `onLine func(string)` is added as the last parameter so the registering caller can
receive output lines as they arrive (for streaming to the TUI) without changing the return
contract (the full output string is still returned). The `onLine` parameter is nil-safe by
convention: all implementations must guard with `if onLine != nil`.
`OnConsoleOutput` and `OnConsoleClear` are separate host callbacks that decouple the line
delivery mechanism from the exec signature: the registering harness wires these to the TUI
program, while the exec closure in cmd/exec.go calls them directly.

**Consequence:** This is a breaking change. Any caller that previously assigned `OnExec` with
the old signature must be updated. As of this change, the only caller is `cmd/exec.go`
(extracted from `cmd/main.go`). The SPECS.md §7 table is updated. Callers compiled against
the old ABI will fail to compile.

---

## 20. AgentBridge.WaitForAll and WaitResult Removed

*Added: 2026-05-31*

**Decision:** `AgentBridge.WaitForAll` and the associated `WaitResult` type were removed
from `interfaces.go`. Multi-agent coordination is now provided by the native `wait_for_all`
tool in `modules/tools` rather than as a method on the `AgentBridge` interface.

**Rationale:** `WaitForAll` was originally implemented inside `modules/harness/bridges.go`
as a method on `harnessAgentBridge`. Moving the functionality to a dedicated native tool
keeps the `AgentBridge` interface focused on agent lifecycle management (spawn, close, send,
run, list) and removes polling logic from the bridge layer entirely. The native tool approach
is also easier to test in isolation and more transparent to extensions calling it.

**Consequence:** Any implementation of `AgentBridge` no longer needs to provide `WaitForAll`.
Extensions that previously relied on this bridge method must use the `wait_for_all` tool
instead. This is a breaking change to the `AgentBridge` interface for any out-of-tree
implementations.

---

## 23. UIBridge scene-graph methods and ui_* host calls (UI P1)

*Added: 2026-06-29*

**Decision:** `UIBridge` gains three methods — `CreateArea(sdk.UIArea) error`, `PatchUI(sdk.UIPatchParams) error`, `RemoveArea(string)` — and the host gains three dispatch methods `ui_create_area`, `ui_patch`, `ui_remove_area`, all gated behind the new `sdk.PermUI` permission.

**Rationale:** This is phase P1 of letting any WASM extension drive the TUI via the declarative scene graph defined in `sdk` (see sdk NOTES §13). The host validates the permission and forwards the typed params to the `UIBridge`, mirroring the existing pattern for `exec`/`modal`/`show_picker`. Keeping the three operations on the existing `UIBridge` (rather than a sixth bridge interface) avoids interface proliferation since they are conceptually UI operations alongside `Notify`/`ShowModal`/`ShowPicker`.

**Consequence:** All `UIBridge` implementations (including test stubs in `host_test.go`, `interfaces_test.go`, `mcp/extension_test.go` and the `earlyUIBridge`) must implement the three new methods. `CreateArea`/`PatchUI` return errors that surface to the extension as `HostCallResponse.Error`; `RemoveArea` cannot fail. The harness implementation mutates a shared, goroutine-safe `SceneRenderer` synchronously and sends a redraw signal — see harness NOTES.

---

## 24. UIBridge.UpdateArea and ui_update_area host call (step 3 of statusline scene)

*Added: 2026-06-30*

**Decision:** `UIBridge` gains a fourth scene-graph method `UpdateArea(sdk.UIUpdateAreaParams) error`, and the host gains a corresponding `ui_update_area` dispatch entry gated behind `sdk.PermUI`.

**Rationale:** Following the statusline scene design (docs/plans/2026-06-30-statusline-scene-design.md), extensions need to update area constraints post-creation (e.g. collapse to 0 lines when idle, expand to show detail). Adding it to the existing `UIBridge` is consistent with the pattern established in NOTES §23. The handler follows the same guard structure as `handleUICreateArea` / `handleUIPatch`: permission check, nil bridge check, JSON unmarshal, delegate to `UIBridge.UpdateArea`. The harness implementation delegates to `SceneRenderer.UpdateArea` and sends `sceneDirtyMsg{}`.

**Consequence:** All `UIBridge` implementations must add `UpdateArea`. Affected stubs: `earlyUIBridge`, `testUIBridge` in `host_test.go` and `mcp/extension_test.go`, `fakeUIBridge` in `interfaces_test.go`, and `sceneUIBridge` in `test/wasmchat`. All updated in this commit.

---

## 25. DispatchEventChain — transform-capable interceptor pipeline

*Added: 2026-06-30*

**Decision:** Add `Host.DispatchEventChain(ctx, evt) (final sdk.Event, blocked bool, reason string, err error)` alongside the existing observe-only `DispatchEvent`. It threads the event payload through subscribed extensions in priority order; each `_on_event` response is applied via the pure helper `applyInterceptorResponse` (observe → unchanged, transform → thread new payload, block/cancel → stop with reason). `ExecuteTool` now runs `before_tool_call` through this chain (via `runBeforeToolCall`) for **both** native and WASM tools, so an interceptor can rewrite the tool input or block the call. New helper `blockReason` formats the user-facing blocked-call result.

**Rationale:** Implements phase 1 of the interceptor-contract design (docs/plans/2026-06-30-interceptor-contract-design.md). The bash-security use case ("inspect a tool call, rewrite or block it") maps directly onto `before_tool_call`, and the `permissions` extension already blocks there — it was just observe+veto. Generalizing dispatch into a transform chain, rather than special-casing tool calls, means the same mechanism serves the later provider-request seam (PII redaction, model routing) and any future seam, with one contract. Two design points required care: (1) native tools previously skipped `before_tool_call` entirely — they now run the chain too, since security/rewrite must apply uniformly; (2) on the WASM path the implementing extension is itself a `before_tool_call` subscriber that calls `tool_result` synchronously during the chain, so the pending-result channel is registered *before* running the chain to avoid dropping that result. A lower-priority interceptor that blocks short-circuits before the implementer runs, and the pending entry is cleaned up.

**Consequence:** `DispatchEvent` stays for fire-and-forget callers; `DispatchEventChain` is the transform path. `ExecuteTool` semantics change: `before_tool_call` is now dispatched for native tools (was skipped), and a blocking interceptor returns an error `toolResult` without executing the tool. A transformed payload that fails to unmarshal at the seam is tolerated (original input kept, warning logged) — a buggy interceptor can never crash a turn. Tests: `interceptor_test.go` covers `applyInterceptorResponse` (observe/transform/block/cancel), `blockReason`, `runBeforeToolCall` passthrough, and native-tool input plumbing; `sdk` round-trip tests cover the new `Payload` field. `wllrsdk.go` gains `OnInterceptToolCall`. SPECS §4 (DispatchEventChain contract) and §15 (ExecuteTool) updated.

---

## 26. after_tool_call transform chain — output-side interception

*Added: 2026-06-30*

**Decision:** `ExecuteTool` now runs the tool *result* through the `after_tool_call` transform chain (`runAfterToolCall`) on both the native and WASM paths, symmetric with the `before_tool_call` input chain. An interceptor may rewrite/redact the result (`AfterToolCallPayload` transform) or block it (block replaces the result with `blockReason(reason)` and forces `IsError=true`). `wllrsdk.go` gains `OnInterceptToolResult`.

**Rationale:** Completes the tool-call story: `before_tool_call` already lets an interceptor rewrite or block the *input* (e.g. sanitise a bash command); the output side was still observe-only via `DispatchEvent`. The motivating capability is redacting secrets out of a tool's output before the model ever sees them (e.g. an `exec` that prints an env dump) — the natural partner of input sanitisation, and impossible without an output transform. Reusing `DispatchEventChain` keeps one contract across input and output; `runAfterToolCall` mirrors `runBeforeToolCall` exactly (same malformed-payload tolerance, same block semantics). The UIBridge `AfterToolCall` notification now fires with the post-interception result so the TUI shows what the model actually received, not the raw output.

**Consequence:** `EventAfterToolCall` changes from observe-only to a transform chain — but backward compatible: an extension that returns no `Payload` still observes the result identically (the previous `DispatchEvent` call is replaced by the chain, which threads nothing when no one transforms). New `Host.runAfterToolCall`. `wllrsdk.go` gains `OnInterceptToolResult(fn) -> (newResult, newIsError, block, reason)`. Tests: `TestRunAfterToolCall_NoInterceptorsKeepsResult` (passthrough); existing native-tool test still green. SPECS §15 updated (after_tool_call is now a transform chain; UI sees the final result). With this, both tool-call seams (input + output) are transform-capable.

---

## 27. HasSubscribers + append_file + SetLogger — log-sink plumbing

*Added: 2026-06-30*

**Decision:** Add `Host.HasSubscribers(EventType) bool`, `Host.SetLogger(*slog.Logger)`, the `append_file` host method (`handleAppendFile`, `PermFileWrite`), and `CapabilityProvider.AppendFile`. These support moving log-file writing out of the Go core into a bundled `logging` WASM extension.

**Rationale:** The core slog handler (cmd/loghandler.go) batches log records and dispatches `EventLog`, but only once a sink exists — `HasSubscribers` lets it cheaply check without building/marshalling a payload that nothing consumes (and lets it keep buffering startup logs in a ring until the `logging` extension subscribes). `SetLogger` is needed because logging is configured *after* `NewHost` (the handler must hold the host to dispatch), so the host's own diagnostic logger is swapped in post-construction. `AppendFile` is the capability the log sink needs: `WriteFile` truncates, which would discard prior log lines; append is the correct semantics and is generically useful. `CapabilityProvider` gaining a method is a breaking interface change for out-of-tree implementations, but all in-tree impls (osCapabilityProvider + test doubles) are updated.

**Consequence:** `CapabilityProvider` interface grows `AppendFile(path, content string) error` — every implementation (cmd/capability.go, host_test.go, interfaces_test.go, test/wasmchat) updated. `append_file` is permission-gated identically to `write_file`. `HasSubscribers` copies the extension slice under `mu.RLock` like `DispatchEvent`. The reentrancy-guarded log dispatch lives in `cmd` (the handler), not the host — the host just provides `DispatchEvent`/`HasSubscribers`; the guard is the handler's `inDispatch` atomic.
