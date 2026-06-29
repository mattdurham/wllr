# bob/sdk — Interface Contracts and Behavioral Invariants

Package `sdk` defines shared types for the bob coding harness and its WASM extensions.
These types cross the host/WASM boundary via JSON; their wire format is stable across ABI versions.

---

## ABIVersion

- `ABIVersion = 1` (int, untyped constant)
- Extensions that export `_abi_version() i32` must return this value when strict version checking is enabled.
- Strict version checking is optional in v1; the host may accept extensions that omit the export.

---

## EventType Constants

`EventType` is a `string` typedef. All values are stable across ABI versions — the host and extensions compare them as plain strings; no numeric mapping exists.

| Constant                      | Wire value                  | When dispatched                                              |
|-------------------------------|-----------------------------|--------------------------------------------------------------|
| `EventSessionStart`           | `"session_start"`           | A new session begins                                          |
| `EventBeforeAgentStart`       | `"before_agent_start"`      | An agent is about to start (prompt available)                 |
| `EventBeforeProviderRequest`  | `"before_provider_request"` | The host is about to call the LLM provider                   |
| `EventAfterProviderResponse`  | `"after_provider_response"` | The LLM provider has returned a response                     |
| `EventOnToolCall`             | `"on_tool_call"`            | The LLM has emitted a tool-call (observation only)           |
| `EventOnToolResult`           | `"on_tool_result"`          | A tool result has been produced (observation only)           |
| `EventMessageStart`           | `"message_start"`           | A new message stream begins                                  |
| `EventMessageEnd`             | `"message_end"`             | A message stream has completed                               |
| `EventShutdown`               | `"shutdown"`                | The host is shutting down; extensions should flush state     |
| `EventBeforeToolCall`         | `"before_tool_call"`        | Dispatched to the extension that implements a tool; extension MUST call `tool_result` |
| `EventAfterToolCall`          | `"after_tool_call"`         | Dispatched after a tool execution completes                  |
| `EventOnCommand`              | `"on_command"`              | User invoked a slash command registered by an extension      |
| `EventTick`                   | `"tick"`                    | Periodic heartbeat dispatched by the host at a configured interval |
| `EventContextUsage`           | `"context_usage"`           | Dispatched after each completed turn with context window usage |

**Invariants:**
- The set of `EventType` string values must not change between ABI versions without a version bump.
- An unknown `EventType` must be silently ignored by extensions (forward-compatibility).
- There are exactly 14 defined event types.

---

## Event / EventResponse JSON Contract

### Event

```json
{ "type": "<EventType>", "payload": <raw-JSON-or-null> }
```

- `type` — required; one of the `EventType` string constants.
- `payload` — a `json.RawMessage`; always valid JSON (object, array, or null). Never an empty byte slice.

### EventResponse

```json
{ "cancel": true, "block": true, "error": "message" }
```

All fields are `omitempty`.

| Field    | Type   | Meaning                                                            |
|----------|--------|--------------------------------------------------------------------|
| `cancel` | bool   | Request the host to cancel the current operation                   |
| `block`  | bool   | Request the host to block/suppress the current output or action    |
| `error`  | string | Report an extension error to the host                              |

---

## Payload Types (14 total)

### SessionStartPayload (`EventSessionStart`)

| Field    | Type   | Description                                          |
|----------|--------|------------------------------------------------------|
| `reason` | string | Why the session was started (e.g. `"new_session"`)   |

### BeforeAgentStartPayload (`EventBeforeAgentStart`)

| Field           | Type   | Description                      |
|-----------------|--------|----------------------------------|
| `prompt`        | string | The user-facing agent prompt     |
| `system_prompt` | string | The system prompt for this agent |

### BeforeProviderRequestPayload (`EventBeforeProviderRequest`)

| Field      | Type       | Description                           |
|------------|------------|---------------------------------------|
| `messages` | []Message  | Full message history sent to provider |
| `model`    | string     | Model identifier string               |

### AfterProviderResponsePayload (`EventAfterProviderResponse`)

| Field   | Type       | Description                |
|---------|------------|----------------------------|
| `usage` | UsageStats | Token usage from this call |

### OnToolCallPayload (`EventOnToolCall`)

| Field          | Type            | Description                                |
|----------------|-----------------|--------------------------------------------|
| `tool_call_id` | string          | Unique identifier for this tool invocation |
| `tool_name`    | string          | Name of the tool being called              |
| `input`        | json.RawMessage | Raw JSON input arguments                   |

### OnToolResultPayload (`EventOnToolResult`)

