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
| `EventToken`                  | `"token"`                   | Dispatched with a batch (~75ms) of streamed assistant text (TokenPayload) |
| `EventNotify`                 | `"notify"`                  | Dispatched when a system notification line is shown in chat (NotifyPayload) |
| `EventLog`                    | `"log"`                     | Dispatched with a batch (~30ms) of structured log records (LogBatchPayload) |
| `EventModelChanged`           | `"model_changed"`           | Dispatched after active provider/model status changes (ModelChangedPayload) |

**Invariants:**

- The set of `EventType` string values must not change between ABI versions without a version bump.
- An unknown `EventType` must be silently ignored by extensions (forward-compatibility).
- There are exactly 18 defined event types.
- `EventToken` carries a `TokenPayload{AgentID, Text}`; batches are coalesced by the harness so the WASM crossing rate stays bounded (~13/sec) regardless of provider speed.
- `EventNotify` carries a `NotifyPayload{Text}`; it is dispatched for every notification line shown in the chat, regardless of origin (extension `notify` call, model change, reload, extension error). Handlers must not call `notify` to avoid a dispatch loop.
- `EventLog` carries a `LogBatchPayload{Records []LogRecord}`; the host's slog handler coalesces records (~30ms) and forwards them so extensions can act as log sinks. The dispatch path is reentrancy-guarded — logs emitted while dispatching `EventLog` are not re-dispatched — so handlers must not rely on logging from within an `EventLog` handler.
- `EventModelChanged` carries a `ModelChangedPayload{Provider, Model}` and is dispatched after the harness updates its live status state for provider/model changes.

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

## Payload Types

### SessionStartPayload (`EventSessionStart`)

| Field    | Type   | Description                                          |
|----------|--------|------------------------------------------------------|
| `reason` | string | Why the session was started (e.g. `"new_session"`)   |

### BeforeAgentStartPayload (`EventBeforeAgentStart`)

| Field           | Type   | Description                      |
|-----------------|--------|----------------------------------|
| `prompt`        | string | The user-facing agent prompt     |
| `system_prompt` | string | The system prompt for this agent |
| `queued`        | bool   | True when the prompt is waiting in the agent inbox rather than starting immediately |

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
| `threshold_pct` | float64     | Compaction trigger threshold as a percentage (0–100, e.g. 80 means 80%)       |

**Note for extension authors:** `ContextUsage.Percent` is expressed as a percentage (0–100, e.g. 75.4 means 75.4% full). `threshold_pct` is the compaction trigger threshold as a percentage (0–100). To compute remaining-to-threshold, use `threshold_pct - percent`. Do not hard-code the threshold value.

### ModelChangedPayload (`EventModelChanged`)

| Field      | Type   | Description                         |
|------------|--------|-------------------------------------|
| `provider` | string | Active provider identifier          |
| `model`    | string | Active model identifier, if known   |

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
| `PermUI`            | `"ui"`            | Drive the TUI scene graph (areas + patches)    |

**Invariants:**

1. All extensions — trusted built-ins and untrusted user extensions alike — are held to their declared permissions (least privilege). Trusted built-ins loaded via `Host.LoadBytes` declare permissions explicitly; no manifest is required, but the supplied permission slice is the complete grant. `Host.HasPermission` returns true only for explicitly granted permissions. An extension granted `file_read`/`file_write` cannot call `exec`, `http_post`, `http_get`, or `mcp_spawn`.
2. Untrusted extensions (loaded via `Host.Load`) declare permissions in a companion manifest. The canonical format is `<basename>.json` (`{"permissions":["file_read",...]}`); YAML (`<basename>.yaml`/`.yml`) is accepted for parity with build metadata. Permission names are normalized against the SDK constants — unknown names are dropped and reported at load time.
3. Undeclared permissions are denied; the `request_permission` host_call returns an error response.

### ExtensionManifest

```json
{ "permissions": ["file_read", "file_write"] }
```

| Field         | Type           | Description                                |
|---------------|----------------|--------------------------------------------|
| `permissions` | []Permission   | List of permissions the extension requires |

**Invariant:** Manifest `tools` declarations (present in some extension JSON files) are documentation-only and are not consumed by the host. Tools are registered at runtime via the `register_tool` host_call, so schema validation against runtime registrations is not required.

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
| `output_schema` | json.RawMessage (omitempty) | JSON Schema object describing the tool's text/JSON output; preserved for docs/UI |
| `override`     | bool (omitempty) | When true, allows this registration to replace an existing tool with the same name |

