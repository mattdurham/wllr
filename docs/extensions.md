# Extensions — WASM Extension Author API Reference

Bob extensions are WebAssembly modules loaded at startup. They receive lifecycle
events from the harness, can register tools and slash commands, and communicate
with the host via a synchronous JSON-RPC mechanism (`host_call`).

---

## Overview

- Extensions are `.wasm` files placed in `BOB_EXTENSIONS_DIR`.
- The host loads them with [wazero](https://github.com/tetratelabs/wazero) — a
  pure-Go, zero-dependency WebAssembly runtime.
- Extensions must be compiled to the **WASI** target. The reference toolchain is
  [TinyGo](https://tinygo.org/) (`tinygo build -target wasi`).
- All communication between host and extension happens through shared linear
  memory and four host import functions (see below).
- Extensions are isolated: each has its own WASM linear memory and its own
  in-process key-value store.

---

## Required Exports

Every `.wasm` extension **must** export exactly these four functions. The host
validates their presence at load time and refuses to load any module that is
missing one.

### `_init() int32`

Called once immediately after the module is instantiated, before any events are
dispatched.

- Use `_init` to subscribe to events (`host_call subscribe`), register tools,
  and register slash commands.
- Return `0` on success. Any non-zero return code is treated as a fatal
  initialisation error; the extension is unloaded.

### `_on_event(ptr int32, len int32) int32`

Called by the host for each event the extension has subscribed to.

- `ptr` — pointer into the extension's linear memory where the JSON-encoded
  `sdk.Event` has been written.
- `len` — byte length of the JSON payload.
- Return `0` if the extension has no response (the common case).
- Return a non-zero pointer to a JSON-encoded `EventResponse` (also in the
  extension's linear memory) to cancel or block the event, or to signal an
  error. The host calls `_free` on this pointer after reading the response.

### `_alloc(size int32) int32`

Allocate `size` bytes in WASM linear memory and return the pointer.

- The host calls `_alloc` to obtain memory before writing an event payload or
  a `host_call` response into the extension's address space.
- Must return a valid pointer, or `0` to signal out-of-memory (the host will
  treat the allocation as failed).
- TinyGo example: `return int32(uintptr(unsafe.Pointer(&make([]byte, size)[0])))`

### `_free(ptr int32)`

Free a previously allocated pointer.

- Called by the host after it has finished reading a pointer that was either
  passed to `_on_event` or returned from `_on_event`.
- In TinyGo with GC enabled this can be a no-op — the GC reclaims memory.

---

## Host Imports (module `"env"`)

Extensions declare these as `//go:wasmimport env <name>` in TinyGo (or via
their toolchain's equivalent mechanism). All four are provided by the host
module named `"env"`.

### `host_log(level uint32, ptr uint32, length uint32)`

Write a log message.

| Level | Meaning |
|-------|---------|
| 0     | debug   |
| 1     | info    |
| 2     | warn    |
| 3     | error   |

`ptr` + `length` describe a UTF-8 string in the extension's linear memory.

### `host_alloc(size uint32) uint32`

Reserved for future use. In v1 this always returns `0`. Extensions should not
call it.

### `host_free(ptr uint32)`

No-op in v1. Reserved for future use.

### `host_call(req_ptr uint32, req_len uint32, resp_ptr_ptr uint32, resp_len_ptr uint32) uint32`

Synchronous JSON-RPC call from extension to host.

- `req_ptr` / `req_len` — pointer and byte length of a JSON-encoded
  `HostCallRequest` in the extension's linear memory.
- `resp_ptr_ptr` — pointer to a `uint32` slot where the host writes the pointer
  to the response bytes (allocated via `_alloc` in the extension).
- `resp_len_ptr` — pointer to a `uint32` slot where the host writes the byte
  length of the response.
- Returns an error code (see Error Codes below).

Pass `0` for both `resp_ptr_ptr` and `resp_len_ptr` if no response is needed.

---

## host_call Method Reference

`HostCallRequest` JSON envelope:

```json
{"method": "<method_name>", "params": { ... }}
```

`HostCallResponse` JSON envelope (written into extension memory via `_alloc`):

```json
{"result": { ... }, "error": "<error string if any>"}
```

An empty `"error"` field (or its absence) means success.

---

### `subscribe`

Register interest in a lifecycle event. Must be called from `_init`.

```json
{"method": "subscribe", "params": {"event": "session_start"}}
```

| Field   | Type   | Description                          |
|---------|--------|--------------------------------------|
| `event` | string | One of the event type strings below. |

No response result.

---

### `register_tool`

Register a tool that the LLM may call. The host forwards tool calls back to the
extension as `on_tool_call` events.

```json
{
  "method": "register_tool",
  "params": {
    "name": "my_tool",
    "description": "Human-readable description for the LLM.",
    "input_schema": {
      "type": "object",
      "properties": {
        "query": {"type": "string", "description": "The search query"}
      },
      "required": ["query"]
    },
    "output_schema": {
      "type": "object",
      "properties": {
        "answer": {"type": "string", "description": "Tool result text"}
      }
    }
  }
}
```

| Field           | Type   | Description                                                  |
|-----------------|--------|--------------------------------------------------------------|
| `name`          | string | Unique tool name. Duplicate registration fails.              |
| `description`   | string | Shown to the LLM to explain tool purpose.                    |
| `input_schema`  | object | JSON Schema object for the tool's input.                     |
| `output_schema` | object | JSON Schema object documenting the returned tool result text. |

No response result. Returns an error if the tool name is already registered.

---

## Built-in LLM tools

The LLM-visible tool surface is the set of native tools registered by the host
plus bundled or installed extensions. Tool inputs are JSON objects matching the
registered `input_schema`. Tool outputs are returned to the model as text; many
tools encode structured output as a JSON string. On tool failure, the output is
the error text and the result is marked as an error.

MCP tools are dynamic: `mcp-bridge` registers whatever each configured MCP server
advertises via `tools/list`, so their inputs and outputs are defined by that MCP
server's schema rather than by this document.

### Native tools

| Tool | Inputs | Output |
|------|--------|--------|
| `read_file` | `path` string, required: absolute or relative file path. | File contents as text. |
| `write_file` | `path` string, required; `content` string, required. | Text: `written <n> bytes to <path>`. |
| `exec` | `command` string, required; `dir` string, optional working directory; `timeout_ms` integer, optional, default 30000. | Combined stdout/stderr as text. Errors return text such as `exec cancelled`, `exec timed out after <duration>`, or command output followed by `error: <message>`. |
| `get_env` | `name` string, optional. | If `name` is set, the variable value as text. If omitted, a JSON array of `"KEY=VALUE"` strings. |
| `get_agent_status` | `agent_id` string, required; `history_limit` integer, optional, default 10. | JSON object with `agent_id`, `is_running`, `working`, `liveness`, `pending_messages`, liveness fields (`last_activity_age_ms`, `turn_duration_ms`, `last_tool_age_ms`, `last_tool_done_age_ms`, `active_tool`, `last_tool`, `shutdown_requested`), `turn_count`, `last_summary`, and `recent` message previews. |

### Agent and team tools

Registered by the bundled `agents` extension.

| Tool | Inputs | Output |
|------|--------|--------|
| `create_agent` | `name` string, required; `system_prompt` string, required; `prompt` string, required; `model` string, optional; `thinking_budget` integer, optional. | JSON result from host agent spawn/delivery. Includes the new agent ID on success. |
| `shutdown_agent` | `agent_id` string, required. | JSON object with `status: "shutdown_requested"`, `agent_id`, `stopped: false`, and, when available, current running, queue, activity, and shutdown-request state. |
| `list_agents` | No fields. | JSON object containing live agents with IDs, names, running state, pending message counts, recent activity age, turn duration, last/active tool names, and shutdown-request state. |
| `send_message` | `agent_id` string, required; `message` string, required. | JSON result from host agent delivery. |
| `create_team` | `name` string, required. | JSON result from host team creation, including team ID on success. |
| `add_to_team` | `team_id` string, required; `agent_id` string, required. | JSON result from host team membership update. |
| `get_team` | `team_id` string, required. | JSON object describing the requested team. |
| `shutdown_team` | `team_id` string, required. | JSON result from host team shutdown. |

### Task tools

Registered by the installed `tasks` extension.

Task fields use these string enums:

- `status`: `pending`, `in_progress`, `completed`, `blocked`
- `priority`: `low`, `medium`, `high`, `critical`

| Tool | Inputs | Output |
|------|--------|--------|
| `tasklist_create` | `name` string, required; `description` string, optional; `owner_agent_id` string, optional. | JSON object: `{"list_id":"..."}`. |
| `tasks_create` | `list_id` string, required; `title` string, required; `description` string, optional; `priority` string, optional; `tags` string array, optional; `dependencies` string array, optional. | JSON object: `{"task_id":"..."}`. |
| `tasks_update` | `list_id` string, required; `task_id` string, required; optional updates: `title`, `description`, `status`, `priority`, `tags`, `dependencies`. | JSON object: `{"success":true}`. |
| `tasks_list` | `list_id` string, required; `status` string, optional filter. | JSON object: `{"tasks":[...]}` where each task includes `id`, `title`, `description`, `status`, `priority`, `tags`, `dependencies`, and assignment fields when present. |
| `tasks_get` | `list_id` string, required; `task_id` string, required. | JSON task object. |
| `tasks_claim` | `list_id` string, required; `agent_id` string, required. | JSON object: `{"task":{...}}` for a claimed task, or `{"task":null}` if none are available. |

### Skill tools

Registered by the installed `skills` extension.

| Tool | Inputs | Output |
|------|--------|--------|
| `list_skills` | No fields. | JSON array of skill metadata objects with `name`, `description`, and `category` when set. |
| `get_skill` | `name` string, required. | Skill body text with frontmatter stripped. |

### Memory tool

Registered by the installed `memory` extension.

| Tool | Inputs | Output |
|------|--------|--------|
| `memory_install` | No fields. | JSON object on success: `{"installed":true,"version":"...","path":"..."}`. On failure, JSON object: `{"error":"..."}` marked as an error result. |

### LSP tools

Registered by the installed `lsp` extension. These are best-effort code
intelligence tools. Apply edits separately with normal file tools after reviewing
the returned locations.

| Tool | Inputs | Output |
|------|--------|--------|
| `lsp_capabilities` | No fields. | JSON object with `tools`, `backends`, and a `note`. |
| `lsp_diagnostics` | `file` string, required. | JSON diagnostic object with `kind`, `target`, `language`, `command`, `ok`, `output`, and optional `error`. |
| `lsp_lint` | `path` string, optional; `file` string, optional. Defaults to `.`. | JSON lint/diagnostic object. For Go projects it runs `go test ./...`. |
| `lsp_symbols` | `file` string, required. | JSON search object with `kind`, `target`, `pattern`, `ok`, `matches`, and optional `error`. |
| `lsp_definition` | `symbol` string, required; `path` string, optional, default `.`. | JSON search object containing likely definition matches. |
| `lsp_references` | `symbol` string, required; `path` string, optional, default `.`. | JSON search object containing likely reference matches. |
| `lsp_refactor_preview` | `symbol` string, required; `new_name` string, required; `path` string, optional, default `.`. | JSON object with `kind`, `path`, `symbol`, `new_name`, `pattern`, `matches`, `ok`, `note`, and optional `error`. |

---

### `register_command`

Register a slash command visible in the TUI.

```json
{"method": "register_command", "params": {"name": "greet", "description": "Say hello"}}
```

| Field         | Type   | Description              |
|---------------|--------|--------------------------|
| `name`        | string | Command name (no slash). |
| `description` | string | Shown in `/help` output. |

No response result.

---

### `send_message`

Inject a message into the conversation as if typed by the user or generated by
the assistant. The harness will trigger a new provider request.

```json
{"method": "send_message", "params": {"role": "user", "content": "Hello!"}}
```

| Field     | Type   | Description                  |
|-----------|--------|------------------------------|
| `role`    | string | `"user"` or `"assistant"`.   |
| `content` | string | The message text.            |

No response result.

---

### `set_status`

Set a keyed value readable via `get_status_info`. The bundled `statusline`
extension reads these values and may surface them in the status scene area.

> **Deprecated.** The statusline is now fully scene-driven. Prefer patching
> the `"statusline"` area directly with `ui_patch` for full layout and styling
> control. `set_status` is kept for backward compatibility.

```json
{"method": "set_status", "params": {"key": "my_ext", "value": "active"}}
```

| Field   | Type   | Description                      |
|---------|--------|----------------------------------|
| `key`   | string | Identifier for the status entry. |
| `value` | string | Display value.                   |

No response result.

---

### `notify`

Append a notification line to the chat view (rendered as a system message).

```json
{"method": "notify", "params": {"text": "Background task complete."}}
```

| Field  | Type   | Description       |
|--------|--------|-------------------|
| `text` | string | Notification text.|

No response result.

---

### `tool_result`

Return the result of a tool call to the harness. Call this after processing an
`on_tool_call` event.

```json
{
  "method": "tool_result",
  "params": {
    "tool_call_id": "toolu_abc123",
    "result": "Paris",
    "is_error": false
  }
}
```

| Field          | Type    | Description                                         |
|----------------|---------|-----------------------------------------------------|
| `tool_call_id` | string  | The `tool_call_id` from the `on_tool_call` payload. |
| `result`       | string  | The tool output (text).                             |
| `is_error`     | boolean | `true` if the tool encountered an error.            |

No response result.

---

### `get_os`

Returns the host operating system and CPU architecture. Values match Go's
`runtime.GOOS` and `runtime.GOARCH` (e.g. `"darwin"`, `"arm64"`).

No params required.

```json
{"method": "get_os", "params": {}}
```

Response result:

```json
{"os": "darwin", "arch": "arm64"}
```

| Field  | Type   | Description                                    |
|--------|--------|------------------------------------------------|
| `os`   | string | Host OS (`darwin`, `linux`, `windows`, …).     |
| `arch` | string | Host CPU architecture (`amd64`, `arm64`, …).   |

No permission required.

---

### `store_set`

Persist a string value in the extension's private key-value store. The store
survives for the lifetime of the process.

```json
{"method": "store_set", "params": {"key": "last_query", "value": "Paris"}}
```

| Field   | Type   | Description |
|---------|--------|-------------|
| `key`   | string | Store key.  |
| `value` | string | Store value.|

No response result.

---

### `store_get`

Retrieve a value from the extension's private key-value store.

```json
{"method": "store_get", "params": {"key": "last_query"}}
```

| Field | Type   | Description |
|-------|--------|-------------|
| `key` | string | Store key.  |

Response result on success:

```json
{"value": "Paris"}
```

Returns an error (`"not found"`) if the key does not exist.

---

### `abort`

Cancel the in-progress provider stream immediately. Equivalent to the user
pressing Ctrl+C.

```json
{"method": "abort", "params": {}}
```

No response result.

---

### `exec`

Execute a shell command on the host filesystem. The combined stdout and stderr
are returned. Requires the `exec` permission.

```json
{"method": "exec", "params": {"command": "ls -la", "dir": "/tmp"}}
```

| Field     | Type   | Description                                             |
|-----------|--------|---------------------------------------------------------|
| `command` | string | Shell command passed to `sh -c`.                        |
| `dir`     | string | Working directory. Defaults to current dir if omitted.  |

Response result:

```json
{"output": "file1
file2
", "error": ""}
```

| Field    | Type   | Description                                       |
|----------|--------|---------------------------------------------------|
| `output` | string | Combined stdout+stderr of the command.            |
| `error`  | string | Non-empty if the command exited with a non-zero status. |

Requires permission: `exec`

---

### `read_file`

Read the contents of a file on the host filesystem.

```json
{"method": "read_file", "params": {"path": "/etc/hosts"}}
```

| Field  | Type   | Description                              |
|--------|--------|------------------------------------------|
| `path` | string | Absolute or relative path to the file.  |

Response result:

```json
{"content": "127.0.0.1 localhost
..."}
```

| Field     | Type   | Description            |
|-----------|--------|------------------------|
| `content` | string | File contents as UTF-8.|

Requires permission: `file_read`

---

### `write_file`

Write content to a file on the host filesystem. Parent directories are created
automatically.

```json
{"method": "write_file", "params": {"path": "/tmp/out.txt", "content": "hello"}}
```

| Field     | Type   | Description                        |
|-----------|--------|------------------------------------|
| `path`    | string | Absolute or relative path to write.|
| `content` | string | Content to write (UTF-8).          |

Response result:

```json
{"written": "/tmp/out.txt"}
```

Requires permission: `file_write`

---

### `append_file`

Append content to a file on the host filesystem, creating it (and parent
directories) if absent. Unlike `write_file`, existing content is preserved — use
this for log-style accumulation.

```json
{"method": "append_file", "params": {"path": "/tmp/app.log", "content": "line\n"}}
```

| Field     | Type   | Description                         |
|-----------|--------|-------------------------------------|
| `path`    | string | Absolute or relative path to append.|
| `content` | string | Content to append (UTF-8).          |

Response result:

```json
{"appended": "/tmp/app.log"}
```

Requires permission: `file_write`

---

### `get_env`

Read an environment variable from the host process. Pass an empty name to get
all variables as a JSON array of `"KEY=VALUE"` strings.

```json
{"method": "get_env", "params": {"name": "HOME"}}
```

| Field  | Type   | Description                                          |
|--------|--------|------------------------------------------------------|
| `name` | string | Variable name. Omit or pass `""` to list all vars. |

Response result:

```json
{"value": "/home/user"}
```

No permission required (read-only).

---

### `ui_create_area`

Register a named UI **area** — a region of the screen the extension owns. An
extension may own one area, inject into an existing area's scene graph, or spawn
additional areas. Requires the `ui` permission.

```json
{"method": "ui_create_area", "params": {"area": {
  "id": "my-panel",
  "placement": "status",
  "weight": 1,
  "min_height": "1",
  "max_height": "5",
  "min_width": "",
  "max_width": "100%"
}}}
```

| Field             | Type   | Description                                                                  |
|-------------------|--------|------------------------------------------------------------------------------|
| `area.id`         | string | Unique area ID. Errors if it already exists.                                 |
| `area.placement`  | string | Layout hint: `main`, `sidebar`, `status`, `overlay`, or `input` (read-only). |
| `area.weight`     | int    | Optional relative size hint among same-placement areas.                      |
| `area.min_height` | string | Minimum height: `"N"` (lines) or `"N%"` (% of terminal). `""` = none.       |
| `area.max_height` | string | Maximum height: `"N"` (lines) or `"N%"` (% of terminal). `""` = none.       |
| `area.min_width`  | string | Minimum width: `"N"` (cols) or `"N%"` (% of terminal). `""` = none.         |
| `area.max_width`  | string | Maximum width: `"N"` (cols) or `"N%"` (% of terminal). `""` = none.         |

Constraint values accept `"N"` (absolute) or `"N%"` (percentage of the terminal
dimension). Empty string (`""`) means unconstrained. The harness clamps the
rendered output after calling `Render`.

Requires permission: `ui`

---

### `ui_update_area`

Update the sizing constraints and/or weight of an **existing** area. All fields
are optional — omitted or empty fields leave current values unchanged. Returns
an error if the area ID does not exist. Requires the `ui` permission.

```json
{"method": "ui_update_area", "params": {
  "id": "my-panel",
  "max_height": "3"
}}
```

| Field        | Type   | Description                                                   |
|--------------|--------|---------------------------------------------------------------|
| `id`         | string | Area ID to update (required).                                 |
| `min_height` | string | New minimum height. `""` = leave unchanged.                   |
| `max_height` | string | New maximum height. `""` = leave unchanged.                   |
| `min_width`  | string | New minimum width. `""` = leave unchanged.                    |
| `max_width`  | string | New maximum width. `""` = leave unchanged.                    |
| `weight`     | int?   | New weight. Omit (null) to leave unchanged.                   |

Requires permission: `ui`

---

### `ui_patch`

Apply a batch of scene-graph mutations to an area. Ops apply in order and the
batch is all-or-nothing: if any op references a missing area or node, the whole
batch is rejected and the live tree is unchanged. Requires the `ui` permission.

```json
{"method": "ui_patch", "params": {"area": "chat", "ops": [
  {"op": "set_root", "node": {"id": "root", "type": "vstack", "children": [
    {"id": "line1", "type": "text", "text": "Hello"}
  ]}},
  {"op": "append_text", "id": "line1", "text": " world"}
]}}
```

Node shape (`UINode`): `{"id", "type", "text"?, "props"?, "children"?}` where
`type` is one of `text`, `vstack`, `hstack`, `viewport`, `spinner`, `divider`.
`props` (`UIProps`) carries optional style/layout: `width`/`height` (`"fill"`,
`"auto"`, or a cell count), `border` (`none`/`normal`/`rounded`/`thick`/`double`),
`padding`/`margin` (1, 2, or 4 cell counts), `align`, `fg`/`bg` (theme tokens such
as `accent`, `muted`, `error`), and `bold`/`italic`/`underline`/`faint`/`wrap`.

Op types (`op` field):

| `op`          | Fields                     | Effect                                            |
|---------------|----------------------------|---------------------------------------------------|
| `set_root`    | `node`                     | Replace the area's whole scene graph              |
| `insert`      | `parent`, `index`, `node`  | Insert `node` under `parent` at `index` (nil = append; `parent` "" = root) |
| `update`      | `id`, `props`              | Replace the props of node `id`                    |
| `remove`      | `id`                       | Remove node `id` and its subtree                  |
| `append_text` | `id`, `text`               | Append `text` to text node `id` (streaming op)    |

Requires permission: `ui`

---

### `ui_remove_area`

Remove a UI area and its scene graph. Removing a missing area is a no-op.
Requires the `ui` permission.

```json
{"method": "ui_remove_area", "params": {"area": "chat"}}
```

Requires permission: `ui`

---

## Lifecycle Events

Events are dispatched to subscribed extensions via `_on_event`. The `sdk.Event`
JSON envelope is:

```json
{"type": "<event_type>", "payload": { ... }}
```

---

### `session_start`

Fired once when the TUI initialises a new session.

```json
{"reason": "new_session"}
```

| Field    | Type   | Description                    |
|----------|--------|--------------------------------|
| `reason` | string | Always `"new_session"` in v1.  |

---

### `before_agent_start`

Fired when the user submits a message, before any provider request is made.

```json
{"prompt": "What is the capital of France?", "system_prompt": ""}
```

| Field           | Type   | Description                       |
|-----------------|--------|-----------------------------------|
| `prompt`        | string | The raw user input.               |
| `system_prompt` | string | System prompt (may be empty).     |

---

### `before_provider_request`

Fired immediately before the request is sent to the LLM.

```json
{
  "messages": [{"role": "user", "content": "What is the capital?"}],
  "model": "claude-sonnet-4-5"
}
```

| Field      | Type            | Description                   |
|------------|-----------------|-------------------------------|
| `messages` | array of Message| Full conversation history.    |
| `model`    | string          | Active model identifier.      |

---

### `after_provider_response`

Fired after the provider stream completes successfully.

```json
{"usage": {"input_tokens": 0, "output_tokens": 0}}
```

| Field                    | Type | Description                       |
|--------------------------|------|-----------------------------------|
| `usage.input_tokens`     | int  | Input token count (when provided).|
| `usage.output_tokens`    | int  | Output token count (when provided).|

---

### `on_tool_call`

Fired when the LLM requests a tool call. The extension that registered the tool
should process this event and respond with `tool_result`.

```json
{
  "tool_call_id": "toolu_abc123",
  "tool_name": "my_tool",
  "input": {"query": "Paris"}
}
```

| Field          | Type   | Description                                 |
|----------------|--------|---------------------------------------------|
| `tool_call_id` | string | Opaque ID; must be returned in `tool_result`.|
| `tool_name`    | string | Name of the tool being called.              |
| `input`        | object | Raw JSON input matching the tool's schema.  |

---

### `on_tool_result`

Fired after a tool result has been submitted back to the provider.

```json
{
  "tool_call_id": "toolu_abc123",
  "result": "Paris",
  "is_error": false
}
```

| Field          | Type    | Description                  |
|----------------|---------|------------------------------|
| `tool_call_id` | string  | The corresponding call ID.   |
| `result`       | string  | The tool output.             |
| `is_error`     | boolean | Whether the tool errored.    |

---

### `message_start`

Fired when a new message begins streaming from the provider.

```json
{"role": "assistant"}
```

| Field  | Type   | Description                    |
|--------|--------|--------------------------------|
| `role` | string | `"user"` or `"assistant"`.     |

---

### `message_end`

Fired when a message has finished streaming.

```json
{"role": "assistant", "content": "The capital of France is Paris."}
```

| Field     | Type   | Description                         |
|-----------|--------|-------------------------------------|
| `role`    | string | `"user"` or `"assistant"`.          |
| `content` | string | Full content of the completed message.|

---

### `token`

Fired with a batch of streamed assistant text as the agent produces it. Batches
are coalesced (~75ms) so the per-token boundary-crossing rate stays bounded. Use
this to drive a scene-graph area with live streaming text (see `ui_patch`).

```json
{"agent_id": "main", "text": "The capital of "}
```

| Field      | Type   | Description                                      |
|------------|--------|--------------------------------------------------|
| `agent_id` | string | ID of the agent that produced the text batch.    |
| `text`     | string | A batch of streamed assistant text.              |

The `OnToken(func(agentID, text string))` SDK helper subscribes to this event.

> **Driving the main transcript from WASM.** The bundled `agents` extension owns
> the main chat transcript: it creates the `chat` scene area (placement `main`)
> and builds the transcript from `before_agent_start` (user prompts), `token`
> (streamed assistant text), and `notify` (system lines). The harness renders
> that area's content inside its own scrollable viewport — scrolling and key
> input stay in the harness. There is no built-in fallback renderer: if no
> extension creates the `chat` area, the chat viewport is empty.

---

### `notify`

Fired whenever a system notification line is shown in the chat — from an
extension's `notify` call, a model change, an extension reload, or an extension
error. Lets an extension that owns the transcript render notifications into its
scene graph.

```json
{"text": "Extensions reloaded."}
```

| Field  | Type   | Description                                                    |
|--------|--------|----------------------------------------------------------------|
| `text` | string | The notification line. May begin with `"⚠"` for warnings/errors. |

The `OnNotify(func(text string))` SDK helper subscribes to this event.

> **Do not call `notify` from inside an `OnNotify` handler** — it would
> re-trigger this event and loop indefinitely.

---

### `model_changed`

Fired after the active provider/model status changes, including the initial
startup state after the TUI initializes.

```json
{"provider": "openai", "model": "gpt-5.5"}
```

| Field      | Type   | Description                         |
|------------|--------|-------------------------------------|
| `provider` | string | Active provider identifier.         |
| `model`    | string | Active model identifier, if known.  |

The `OnModelChanged(func(provider, model string))` SDK helper subscribes to this
event.

---

### `log`

Fired with a coalesced batch (~30ms) of structured log records emitted by the
host's `slog` handler. Lets an extension act as a **log sink** — write a file,
ship to a backend, filter, etc. The bundled `logging` extension uses this to
write `~/.wllr/logs/<timestamp>.log`.

```json
{"records": [
  {"time": "2026-06-30T12:00:00.123Z", "level": "info", "message": "stream done",
   "attrs": [{"key": "tokens", "value": "42"}]},
  {"time": "2026-06-30T12:00:01.4Z", "level": "error", "message": "boom"}
]}
```

| Field            | Type   | Description                                            |
|------------------|--------|--------------------------------------------------------|
| `records[].time`    | string | RFC3339Nano UTC timestamp.                          |
| `records[].level`   | string | `debug` / `info` / `warn` / `error`.                |
| `records[].message` | string | The log message.                                    |
| `records[].attrs`   | array  | Ordered `{key, value}` pairs (values pre-stringified). |

The `OnLog(func(records []LogRecord))` SDK helper subscribes to this event. Pair
it with `append_file` to write a log file.

> **Do not call `Log`/`Logf` from inside an `OnLog` handler.** The host suppresses
> logs emitted while dispatching `log` (reentrancy guard), so they would be
> silently dropped — and relying on it is wasteful.

---

### `shutdown`

Fired when the harness is shutting down. Use this for cleanup.

```json
{"reason": "user_quit"}
```

| Field    | Type   | Description             |
|----------|--------|-------------------------|
| `reason` | string | Reason for shutdown.    |

---

## EventResponse

`_on_event` may return `0` (no response) or a pointer to a JSON-encoded
`EventResponse` object.

```json
{"cancel": false, "block": false, "error": "", "payload": null}
```

| Field     | Type    | Semantics                                                          |
|-----------|---------|--------------------------------------------------------------------|
| `cancel`  | boolean | Cancel the current operation (e.g. abort a stream in progress).   |
| `block`   | boolean | Block the interaction; on a transform chain, stops it with `error` as the reason. |
| `error`   | string  | Non-empty string signals an error / block reason; displayed as a notification. |
| `payload` | object  | **Transformed event payload.** Same JSON shape as the incoming `Event.payload`. Empty = no transformation. |

All fields are optional and default to their zero values. Return `0` (null
pointer) from `_on_event` when no response is needed — this is the common case.

### Interceptors (transform chains)

Some interactions are dispatched as an **interceptor chain**: the host threads
the event payload through subscribed extensions in priority order (lower
`priority` first). Each extension may:

- **observe** — return `0` / empty response; the payload is unchanged.
- **transform** — return `{"payload": <modified payload>}`; the modified payload
  is threaded to the next interceptor and applied to the operation.
- **block** — return `{"block": true, "error": "<reason>"}`; the chain stops and
  the operation is refused with the reason.

The first block wins. A transformed payload that does not match the expected
shape at the seam is ignored (the prior payload is kept).

**`before_tool_call` is a transform chain.** An interceptor may rewrite a tool
call's `input` (e.g. a security layer sanitising a bash command) or block the
call. This applies to both WASM-implemented and native (built-in) tools. A
blocked call returns an error tool result and the tool never executes.

The SDK helper `OnInterceptToolCall(fn)` wraps this: `fn(agentID, toolName,
input)` returns `(newInput, block, reason)` — a non-nil `newInput` rewrites the
call, `block=true` refuses it with `reason`.

**`after_tool_call` is a transform chain.** An interceptor may rewrite or redact
a tool's `result` before the model sees it (e.g. strip secrets from command
output) or block it. This is the output-side partner of `before_tool_call` and
applies to both WASM and native tools. A block replaces the result with the
reason and marks it an error.

The SDK helper `OnInterceptToolResult(fn)` wraps this: `fn(agentID, toolName,
result, isError)` returns `(newResult, newIsError, block, reason)` — a non-empty
`newResult` rewrites the output, `block=true` redacts it with `reason`.

**`before_provider_request` is a transform chain.** An interceptor may redact
the messages about to be sent to the LLM (e.g. strip PII / API keys), reroute
the model (e.g. a cheap local model vs a frontier model), or block the request.
Redaction is **send-time only** — the agent's stored history keeps the original
content. A block fails the turn.

The SDK helper `OnInterceptProviderRequest(fn)` wraps this: `fn(messages,
model)` returns `(newMessages, newModel, block, reason)` — a non-nil
`newMessages` redacts/edits the outgoing messages, a non-empty `newModel`
reroutes, `block=true` refuses the request with `reason`.

---

## Memory Management Protocol

Understanding the ownership contract prevents use-after-free and memory leaks.

### Event dispatch (`_on_event`)

1. Host calls `_alloc(len)` to allocate space in the extension's memory.
2. Host writes the event JSON at the returned pointer.
3. Host calls `_on_event(ptr, len)`.
4. Host calls `_free(ptr)` to release the event buffer.
5. If `_on_event` returns a non-zero pointer, the host reads the response JSON
   from that pointer, then calls `_free(resp_ptr)`.

**Extension owns the response buffer** it returns from `_on_event`. The host
frees it after reading.

### host_call response

1. Extension allocates `req` buffer via `_alloc`, writes the request JSON.
2. Extension calls `host_call(req_ptr, req_len, &resp_ptr, &resp_len)`.
3. Host allocates a response buffer inside the extension via `_alloc(resp_len)`.
4. Host writes response JSON and stores the pointer in `*resp_ptr_ptr` and
   length in `*resp_len_ptr`.
5. `host_call` returns.
6. **Extension is responsible for calling `_free(resp_ptr)`** after reading the
   response. The host does not free it.
7. Extension calls `_free(req_ptr)` to release the request buffer.

---

## Error Codes

Returned by `host_call` as a `uint32`.

| Constant     | Value | Meaning                                                |
|--------------|-------|--------------------------------------------------------|
| `ErrOK`      | `0`   | Success.                                               |
| `ErrGeneral` | `1`   | General error (check `HostCallResponse.error` for msg).|
| `ErrCancel`  | `2`   | Operation cancelled.                                   |

---

## Build and Install

### Prerequisites

- [TinyGo](https://tinygo.org/) 0.30+ (for WASI target support and `//go:wasmimport`)
- Standard Go module setup

### Build

```bash
# From the extension directory
tinygo build -o hello.wasm -target wasi .
```

### Install

```bash
# Copy to the extensions directory
cp hello.wasm "$BOB_EXTENSIONS_DIR/"
```

Bob scans `BOB_EXTENSIONS_DIR` at startup and loads all `.wasm` files found
there. Use `/reload` in the TUI to hot-reload extensions without restarting.

---

## Annotated Example

The `extensions/example/` directory contains a fully working extension. It
demonstrates the minimal pattern: subscribe in `_init`, handle an event in
`_on_event`, and make `host_call` calls via a helper.

Key points from `extensions/example/main.go`:

- The `//go:build tinygo` build tag ensures the file is only compiled with
  TinyGo.
- Host imports are declared with `//go:wasmimport env <name>` directives.
- Required exports are annotated with `//export <name>` directives.
- `_alloc` uses `unsafe.Pointer` on a Go slice to hand memory to the host.
- `_free` is a no-op because TinyGo's GC handles reclamation.
- `hostCallJSON` is a convenience wrapper that marshals params, writes them into
  WASM memory, calls `host_call`, and frees buffers on return.
- The extension subscribes only to `session_start` and patches the
  `"statusline"` scene area to update the display when the session begins.
  See `extensions/statusline/main.go` for the reference implementation.
