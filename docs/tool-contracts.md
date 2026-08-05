# Tool Contracts

This document is the discoverable contract reference for wllr native tools and
bundled extension tools with static registrations. Tool schemas are still sent
to providers as JSON Schema; this reference records the human-facing input and
output expectations, including whether failures are fatal tool errors or
non-fatal structured results.

Unless a tool says otherwise:

- Inputs are JSON objects matching the registered input schema.
- Required fields omitted from input return `is_error: true` with a plain-text
  validation message.
- Successful structured outputs are JSON encoded as the tool result string.
- Plain-text outputs are returned as the tool result string.
- Host or implementation failures return `is_error: true`. Some inspection
  tools return `ok: false` JSON for non-fatal domain failures so the agent can
  continue.

Dynamic MCP tools are documented by the MCP server that exposes them. wllr
passes their `inputSchema` through at registration time and formats MCP content
items into a text tool result.

## Native Tools

### `read_file`

Input:

- `path` string, required. Absolute or relative file path.

Output:

- Plain text containing the file contents.
- Fatal errors: missing `path`, read failure. Error text is plain text.

### `write_file`

Input:

- `path` string, required. File path to write.
- `content` string, required. Full file content to write.

Output:

- Plain text: `written <n> bytes to <path>`.
- Fatal errors: missing `path`, parent-directory creation failure, write
  failure. Error text is plain text.

### `edit_file`

**Preferred tool for file modifications.** Use `edit_file` instead of `sed`,
`python`, or other external tools when making text replacements. It provides
safe, atomic editing with exact-match validation.

Input:

- `path` string, required. File path to edit.
- `edits` array, required. Array of `{oldText, newText}` objects:
  - `oldText` string, required. Exact text to find.
  - `newText` string, required. Replacement text.

Output:

- JSON object with:
  - `success` boolean: `true` if all edits were applied.
  - `message` string: Human-readable result message.
  - `edits` array (optional): Indices of successfully applied edits.
  - `errors` array (optional): Error details for failed edits.

Behavior:

- Every `oldText` must match **exactly once** in the file.
- If any `oldText` is not found, or found more than once, the edit fails
  without modifying the file.
- Overlapping edits are rejected.
- All edits are validated before any changes are applied (atomic behavior).
- Returns structured errors identifying which edit failed and why.

Example:

```json
{
  "path": "modules/harness/model.go",
  "edits": [
    {
      "oldText": "old function name",
      "newText": "new function name"
    },
    {
      "oldText": "old value",
      "newText": "new value"
    }
  ]
}
```

### `exec`

Input:

- `command` string, required. Shell command executed with `sh -c`.
- `dir` string, optional. Working directory; defaults to the current process
  directory.
- `timeout_ms` integer, optional. Timeout in milliseconds; defaults to `30000`.

Output:

- Plain text containing combined stdout and stderr.
- Fatal errors: missing `command`, start failure, non-zero exit, cancellation, or
  timeout. If the command produced output before failing, the output is returned
  followed by `error: <message>`.

### `get_env`

Input:

- `name` string, optional. Environment variable name. If omitted, returns all
  environment entries.

Output:

- With `name`: plain text containing that variable's value, or an empty string
  when unset.
- Without `name`: JSON array of `KEY=value` strings.
- Input JSON parse errors are ignored; malformed input behaves like `{}`.

### `get_agent_status`

Input:

- `agent_id` string, required. Agent ID to inspect.
- `history_limit` integer, optional. Number of recent messages to include;
  defaults to `10`.

Output:

- JSON object with `agent_id`, `is_running`, `pending_messages`,
  `working`, `liveness`, `last_activity_age_ms`, `turn_duration_ms`,
  `last_tool_age_ms`, `last_tool_done_age_ms`, `active_tool`, `last_tool`,
  `shutdown_requested`, `turn_count`, `last_summary`, and `recent`.
- `working=true` means the child is executing its current turn unless
  `liveness` is `dead`; orchestrators should wait for a child notification
  rather than polling or sending progress probes.
- Age and duration fields are milliseconds. A zero age means the event has not
  happened yet or no timestamp is available.
- `recent` is an array of `{ "role": string, "preview": string }`.
- Fatal errors: missing `agent_id`, unknown agent. Error text is plain text.

## Agents Extension

### `create_agent`

Input: `name`, `system_prompt`, and `prompt` strings are required. Optional
fields are `model` string and `thinking_budget` integer.

Output: JSON object `{ "agent_id": string, "status": "created" }`. Fatal
errors include missing `name` or host spawn failure.

### `shutdown_agent`

Input: `agent_id` string, required.

