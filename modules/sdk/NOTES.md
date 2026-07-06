# bob/sdk — Design Decisions

Append-only design decision log. Never delete entries; add an `*Addendum (date):*` if a decision is reversed.

---

## 1. EventType is a string type, not an iota int

*Added: 2026-05-06*

**Decision:** `EventType` is defined as `type EventType string` with string constants, not as an `int` with iota.

**Rationale:** WASM extensions are independently compiled binaries. Using string values means the host and an extension can agree on an event name without sharing a compiled constant table. An iota-based int would require both sides to be compiled from the same version of this package (or a byte-for-byte identical layout). String constants survive independent compilation, separate module versions, and future additions without breaking existing extensions that simply ignore unknown types.

**Consequence:** Comparison is a string equality check (O(n) in string length), but event type strings are short and fixed, so this is negligible. Serialisation produces a human-readable wire format at no extra cost.

---

## 2. Event.Payload is json.RawMessage, not interface{}

*Added: 2026-05-06*

**Decision:** `Event.Payload` (and `OnToolCallPayload.Input`, `Tool.InputSchema`, `HostCallRequest.Params`, `HostCallResponse.Result`) are typed as `json.RawMessage` rather than `interface{}` or `any`.

**Rationale:** The host reads events from the WASM guest's linear memory as raw bytes and writes them back without needing to understand the payload structure. Using `json.RawMessage` allows the host to forward the bytes directly — zero extra allocations and no reflection. An `interface{}` field would require a round-trip through `encoding/json`'s generic map/slice representation (allocations, type assertions) for every event, even when the host does not inspect the payload.

**Consequence:** Callers must manually unmarshal `Payload` into the appropriate struct after switching on `Event.Type`. This is a deliberate trade-off: the host path stays zero-allocation; callers that need typed access do the unmarshal once on their side.

---

## 3. HostCallRequest/Response use json.RawMessage for params and result

*Added: 2026-05-06*

**Decision:** `HostCallRequest.Params` is `json.RawMessage` with `omitempty`; `HostCallResponse.Result` is `json.RawMessage` with `omitempty`.

**Rationale:** Each `host_call` method has a different parameter and result shape. Encoding them as `json.RawMessage` lets the extension SDK encode method-specific structs independently, then embed the bytes directly into the envelope without a second marshal/unmarshal cycle. The host similarly extracts `Params` bytes and routes them to the appropriate handler without parsing them at the envelope layer.

**Consequence:** There is no compile-time type safety between a method name and its params/result shape. The contract is documented in SPECS.md and enforced at runtime by the host handler for each method.

---

## 4. Role is a string type, not a bool or iota

*Added: 2026-05-06*

**Decision:** `Role` is defined as `type Role string` with constants `RoleUser = "user"` and `RoleAssistant = "assistant"`.

**Rationale:** The Anthropic Messages API uses `"user"` and `"assistant"` as role identifiers. Using matching string values means `Message` can be serialised directly into provider API calls without a mapping step. Future API roles (e.g. `"tool"` or `"system"`) can be added as constants without changing the type or breaking existing code.

**Consequence:** Invalid role strings are not caught at compile time. Extensions must validate role values at runtime if they care about correctness.

---

## 5. ABIVersion is an untyped int constant, not a typed version struct

*Added: 2026-05-06*

**Decision:** `ABIVersion = 1` is a bare untyped integer constant.

**Rationale:** The WASM ABI export `_abi_version()` must return a plain `i32`. Using an untyped constant lets it be assigned to any integer type without a cast, matching the WASM host binding pattern. A typed struct would require serialisation logic that has no place in a single-function WASM export.

**Consequence:** Semantic versioning is not represented here. Breaking changes require a new constant value and corresponding host-side gating logic; there is no minor/patch distinction at the ABI boundary.

---

## 6. Permission is a string type for stable ABI compatibility

*Added: 2026-05-06*

**Decision:** `Permission` is defined as `type Permission string` with string constants.

**Rationale:** Same rationale as `EventType` (Note 1): string values survive independent compilation of host and WASM extension. Integer permission codes would require both sides to share the same constant table. String permissions are human-readable, can be declared in a JSON manifest without mapping tables, and are forward-compatible (unknown permissions are ignored or result in an error response from `request_permission`).

**Consequence:** Permission comparisons are string equality. The manifest format is simply `{"permissions":["file_read","file_write"]}` — no schema registry needed.