| Field          | Type   | Description                                   |
|----------------|--------|-----------------------------------------------|
| `tool_call_id` | string | Matches the corresponding `OnToolCallPayload` |
| `result`       | string | Text result from the tool                     |
| `is_error`     | bool   | True if the tool returned an error result     |

### MessageStartPayload (`EventMessageStart`)

| Field  | Type   | Description                        |
|--------|--------|------------------------------------|
| `role` | string | Role of the message being started  |

### MessageEndPayload (`EventMessageEnd`)

| Field     | Type   | Description                   |
|-----------|--------|-------------------------------|
| `role`    | string | Role of the completed message |
| `content` | string | Full accumulated text content |

### ShutdownPayload (`EventShutdown`)

| Field    | Type   | Description                    |
|----------|--------|--------------------------------|
| `reason` | string | Human-readable shutdown reason |

### BeforeToolCallPayload (`EventBeforeToolCall`)

| Field          | Type            | Description                                                     |
|----------------|-----------------|-----------------------------------------------------------------|
| `agent_id`     | string          | ID of the agent that issued the tool call                       |
| `tool_call_id` | string          | Unique identifier for this tool invocation                      |
| `tool_name`    | string          | Name of the tool to execute                                     |
| `input`        | json.RawMessage | Raw JSON input arguments for the tool                           |

**Invariant:** The extension receiving `EventBeforeToolCall` MUST call `tool_result` (via `host_call`) before returning from `_on_event`. Not doing so blocks the host's `ExecuteTool` call until context cancellation.

### AfterToolCallPayload (`EventAfterToolCall`)

| Field          | Type   | Description                                       |
|----------------|--------|---------------------------------------------------|
| `agent_id`     | string | ID of the agent that issued the tool call         |
| `tool_call_id` | string | Matches the corresponding `BeforeToolCallPayload` |
| `tool_name`    | string | Name of the tool that was executed                |
| `result`       | string | Text result of the tool execution                 |
| `is_error`     | bool   | True if the tool returned an error result         |

### OnCommandPayload (`EventOnCommand`)

| Field  | Type     | Description                                                    |
|--------|----------|----------------------------------------------------------------|
| `name` | string   | The command name (without leading `/`)                         |
| `args` | []string | Arguments the user typed after the command name                |

### TickPayload (`EventTick`)

No payload fields — the event carries no structured data beyond the event type itself. The payload is `null`.

### ContextUsagePayload (`EventContextUsage`)

| Field       | Type         | Description                                                                     |
|-------------|--------------|---------------------------------------------------------------------------------|
| `usage`     | ContextUsage | Context window usage for the turn (see ContextUsage type in Supporting Types)  |
| `compacted` | bool         | `true` when `compactHistory` ran and succeeded during the turn                  |

**Note for extension authors:** `ContextUsage.Percent` is expressed as a percentage (0–100, e.g. 75.4 means 75.4% full). This is distinct from the pool's internal `ThresholdPct`, which is a fraction (0.0–1.0). Do not compare `Percent` directly to `ThresholdPct`; convert as needed.

---

## Permission Model

### Permission Type

`Permission` is a `string` typedef. Values are stable across ABI versions.

| Constant            | Wire value        | Capability granted                             |
|---------------------|-------------------|------------------------------------------------|
| `PermExec`          | `"exec"`          | Execute arbitrary commands                     |
| `PermFileOpen`      | `"file_open"`     | Open files for reading                         |
| `PermFileRead`      | `"file_read"`     | Read file contents                             |
| `PermFileWrite`     | `"file_write"`    | Write file contents                            |
| `PermNetworkRead`   | `"network_read"`  | Read from the network                          |
| `PermNetworkWrite`  | `"network_write"` | Write to the network                           |

**Invariants:**
1. Trusted extensions (loaded via `Host.LoadBytes` with `trusted=true`) have all permissions granted implicitly; no manifest is required.
2. Untrusted extensions (loaded via `Host.Load`) declare permissions in a companion `<basename>.json` manifest as `{"permissions":["file_read",...]}`.
3. Undeclared permissions are denied; the `request_permission` host_call returns an error response.

### ExtensionManifest

```json
{ "permissions": ["file_read", "file_write"] }
```

| Field         | Type           | Description                                |
|---------------|----------------|--------------------------------------------|
| `permissions` | []Permission   | List of permissions the extension requires |

---

## Supporting Types

### UsageStats

| Field           | Type | Description            |
|-----------------|------|------------------------|
| `input_tokens`  | int  | Prompt tokens used     |
| `output_tokens` | int  | Completion tokens used |