Output: JSON object with `status: "shutdown_requested"`, `agent_id`, and
`stopped: false`. When the agent can still be found after the request, the
object also includes `is_running`, `pending_messages`, `last_activity_age_ms`,
and `shutdown_requested`. Fatal errors include missing `agent_id` or host
delivery failure.

### `list_agents`

Input: no fields.

Output: JSON object from the host agent pool, usually `{ "agents": [...] }`.
Each agent entry includes `id`, `name`, `is_running`, `working`, `liveness`,
`pending_messages`, `last_activity_age_ms`, `turn_duration_ms`,
`last_tool_age_ms`, `last_tool_done_age_ms`, `active_tool`, `last_tool`, and
`shutdown_requested`. If the host returns no data, returns `{ "agents": [] }`.

### `create_team`

Input: `name` string, required.

Output: JSON object `{ "team_id": "team-<name>", "status": "created" }`. Fatal
errors include missing `name` or host team creation failure.

### `add_to_team`

Input: `team_id` and `agent_id` strings, required.

Output: JSON object `{ "status": "added" }`. Fatal errors include missing fields or host membership failure.

### `get_team`

Input: `team_id` string, required.

Output: JSON object returned by the host for that team. Fatal errors include missing `team_id` or host lookup failure.

### `shutdown_team`

Input: `team_id` string, required.

Output: JSON object `{ "status": "closed" }`. Fatal errors include missing `team_id` or host shutdown failure.

### `send_message`

Input: `agent_id` and `message` strings, required.

Output: JSON object `{ "status": "sent" }`. Fatal errors include missing fields or host delivery failure.

## LSP Extension

These tools are the primary path for agentic coding workflows that need
diagnostics, linting, code navigation, reference discovery, or refactor
reconnaissance. Use `lsp_capabilities` near the start of repo/code work unless
the session already established the available backends and output contracts.
Use the navigation/reference tools before broad shell search or large file
sweeps when the task is about code structure.

### `lsp_capabilities`

Input: no fields.

Output: JSON object with `tools`, `backends`, and `note`.

### `lsp_diagnostics`

Input: `file` string, required.

Output: JSON object with `kind`, `target`, `language`, `command`, `ok`, `output`,
and optional `error`. Missing `file` is fatal. Unsupported language or command
failure returns JSON with `ok: false`.

Use after editing a supported source file before raw shell validation, unless the
user explicitly asked for the shell command.

### `lsp_lint`

Input: optional `path` string or `file` string.

Output: JSON object with `kind`, `target`, `language`, `command`, `ok`, `output`,
and optional `error`. Unavailable validation backend or command failure returns
JSON with `ok: false`.

Use for broad project/file validation before generic shell test commands when a
backend is available.

### `lsp_symbols`

Input: `file` string, required.

Output: JSON object with `kind`, `target`, `pattern`, `ok`, `matches`, and
optional `error`. Missing `file` is fatal.

Use to inspect file structure before large `read_file` sweeps.

### `lsp_definition`

Input: `symbol` string, required; `path` string optional and defaults to `.`.

Output: JSON object with `kind`, `target`, `pattern`, `ok`, `matches`, and
optional `error`. Missing `symbol` is fatal.

Use before `grep`, `rg`, or `find` when locating likely definition sites.

### `lsp_references`

Input: `symbol` string, required; `path` string optional and defaults to `.`.

Output: JSON object with `kind`, `target`, `pattern`, `ok`, `matches`, and
optional `error`. Missing `symbol` is fatal.

Use before `grep`, `rg`, or `find` when locating references or call sites.

### `lsp_refactor_preview`

Input: `symbol` and `new_name` strings, required; `path` string optional and
defaults to `.`.

Output: JSON object with `kind`, `path`, `symbol`, `new_name`, `pattern`,
`matches`, `ok`, `note`, and optional `error`. Missing `symbol` or `new_name` is
fatal.

Use before renames or shared API edits. This tool previews matches only; apply
edits with normal file-editing tools afterward.

## Memory Extension

### `memory_install`

Input: no fields.

Output: JSON object `{ "installed": true, "version": string, "path": string }`.
OS detection or install failure is fatal and returns JSON with `error`.

## Skills Extension

### `list_skills`

Input: no fields.

Output: JSON array of skill metadata objects with `name`, `description`, and
`category` fields when present. JSON marshal failure is fatal.

### `get_skill`

Input: `name` string, required.

Output: plain text containing the skill body with frontmatter stripped. Missing
`name` or unknown skill is fatal.

## Plan Extension

The built-in plan extension stores state in its private extension store, so
plans survive process restarts without filesystem permissions.

### `plan_create`