**Invariant:** `InputSchema` and `OutputSchema` are preserved as raw bytes through marshal/unmarshal; the sdk never parses them.

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

## host_call Method Constants (55 total)

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
| `MethodGetOS`             | `"get_os"`             | Returns the host operating system and architecture strings    |
| `MethodConfigRead`        | `"config_read"`        | Read the calling extension's config group from the shared config file |
| `MethodModal`             | `"modal"`              | Display text in a modal overlay window                        |
| `MethodSetSystemPrompt`   | `"set_system_prompt"`  | Replace the base system prompt on all agents                  |
| `MethodAppendSystemPrompt`| `"append_system_prompt"`| Append text to the existing base system prompt               |
| `MethodExec`              | `"exec"`               | Execute a shell command on the host (requires PermExec)       |
| `MethodReadFile`          | `"read_file"`          | Read file contents on the host (requires PermFileRead)        |
| `MethodWriteFile`         | `"write_file"`         | Write content to a file on the host (requires PermFileWrite)  |
| `MethodAppendFile`        | `"append_file"`        | Append content to a file, creating it if absent (requires PermFileWrite) |
| `MethodHTTPPost`          | `"http_post"`          | Make an HTTP POST request (requires PermNetworkWrite)         |
| `MethodHTTPGet`           | `"http_get"`           | Make an HTTP GET request (requires PermNetworkRead)           |
| `MethodBeforeToolCall`    | `"before_tool_call"`   | Intercept a tool call before execution (observation constant) |
| `MethodAfterToolCall`     | `"after_tool_call"`    | Observe a tool result after execution (observation constant)  |

### Agent management methods

| Constant                  | Wire value              | Purpose                                       |
|---------------------------|-------------------------|-----------------------------------------------|
| `MethodAgentSpawn`        | `"agent_spawn"`         | Create and register a new sub-agent           |
| `MethodAgentClose`        | `"agent_close"`         | Cancel and remove a sub-agent                 |
| `MethodAgentSendMessage`  | `"agent_send_message"`  | Send a message to a named agent               |
| `MethodAgentDeliver`      | `"agent_deliver"`       | Append message to inbox and trigger execution |
| `MethodAgentRun`          | `"agent_run"`           | Trigger an immediate agent turn               |
| `MethodAgentList`         | `"agent_list"`          | Return live agent identity, queue, and liveness state |
| `MethodAgentTokenCount`   | `"agent_token_count"`   | Return total token count across all agents    |
| `MethodMailboxSnapshot`   | `"mailbox_snapshot"`    | Get read-only copy of agent inbox messages    |
| `MethodMailboxDelete`     | `"mailbox_delete"`      | Remove one or more messages from inbox        |
| `MethodMailboxEdit`       | `"mailbox_edit"`        | Update message content in inbox               |
| `MethodQueuedMessages`    | `"queued_messages"`     | Return all pending queued messages for an agent |
| `MethodAgentResetHistory` | `"agent_reset_history"` | Replace agent's conversation history          |

### Team management methods

| Constant                  | Wire value              | Purpose                                       |
|---------------------------|-------------------------|-----------------------------------------------|
| `MethodTeamCreate`        | `"team_create"`         | Create a new named team                       |
| `MethodTeamClose`         | `"team_close"`          | Cancel all members and remove the team        |
| `MethodTeamAddMember`     | `"team_add_member"`     | Add an agent to a team                        |
| `MethodTeamRemoveMember`  | `"team_remove_member"`  | Remove an agent from a team (no cancel)       |
| `MethodTeamGetInfo`       | `"team_get_info"`       | Return member agent IDs for a team            |
| `MethodTeamList`          | `"team_list"`           | Return all registered team IDs                |

### MCP bridge methods

| Constant         | Wire value    | Purpose                                            |
|-----------------|---------------|----------------------------------------------------|
| `MethodMCPSpawn` | `"mcp_spawn"` | Spawn an MCP server subprocess (requires PermExec) |
| `MethodMCPClose` | `"mcp_close"` | Terminate an MCP server subprocess                 |
| `MethodMCPSend`  | `"mcp_send"`  | Write JSON-RPC data to an MCP server's stdin       |
| `MethodMCPRead`  | `"mcp_read"`  | Read a JSON-RPC response from an MCP server's stdout |

### UI methods