### ContextUsage

| Field           | Type    | Description                                                                 |
|-----------------|---------|-----------------------------------------------------------------------------|
| `InputTokens`   | int64   | Total input tokens for the last turn                                        |
| `OutputTokens`  | int64   | Total output tokens for the last turn                                       |
| `ContextWindow` | int64   | Model's maximum context window (from `WLLR_CONTEXT_WINDOW` or model default) |
| `Percent`       | float64 | `InputTokens / ContextWindow × 100`; `0` if `ContextWindow == 0`           |

Note: Percent is 0–100. CompactConfig.ThresholdPct is a fraction 0.0–1.0.

### Role Constants

`Role` is a `string` typedef.

| Constant        | Wire value    |
|-----------------|---------------|
| `RoleUser`      | `"user"`      |
| `RoleAssistant` | `"assistant"` |

**Invariant:** Role string values must not change; extensions may hard-code them.

### MessageType

`MessageType` is a `string` typedef. Controls routing and LLM visibility of a `Message`.

| Constant              | Wire value    | Meaning                                                              |
|-----------------------|---------------|----------------------------------------------------------------------|
| `MessageTypeNormal`   | `"normal"`    | Regular user/assistant message; included in LLM context             |
| `MessageTypeSteering` | `"steering"`  | Guidance message; in history but filtered from LLM context slice    |
| `MessageTypeSystem`   | `"system"`    | Go-level control message (e.g. shutdown_request); never sent to LLM, not written to history |

**Invariants:**
- An empty `Type` field (zero value) is treated as normal everywhere; it is omitted from JSON via `omitempty` for backward compatibility.
- `MessageTypeSystem` messages carry non-empty JSON payloads in `Content`; they are never empty strings.
- `MessageType` string values must not change across ABI versions.

### Message

| Field     | Type        | JSON           | Description                                                   |
|-----------|-------------|----------------|---------------------------------------------------------------|
| `role`    | Role        | `"role"`       | `"user"` or `"assistant"`                                     |
| `content` | string      | `"content"`    | Text content of the message                                   |
| `type`    | MessageType | `"type,omitempty"` | Message classification; absent/empty means normal         |

### Tool

| Field          | Type            | Description                                                              |
|----------------|-----------------|--------------------------------------------------------------------------|
| `name`         | string          | Tool identifier                                                          |
| `description`  | string          | Human-readable description                                               |
| `input_schema` | json.RawMessage | JSON Schema object describing the tool's input; forwarded verbatim to the LLM provider |
| `override`     | bool (omitempty) | When true, allows this registration to replace an existing tool with the same name |

**Invariant:** `InputSchema` is preserved as raw bytes through marshal/unmarshal; the sdk never parses it.

---

## HostCallRequest / HostCallResponse Contract

### HostCallRequest

```json
{ "method": "<MethodName>", "params": <raw-JSON-or-omitted> }
```

| Field    | Type            | Description                                     |
|----------|-----------------|-------------------------------------------------|
| `method` | string          | One of the `Method*` constants                  |
| `params` | json.RawMessage | Method-specific parameters; omitted when absent |

### HostCallResponse

```json
{ "result": <raw-JSON-or-omitted>, "error": "message-or-omitted" }
```

| Field    | Type            | Description                                       |
|----------|-----------------|---------------------------------------------------|
| `result` | json.RawMessage | Method-specific return value; omitted on error    |
| `error`  | string          | Non-empty when the host encountered an error      |

**Invariant:** Exactly one of `result` or `error` will be non-zero in a well-formed response. Extensions should check `error` before using `result`.

---

## host_call Method Constants (30 total)

### Core methods