---

## 7. Tool.Override field added for replacement semantics

*Added: 2026-05-06*

**Decision:** `sdk.Tool` gains an `Override bool` field with `json:"override,omitempty"`.

**Rationale:** Built-in extensions register tools at load time. User extensions may want to replace a built-in tool with a custom implementation. Without an explicit override flag, the second registration returns an error ("tool already registered"). The `omitempty` tag keeps the wire format minimal — the field is absent for non-override registrations.

**Consequence:** Extensions that want to replace an existing tool must set `"override": true` in their `register_tool` host_call params. The override is unconditional: any extension can replace any tool if it sets the flag.

---

## 7a. Tool.OutputSchema documents tool results

*Added: 2026-07-04*

**Decision:** `sdk.Tool` gains an `OutputSchema json.RawMessage` field with `json:"output_schema,omitempty"`.

**Rationale:** `input_schema` documents what the model must pass into a tool. The returned value also needs a machine-readable contract for `/tools`, docs, and extension authors, especially for bundled WASM tools that return JSON strings. Current provider tool APIs only accept input schemas, so the adapter continues to forward only `InputSchema` to the LLM provider.

**Consequence:** `register_tool` may include `output_schema`. The SDK preserves it verbatim like `input_schema`; it is documentation/UI metadata and is not parsed by the SDK.

---

## 8. AgentID Added to BeforeToolCallPayload and AfterToolCallPayload

*Added: 2026-05-08*

**Decision:** Both `BeforeToolCallPayload` and `AfterToolCallPayload` gained an `AgentID string` (`agent_id` in JSON) field.

**Rationale:** With multiple agents running concurrently, a monitoring extension (e.g. a task tracker or security auditor) receives `before_tool_call` events from all agents. Without `agent_id`, the extension can only correlate by `tool_call_id` — which is unique per call but doesn't reveal which agent context the tool is executing in. Adding `agent_id` is a non-breaking addition: existing extensions that don't use the field simply ignore it (JSON unknown fields are silently skipped). The field is also included in `AfterToolCallPayload` for symmetry and to allow extensions to correlate the full before/after lifecycle without maintaining a `tool_call_id → agent_id` mapping.

**Consequence:** `Host.ExecuteTool` now takes `agentID string` as a parameter. All callers (the `sdkToolAdapter` in `harness/tools.go`) must supply the agent ID when invoking `ExecuteTool`.

---

## 9. EventOnCommand and OnCommandPayload — Extension Slash Commands

*Added: 2026-05-08*

**Decision:** `EventOnCommand` (`"on_command"`) is added with `OnCommandPayload{Name, Args}`.

**Rationale:** Extensions register slash commands via `MethodRegisterCommand`. When the user types that command, the harness needs to notify the registering extension so it can act. The simplest approach is a new event type: the harness dispatches `EventOnCommand` after the user invokes the slash command, and the extension subscribes to it to receive the call. This keeps the dispatch mechanism uniform (all extension-to-harness communication goes through events) and requires no new ABI machinery. The extension identifies which command was invoked via `OnCommandPayload.Name`.

**Consequence:** Extensions that register a command must also subscribe to `EventOnCommand` and check `payload.name` to determine which of their commands was invoked (if they register more than one). Extensions that don't subscribe to `EventOnCommand` will have their registered commands silently ignored at invocation time.

---

## 10. MethodModal, MethodSetSystemPrompt, MethodAppendSystemPrompt Added

*Added: 2026-05-08*

**Decision:** Three new `Method*` constants were added for modal display and system prompt management.

**Rationale:**

- `MethodModal`: Extensions that display large blobs of text (like AGENTS.md, skill descriptions, command output) need a focused overlay, not a chat notification. A dedicated modal avoids cluttering the conversation.
- `MethodSetSystemPrompt` / `MethodAppendSystemPrompt`: The system prompt is shared state that affects all agents. Exposing it as a host_call method allows extensions to set it at any time (not just at load time), and allows the `context` extension to reinitialize it on config reload.

**Consequence:** These methods require the host to have wired `OnModal`, `OnSetSystemPrompt`, and `OnAppendSystemPrompt` callbacks. If the callbacks are nil, the host returns an error response to the extension.

---

## 11. MCP Bridge Methods — MethodMCPSpawn/Close/Send/Read

*Added: 2026-05-08*

