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