| Constant                  | Wire value             | Purpose                                                       |
|---------------------------|------------------------|---------------------------------------------------------------|
| `MethodSubscribe`         | `"subscribe"`          | Subscribe to one or more event types                          |
| `MethodRegisterTool`      | `"register_tool"`      | Advertise a tool the extension can handle                     |
| `MethodRegisterCommand`   | `"register_command"`   | Register a slash command the extension provides               |
| `MethodSendMessage`       | `"send_message"`       | Inject a message into the conversation                        |
| `MethodSetStatus`         | `"set_status"`         | Update the extension's status in the host UI                  |
| `MethodNotify`            | `"notify"`             | Send a notification to the host/user                          |
| `MethodToolResult`        | `"tool_result"`        | Return the result of a tool call the extension handled        |
| `MethodStoreSet`          | `"store_set"`          | Persist a key-value pair in the host's extension store        |
| `MethodStoreGet`          | `"store_get"`          | Retrieve a value from the host's extension store              |
| `MethodAbort`             | `"abort"`              | Signal the host to abort the current agent operation          |
| `MethodRequestPermission` | `"request_permission"` | Check whether the extension holds a given permission          |
| `MethodGetEnv`            | `"get_env"`            | Read a host environment variable (no permission required)     |
| `MethodConfigRead`        | `"config_read"`        | Read the calling extension's config group from the shared config file |
| `MethodModal`             | `"modal"`              | Display text in a modal overlay window                        |
| `MethodSetSystemPrompt`   | `"set_system_prompt"`  | Replace the base system prompt on all agents                  |
| `MethodAppendSystemPrompt`| `"append_system_prompt"`| Append text to the existing base system prompt               |
| `MethodExec`              | `"exec"`               | Execute a shell command on the host (requires PermExec)       |
| `MethodBeforeToolCall`    | `"before_tool_call"`   | Intercept a tool call before execution (observation constant) |
| `MethodAfterToolCall`     | `"after_tool_call"`    | Observe a tool result after execution (observation constant)  |

### Agent management methods

| Constant                  | Wire value              | Purpose                                       |
|---------------------------|-------------------------|-----------------------------------------------|
| `MethodAgentSpawn`        | `"agent_spawn"`         | Create and register a new sub-agent           |
| `MethodAgentClose`        | `"agent_close"`         | Cancel and remove a sub-agent                 |
| `MethodAgentSendMessage`  | `"agent_send_message"`  | Send a message to a named agent               |
| `MethodAgentList`         | `"agent_list"`          | Return all live agent IDs and names           |
| `MethodAgentTokenCount`   | `"agent_token_count"`   | Return total token count across all agents    |

### Team management methods

| Constant                  | Wire value              | Purpose                                       |
|---------------------------|-------------------------|-----------------------------------------------|
| `MethodTeamCreate`        | `"team_create"`         | Create a new named team                       |
| `MethodTeamClose`         | `"team_close"`          | Cancel all members and remove the team        |
| `MethodTeamAddMember`     | `"team_add_member"`     | Add an agent to a team                        |
| `MethodTeamRemoveMember`  | `"team_remove_member"`  | Remove an agent from a team (no cancel)       |

### MCP bridge methods

| Constant         | Wire value    | Purpose                                            |
|-----------------|---------------|----------------------------------------------------|
| `MethodMCPSpawn` | `"mcp_spawn"` | Spawn an MCP server subprocess (requires PermExec) |
| `MethodMCPClose` | `"mcp_close"` | Terminate an MCP server subprocess                 |
| `MethodMCPSend`  | `"mcp_send"`  | Write JSON-RPC data to an MCP server's stdin       |
| `MethodMCPRead`  | `"mcp_read"`  | Read a JSON-RPC response from an MCP server's stdout |

**Invariant:** Method strings must not change between ABI versions without a version bump.

---

## UI Scene Graph (P0 — types only)

These types describe a declarative, node-based view of an area of the TUI. They
are pure data definitions in this phase: no host_call method is wired to them
yet (that lands in a later phase). They cross the host/WASM boundary as JSON.

### UINodeType

`UINodeType` is a `string` typedef selecting a rendering primitive. Stable across ABI versions.

| Constant         | Wire value   | Renders as                                   |
|------------------|--------------|----------------------------------------------|
| `UINodeText`     | `"text"`     | Leaf text node (uses `Text`)                 |
| `UINodeVStack`   | `"vstack"`   | Vertical join of children                    |
| `UINodeHStack`   | `"hstack"`   | Horizontal join of children                  |
| `UINodeViewport` | `"viewport"` | Scrollable region wrapping one child subtree |
| `UINodeSpinner`  | `"spinner"`  | Animated activity indicator                  |
| `UINodeDivider`  | `"divider"`  | Horizontal rule                              |

**Invariant:** An unknown `UINodeType` must render as an empty box (forward-compatibility).

### UINode

```json
{ "id": "msg-1", "type": "vstack", "text": "", "props": {…}, "children": [ … ] }
```

- `id` — required; unique within the owning area; the address for incremental patches.
- `type` — required; one of `UINodeType`.
- `text` — meaningful only for `UINodeText`; omitted when empty.
- `props` — optional `*UIProps`; omitted when nil.
- `children` — meaningful only for container types; omitted when empty.

### UIProps