| Constant           | Wire value         | Purpose                                                |
|-------------------|--------------------|--------------------------------------------------------|
| `MethodUICreateArea` | `"ui_create_area"` | Register a UI scene-graph area (requires `ui`)         |
| `MethodUIPatch`    | `"ui_patch"`       | Apply a batch of scene-graph patch ops (requires `ui`) |
| `MethodUIRemoveArea` | `"ui_remove_area"` | Remove a UI area and its scene graph (requires `ui`)   |
| `MethodUIUpdateArea` | `"ui_update_area"` | Update constraints/weight of an existing area (requires `ui`) |

### Observability / Status methods

| Constant               | Wire value           | Purpose                                                      |
|------------------------|----------------------|--------------------------------------------------------------|
| `MethodShowPicker`     | `"show_picker"`      | Open an interactive TUI list picker                          |
| `MethodGetStatusInfo`  | `"get_status_info"`  | Get current status bar state (no permission required)       |
| `MethodGetContextUsage`| `"get_context_usage"`| Get current context window usage (no permission required)   |
| `MethodSetStatusLine`  | `"set_status_line"`  | Replace entire status bar text (no permission required)     |

**Invariant:** Method strings must not change between ABI versions without a version bump.

---

## UI Scene Graph

These types describe a declarative, node-based view of an area of the TUI. They
cross the host/WASM boundary as JSON. The `ui_create_area` / `ui_patch` /
`ui_remove_area` host_call methods (gated behind `PermUI`) operate on them; the
harness `SceneRenderer` applies and renders them.

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

An area is a named screen region owned by exactly one extension. `UIAreaPlacement` (`"main"`, `"sidebar"`, `"status"`, `"overlay"`, `"input"`) is an advisory layout hint; the harness owns final layout. `Weight` is a relative size hint among areas sharing a placement (`0` = harness default). Area `ID` is unique across all areas; the host rejects a create for an existing ID.

`UIArea` carries optional sizing constraints:

| Field        | Wire key      | Values                                    | Meaning |
|--------------|---------------|-------------------------------------------|---------|
| `MinHeight`  | `min_height`  | `"3"` or `"20%"`                          | Minimum rendered lines; pad with blank lines if below |
| `MaxHeight`  | `max_height`  | `"10"` or `"80%"`                         | Maximum rendered lines; truncate if above |
| `MinWidth`   | `min_width`   | `"80"` or `"50%"`                         | Minimum render width passed to `Render` |
| `MaxWidth`   | `max_width`   | `"120"` or `"100%"`                       | Maximum render width passed to `Render` |

All constraint fields are optional (`omitempty`); empty string means unconstrained. Percentage values are resolved against the current terminal dimension at render time.

### UIUpdateAreaParams

Params blob for `ui_update_area`. All constraint fields optional — omitted fields leave current values unchanged. `Weight` is `*int`; nil means leave unchanged.

```json
{ "id": "statusline", "max_height": "3", "weight": 2 }
```

Returns an error if the area ID does not exist.

**Invariant:** Area `ID` is unique across all areas; the host rejects a create for an existing ID. The `UIPatchOp.Index` `*int` distinction (nil = append, `0` = first position) is significant and must survive the wire.

---

## Event Payloads (Detailed)

### TokenPayload (`EventToken`)

| Field    | Type   | Description                         |
|----------|--------|-------------------------------------|
| `agent`  | string | Agent that produced the token       |
| `text`   | string | Chunk of streamed assistant text    |

Batches are coalesced (~75ms) to keep the WASM boundary crossing rate bounded regardless of provider speed.

### NotifyPayload (`EventNotify`)

| Field | Type   | Description                            |
|-------|--------|----------------------------------------|
| `text`| string | Notification text shown in the chat    |

Dispatched for every notification line regardless of origin. Handlers must not call `notify` to avoid a dispatch loop.

### LogBatchPayload (`EventLog`)

| Field    | Type        | Description                                      |
|----------|-------------|--------------------------------------------------|
| `records`| []LogRecord | Structured log records emitted by the host's slog handler |

Batches are coalesced (~30ms) and dispatched with reentrancy protection.

### ModelChangedPayload (`EventModelChanged`)

| Field      | Type   | Description                         |
|------------|--------|-------------------------------------|
| `provider` | string | Active provider identifier          |
| `model`    | string | Active model identifier, if known   |

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
3. `Tool.InputSchema` and `Tool.OutputSchema` are stored as `json.RawMessage`; the sdk never unmarshals them. `InputSchema` is forwarded to the LLM provider; `OutputSchema` documents the returned tool result for UI/docs.
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
16. All 55 `Method*` constants are documented above; missing methods in SPECS.md indicate incomplete documentation rather than missing implementation.