**Decision:** Four `MethodMCP*` constants are added to the sdk to support the MCP bridge extension managing external MCP server subprocesses.

**Rationale:** The MCP bridge extension needs to spawn, communicate with, and terminate MCP server subprocesses. These are I/O operations that require host access (process management, stdio pipes). Exposing them as host_call methods follows the existing pattern for restricted operations: the extension requests the operation, the host executes it with appropriate permissions. `mcp_spawn` requires `PermExec` because it runs an arbitrary command. `mcp_close/send/read` are allowed once the process is spawned (no additional permission check, as the exec permission was validated at spawn time).

**Consequence:** Extensions that bridge MCP servers must declare `exec` in their manifest. The host is responsible for process lifecycle; the extension only sees JSON-RPC messages via `mcp_send` / `mcp_read`.

---

## 12. MessageType field added to Message for Go-level routing

*Added: 2026-05-31*

**Decision:** `sdk.Message` gains a `Type MessageType` field (`json:"type,omitempty"`). Three constants are defined: `MessageTypeNormal` (`"normal"`), `MessageTypeSteering` (`"steering"`), `MessageTypeSystem` (`"system"`).

**Rationale:** Agent coordination requires messages that are not intended for the LLM but are instead consumed by the Go runtime (e.g. shutdown_request, AGENT_SHUTDOWN). Previously all messages were treated identically — user or assistant content sent to the provider. With multi-agent orchestration, there is a need for: (a) system-level control messages never sent to the LLM, and (b) steering messages visible in history but filtered from the provider context slice. The `type` field enables the `sdkToFantasyMessages` conversion to filter these without modifying the message store.

**Consequence:** The `omitempty` tag preserves backward compatibility: existing serialized messages with no `type` field unmarshal to `Type == ""`, which is treated identically to `MessageTypeNormal` in all code paths. Extensions that do not set `Type` are unaffected. New code must not add `MessageTypeSystem` messages to the LLM context or history.

---

## 13. UI Scene-Graph Types — Node-Based Declarative TUI (P0)

*Added: 2026-06-29*

**Decision:** Add a node-based, JSON-serializable scene-graph vocabulary to the sdk: `UINode` (+`UINodeType`), `UIProps`, `UIPatchOp` (+`UIPatchOpType`) wrapped by `UIPatchParams`, and `UIArea` (+`UIAreaPlacement`) wrapped by `UICreateAreaParams`. This is phase P0 of letting any WASM extension drive the TUI declaratively: it defines the data contract only — no host_call method, permission, or host wiring is added yet.

**Rationale:**

- The harness currently renders via an imperative `harness.Renderer` (AppendToken, AddToolCall, …) and the agent token stream is wired straight to it. To prove that any WASM component can drive the UI, rendering must become data-driven: the harness becomes a generic renderer of a node tree, and extensions emit the tree plus incremental patches.
- A scene graph addressed by stable node IDs supports partial updates and deletes, avoiding full re-serialization each token. `UIOpAppendText` is the deliberate cheap streaming op so token deltas append in place.
- `UIPatchOp.Index` is `*int` rather than `int` because `omitempty` on a plain int would drop a legitimate insert position of `0`; a nil pointer cleanly encodes "append".
- Colour props reference named theme tokens, not raw colours, so the host keeps ownership of theming when extensions describe UI.
- The "area" is the unit of ownership: an extension owns one area, injects into an existing area's scene graph, or spawns a new area. Placement is advisory; the harness composites.

**Consequence:** These types are inert until a later phase adds the `ui_create_area`/`ui_patch` host_call methods, a `ui` permission, and a generic `SceneRenderer` in the harness. Adding them now establishes a stable wire contract that the harness, the agents extension, and `wllrsdk.go` helpers can target independently. No existing behavior changes; the type set is purely additive and does not bump `ABIVersion`.

*Addendum (2026-06-29):* P1 wired these types into the runtime. `sdk` gained `MethodUICreateArea`/`MethodUIPatch`/`MethodUIRemoveArea` and `PermUI`; the extension host added the three handlers (permission-gated) plus `UIBridge.CreateArea`/`PatchUI`/`RemoveArea`; the harness added `SceneRenderer`. `ABIVersion` is unchanged (additive methods/permission). See extension NOTES §23 and harness NOTES (SceneRenderer).

---

## 14. EventToken — streaming text to extensions (UI P2)

*Added: 2026-06-29*