Optional style/layout. Colour fields (`fg`, `bg`) reference **named theme tokens**, never raw colours, so the host owns theming. `width`/`height` accept `"fill"`, `"auto"`, or a decimal cell count. `padding`/`margin` are length 1 (all sides), 2 (`[v,h]`), or 4 (`[t,r,b,l]`). A nil `*UIProps` means "inherit / harness defaults".

### UIPatchOp / UIPatchParams

`UIPatchOpType` selects a mutation; the host applies a batch in order, all-or-nothing.

| Constant         | Wire value      | Meaningful fields            | Effect                                            |
|------------------|-----------------|------------------------------|---------------------------------------------------|
| `UIOpSetRoot`    | `"set_root"`    | `node`                       | Replace the area's whole scene graph              |
| `UIOpInsert`     | `"insert"`      | `parent`, `index`, `node`    | Insert `node` under `parent` at `index`           |
| `UIOpUpdate`     | `"update"`      | `id`, `props`                | Replace the props of node `id`                    |
| `UIOpRemove`     | `"remove"`      | `id`                         | Remove node `id` and its subtree                  |
| `UIOpAppendText` | `"append_text"` | `id`, `text`                 | Append `text` to text node `id` (streaming op)    |

- `UIPatchOp.Index` is `*int`: a nil index appends; an index of `0` is preserved on the wire (not dropped by `omitempty`).
- `UIPatchOp.Parent == ""` for `UIOpInsert` targets the area root container.
- `UIPatchParams` wraps `{ "area": "<id>", "ops": [ … ] }`. If any op references a missing node the host rejects the whole batch.

### UIArea / UICreateAreaParams

An area is a named screen region owned by exactly one extension. `UIAreaPlacement` (`"main"`, `"sidebar"`, `"status"`, `"overlay"`) is an advisory layout hint; the harness owns final layout. `Weight` is a relative size hint among areas sharing a placement (`0` = harness default). Area `ID` is unique across all areas; the host rejects a create for an existing ID.

**Invariant:** These types are additive in P0 and introduce no new callable host_call method or permission; they have no runtime effect until a later phase wires them.

---

## Error Codes

Returned as `int32` from the `host_call` WASM import (not the JSON layer).

| Constant     | Value | Meaning                                        |
|--------------|-------|------------------------------------------------|
| `ErrOK`      | `0`   | Success                                        |
| `ErrGeneral` | `1`   | Unspecified host-side error; check JSON error field |
| `ErrCancel`  | `2`   | The operation was cancelled by the host        |

**Invariant:** Error code values must not change across ABI versions.

---

## Key Package Invariants

1. `EventType` values are stable string constants across ABI versions; numeric iota is never used.
2. `Event.Payload` is always valid JSON or explicitly `null`; it is never an empty byte slice on the wire.
3. `Tool.InputSchema` is stored and forwarded as `json.RawMessage`; the sdk never unmarshals it.
4. `HostCallRequest.Params` and `HostCallResponse.Result` are `json.RawMessage` so the sdk can forward them to/from WASM memory without any intermediate allocation.
5. `Role` string values (`"user"`, `"assistant"`) are stable and may be compared with `==`.
6. All `omitempty` fields in `EventResponse` and `HostCallRequest`/`HostCallResponse` are intentional; the wire format stays minimal.
7. `ABIVersion = 1` is the sole ABI version; future versions increment this constant and may add new event types or methods.
8. Error codes are `int32` and exist only at the WASM boundary; they do not appear in the JSON layer.
9. `BeforeToolCallPayload` and `AfterToolCallPayload` both include `agent_id` to allow extensions to correlate tool calls with the originating agent.
10. `OnCommandPayload` is the payload for `EventOnCommand`, dispatched when a user invokes an extension-registered slash command.
11. `ContextUsage.Percent` is always `InputTokens / ContextWindow * 100`; when `ContextWindow == 0` the value is exactly `0.0` (never NaN or Inf).
12. `ContextUsagePayload.Compacted` is `true` only when `compactHistory` ran and succeeded during the turn that produced the event.
13. `EventContextUsage` (`"context_usage"`) is fired once per completed turn, after the turn's usage is stored, only on success (not on error or cancellation).
14. `MethodGetContextUsage` (`"get_context_usage"`) requires no permission; it returns a zero-valued `ContextUsage` when the agent bridge is unavailable.
15. UI scene-graph types (`UINode`, `UIProps`, `UIPatchOp`, `UIPatchParams`, `UIArea`, `UICreateAreaParams`) are pure JSON data definitions; `UIPatchOp.Index` is a `*int` so a valid index of `0` survives the wire while a nil index (append) is omitted.