Input: `{title, description?, content?, steps?: [{id?, title, description?, acceptance_checks?}]}`.

Output: `{id, title, status, active, message}`.

### `plan_get`

Input: `{id}`.

Output: A complete plan object including ordered steps, statuses, notes, and evidence.

### `plan_list`

Input: `{status?: "active"|"paused"|"completed"|"archived"}`.

Output: `{plans, count}`.

### `plan_update`

Input: `{id, title?, description?, status?, content?}`. Supplied empty strings
clear optional text fields; title cannot be empty.

Output: `{id, title, status, message}`.

### `plan_step_update`

Input: `{plan_id, step_id, status?, description?, notes?}`.

Output: The updated step.

### `plan_evidence`

Input: `{plan_id, step_id, evidence}`.

Output: The updated step with the appended evidence entry.

### `plan_checkpoint`

Input: `{plan_id?}`. When omitted, checkpoints the active plan.

Output: `{plan_id, active_id, checkpointed}`.

### `plan_complete`

Input: `{plan_id}`.

Output: `{completed, plan_id, status}` when every step is complete; otherwise
an error result containing `incomplete_steps`.

### `plan_focus`

Input: `{}`.

Output: The active plan and its first incomplete `next_step`, or an inactive
result when no plan is selected.

### `plan_set_active`

Input: `{id}`.

Output: `{id, active}`.

## Tasks Extension

Task objects contain `id`, `title`, `description`, `status`, `priority`, `tags`,
`dependencies`, `created_at`, `updated_at`, and optional `assignee`.

### `tasklist_create`

Input: `name` string required; `description` and `owner_agent_id` strings
optional.

Output: JSON object `{ "list_id": string }`. Missing `name` is fatal.

### `tasks_create`

Input: `list_id` and `title` strings required. Optional fields are
`description`, `priority`, `tags`, and `dependencies`. `priority` defaults to
`medium`.

Output: JSON object `{ "task_id": string }`. Missing fields or unknown task list
is fatal.

### `tasks_update`

Input: `list_id` and `task_id` strings required. Optional replacement fields are
`title`, `description`, `status`, `priority`, `tags`, and `dependencies`.

Output: JSON object `{ "success": true }`. Missing fields, unknown task list, or
unknown task is fatal.

### `tasks_list`

Input: `list_id` string required; `status` optional filter.

Output: JSON object `{ "tasks": [Task, ...] }`. Missing `list_id` or unknown
task list is fatal.

### `tasks_get`

Input: `list_id` and `task_id` strings, required.

Output: JSON Task object. Missing fields, unknown task list, or unknown task is
fatal.

### `tasks_claim`

Input: `list_id` and `agent_id` strings, required.

Output: JSON object `{ "task": Task }` for the lowest pending,
dependency-satisfied task after setting status to `in_progress` and recording
`assignee`. Returns `{ "task": null }` when no task is available; that is not a
fatal error. Missing fields or unknown task list is fatal.

## Sigil Extension

### `sigil_start_generation`

Input: `conversation_id`, `model_provider`, and `model_name` strings are
required. Optional fields are `agent_name`, `agent_version`, and
`system_prompt`.

Output: JSON object `{ "generation_id": string }`. When Sigil is disabled,
returns `{ "status": "disabled", "reason": string }` as a non-fatal result.

### `sigil_set_result`

Input: `generation_id` and `output` strings, required. `output` is the generated
response text.

Output: JSON object `{ "status": "set" }`. Unknown generations and invalid
input are fatal tool errors.

### `sigil_end_generation`

Input: `generation_id` string, required. Optional `output` contains the final
response text.

Output: JSON object `{ "status": "ended" }`. The Sigil SDK performs batching,
retry, and export asynchronously. Unknown generations are fatal tool errors.

### `sigil_start_tool_execution`

Input: `tool_name` string, required; `conversation_id` string optional.

Output: JSON object `{ "tool_id": string }`. A missing active generation is a
fatal tool error.

### `sigil_end_tool_execution`

Input: `tool_id` string, required. Optional `arguments` and `result` JSON
values, plus an `error` string.

Output: JSON object `{ "status": "ended" }`. Unknown tool IDs are fatal tool
errors.

## Bundled Extensions Without Static LLM Tools

The `context`, `history`, `logging`, `statusline`, `permissions`,
`mcp-bridge`, and `otel-traces` bundled extensions do not statically register
LLM-callable tools in this repository. They may register slash commands,
subscribe to lifecycle events, update UI, or expose dynamic MCP server tools.
Dynamic MCP tool input and output contracts come from each MCP server's
`tools/list` response and server documentation.