**Decision:** Add `EventToken` (`"token"`) and `TokenPayload{AgentID, Text}`. The harness dispatches it with each ~30ms batch of streamed assistant text, reusing the existing token batcher so the WASM crossing rate stays bounded.

**Rationale:** The goal of routing assistant text through a WASM extension (so the extension can paint it into a scene-graph area) requires the streamed text to reach WASM. A per-token event would cross the boundary thousands of times per turn; batching at the existing 30ms cadence caps it at ~33 dispatches/sec. The payload carries `AgentID` so an extension can distinguish main-agent text from sub-agent text.

**Consequence:** Event type count rises to 15. The harness token batcher gains an optional dispatch hook that forwards each flushed batch to `Host.DispatchEvent(EventToken)` on the agent goroutine, in addition to the existing `TokenMsg` chat path (the two coexist; a later phase may remove the direct chat path once the WASM path renders the transcript). `wllrsdk.go` gains `OnToken(func(agentID, text string))`.

*Addendum (2026-07-01):* Token batches are now coalesced at ~75ms instead of ~30ms to reduce terminal render/layout pressure while preserving streaming feel.

---

## 15. EventNotify — notifications to extensions (UI P4)

*Added: 2026-06-29*

**Decision:** Add `EventNotify` (`"notify"`) and `NotifyPayload{Text}`. The harness dispatches it for every notification line shown in the chat, via a single Model choke point (`pushNotification`), in a goroutine so the bubbletea loop never blocks.

**Rationale:** When a WASM extension owns the transcript (WLLR_WASM_CHAT), notifications would otherwise be lost because they were rendered only by the internal `ChatView`. Routing them as an event lets the transcript-owning extension render them as system lines. The event is dispatched regardless of origin (extension `notify`, `/model`, reload, extension errors) because all of these funnel through the Model's notification path. It is also generally useful to any extension (e.g. logging) and is subscription-gated, so existing extensions are unaffected.

**Consequence:** Event type count rises to 16. Extensions must not call `notify` from within an `OnNotify` handler (it would recurse). `wllrsdk.go` gains `OnNotify(func(text string))`. The harness `pushNotification` replaces direct `m.chat.AddNotification` calls and the `NotifyMsg` handler.

---

## 16. UIArea Sizing Constraints and UIUpdateAreaParams — Dynamic Area Layout

*Added: 2026-06-30*

**Decision:** `UIArea` gains four optional constraint fields (`MinHeight`, `MaxHeight`, `MinWidth`, `MaxWidth`), each accepting either an absolute cell/line count (`"3"`) or a percentage of the terminal dimension (`"20%"`). A new `UIUpdateAreaParams` struct and `MethodUIUpdateArea` (`"ui_update_area"`) host_call allow extensions to change constraints after area creation. `UIAreaInput` placement constant is added to document the harness-owned input box slot.

**Rationale:** The statusline scene design requires areas that can grow and shrink dynamically (e.g. collapse to 0 lines when idle, expand to show sub-agent status). Without constraints, extensions would have to truncate/pad themselves in the scene tree, duplicating logic across every extension. Placing constraint resolution in the harness means a single implementation handles all areas consistently. Percentage values allow layouts to adapt to any terminal size without hardcoding widths. `UIUpdateAreaParams` enables runtime resize without tearing down and recreating the area (which would lose the scene tree). `UIAreaInput` is added so extensions can reference the logical slot in documentation and layout queries even though the harness always owns it.

**Consequence:** `UIArea` wire format gains four new `omitempty` fields — backward-compatible (missing fields = unconstrained). `sceneArea` struct in `modules/harness` gains matching fields. `SceneRenderer` gains `UpdateArea`, `ConstrainWidth`, `ConstrainHeight` methods. `ui_update_area` is routed through `UIBridge` in the extension host, same permission gate as other UI methods (`PermUI`).

---

## 17. EventResponse.Payload — transform-capable interception

*Added: 2026-06-30*

**Decision:** Add an optional `Payload json.RawMessage` field to `EventResponse`. An interceptor's `_on_event` may now return a **transformed event payload** (same JSON shape as the incoming `Event.Payload`) in addition to the existing observe (nil) and veto (`Cancel`/`Block`) outcomes.

**Rationale:** This is the single capability gap behind the interceptor-contract design (docs/plans/2026-06-30-interceptor-contract-design.md). Before this, `EventResponse` was observe + veto only: an extension could watch an interaction and cancel/block it, but could not *change* it. Three concrete features all need exactly that — a bash-security layer rewriting a command, PII/key redaction editing the messages sent to the LLM, and cheap/frontier model routing overriding the request model. Rather than add a new response type or a parallel event mechanism, extending `EventResponse` with one `omitempty` field generalizes the existing event/dispatch infra into a transform pipeline (`Host.DispatchEventChain`). It is the smallest change that unlocks the whole class: "observe an interaction, then optionally transform / reroute / block it."

**Consequence:** `EventResponse` gains `Payload`; empty means "no transformation" (backward compatible — existing observe/veto handlers are unaffected, and a zero `EventResponse` still marshals to `{}`). The host threads `Payload` through interceptors in priority order and applies the final payload at the seam. The first seam wired is `before_tool_call` (input rewrite / block); `before_provider_request` (PII + routing) follows in a later phase. `wllrsdk.go` gains an interceptor registry (`_sdkOnIntercept`) and `OnInterceptToolCall`. ABIVersion is unchanged (additive field).

---

## 18. EventLog + LogBatchPayload + append_file — logging as a component seam

*Added: 2026-06-30*

**Decision:** Add `EventLog` (`"log"`), `LogRecord`/`LogAttr`/`LogBatchPayload`, and `MethodAppendFile` (`"append_file"`). `EventLog` carries a coalesced batch of structured log records; `append_file` appends to a host file (creating it + parents), gated by `PermFileWrite`.

**Rationale:** File-log writing moved out of the Go core into a bundled `logging` WASM extension, and any extension can now hook logs (observability, shipping to a backend — pairs naturally with the existing otel-traces extension). The host's slog handler converts records to `LogRecord` (level name lowercased, time RFC3339Nano UTC, attrs pre-stringified to `LogAttr{Key,Value}` preserving emission order so extensions never need slog's typed Value) and dispatches batches via `EventLog`. Batching mirrors `EventToken` (~30ms) to bound the WASM crossing rate. `append_file` exists because `write_file` truncates — a log sink needs append semantics; it is a generic, reusable capability rather than a logging-specific sink call.

**Consequence:** Event type count rises to 17. `LogAttr.Value` is a pre-stringified string (the host calls `slog.Value.String()`), so structured numeric/bool attrs arrive as text — acceptable for a text-log sink and keeps the wire format trivial. `append_file` requires `file_write`. `wllrsdk.go` gains `OnLog`, `AppendFile`, and the `LogRecord`/`LogAttr` types. The reentrancy guard (logs emitted while dispatching `EventLog` are dropped) lives in the host-side handler, not the ABI, but is documented so extension authors don't log from `OnLog`. ABIVersion unchanged (additive).

---

## 19. EventModelChanged — provider/model status updates

*Added: 2026-07-01*

**Decision:** Add `EventModelChanged` (`"model_changed"`) and `ModelChangedPayload{Provider, Model}`.

**Rationale:** Display extensions need an immediate signal when provider/model state changes. Polling `get_status_info` on `tick` can miss startup ordering and delays visible updates after `/model` or the first-run provider wizard.

**Consequence:** Event type count rises to 18. Extensions can subscribe to `model_changed` and update status UI without polling. The harness dispatches the event after its live provider/model state is updated.

---

## 20. AgentList Liveness Fields

*Added: 2026-07-04*

**Decision:** The `agent_list` host_call result includes live state fields for each agent: `is_running`, `working`, `liveness`, `pending_messages`, `last_activity_age_ms`, `turn_duration_ms`, `last_tool_age_ms`, `last_tool_done_age_ms`, `active_tool`, `last_tool`, and `shutdown_requested`, in addition to identity fields.

**Rationale:** Orchestrating agents need a reliable no-side-effect snapshot to distinguish active work from a stalled or idle child. Repeated status pings inside the same turn do not create progress; exposing liveness in the list result lets UI and agent tools present one actionable snapshot instead.

*Addendum (2026-07-05):* Runtime liveness consumers should prefer explicit `working`/`liveness` fields when available. A running child is working unless a concrete dead state is reported; orchestrators should wait for child notifications rather than using repeated polling as a waiting mechanism.

**Consequence:** The result shape is additive and backward-compatible for extensions that only read `id` and `name`. Extensions can now surface current activity and graceful-shutdown state without adding a new host_call method.
