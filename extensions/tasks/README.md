# Tasks Extension

Go-based WASM extension providing task management tools for wllr.

## Overview

The tasks extension adds task list and task management capabilities to wllr through 6 tools:

- `tasklist_create` - Create a new task list
- `tasks_create` - Create a task in a list
- `tasks_update` - Update task fields
- `tasks_list` - List all tasks in a list
- `tasks_get` - Get specific task details
- `tasks_claim` - Atomically claim the next available task (multi-worker safe)

## Installation

Built and installed automatically via:

```bash
make extensions
```

This compiles the extension to `~/.wllr/extensions/tasks/tasks.wasm` and copies the JSON manifest.

## Features

### Task Lists

- Sequential IDs (`list-1`, `list-2`, ...)
- Name and description
- Contains multiple tasks

### Tasks

- Sequential IDs per list (`task-1`, `task-2`, ...)
- Title and description
- Status: `pending`, `in_progress`, `completed`, `blocked`
- Priority: `low`, `medium`, `high`, `critical`
- Assignee (set by `tasks_claim`)
- Tags (array of strings)
- Dependencies (array of task IDs)
- Timestamps (created_at, updated_at)

### Atomic Claiming

`tasks_claim(list_id, agent_id)` finds the first pending, dependency-satisfied
task (lowest `task-N`), flips it to `in_progress`, and records `agent_id` as the
assignee — all under the list lock, so two workers racing to claim can never be
assigned the same task. Returns `{"task": null}` when nothing is available.
Prefer this over `tasks_list` + `tasks_update` for multi-worker coordination.

### Storage

- In-memory (persists for extension process lifetime)
- Thread-safe with mutexes
- Separate counters per list for task IDs

## Usage

All tools accept JSON object inputs and return JSON strings on success. Validation
or missing-state failures mark the tool call as failed with plain-text error
messages such as `tasks_get: task not found`. The central contract reference is
[`docs/tool-contracts.md`](../../docs/tool-contracts.md#tasks-extension).

### Create a task list

```json
{
  "name": "My Tasks",
  "description": "Tasks for project X",
  "owner_agent_id": "main"
}
```

Returns:

```json
{
  "list_id": "list-1"
}
```

### Create a task

```json
{
  "list_id": "list-1",
  "title": "Implement feature",
  "description": "Add new functionality",
  "priority": "high",
  "tags": ["feature", "backend"]
}
```

Returns:

```json
{
  "task_id": "task-1"
}
```

### Update a task

```json
{
  "list_id": "list-1",
  "task_id": "task-1",
  "status": "in_progress"
}
```

Returns:

```json
{
  "success": true
}
```

### List tasks

```json
{
  "list_id": "list-1",
  "status": "in_progress"  // optional filter
}
```

Returns:

```json
{
  "tasks": [
    {
      "id": "task-1",
      "title": "Implement feature",
      "status": "in_progress",
      ...
    }
  ]
}
```

### Get task details

```json
{
  "list_id": "list-1",
  "task_id": "task-1"
}
```

Returns the full task object.

### Claim a task atomically

```json
{
  "list_id": "list-1",
  "agent_id": "main/worker-1"
}
```

Returns:

```json
{
  "task": {
    "id": "task-1",
    "title": "Implement feature",
    "status": "in_progress",
    "assignee": "main/worker-1"
  }
}
```

If no pending dependency-satisfied task exists, returns:

```json
{
  "task": null
}
```

## Testing

Integration tests are located in `test/integration/tasks/` and run via:

```bash
make test
```

Or directly:

```bash
go test -v ./test/integration/tasks/
```

Tests cover:

- Task list creation
- Task creation with all fields
- Task updates (status, priority, etc.)
- Task retrieval
- Task listing with filters
- Complete workflows
- Dependencies between tasks

Unit tests in `claim_test.go` cover the atomic claim logic (`claimNext`):

- Lowest-pending selection and `task-N` ordering (`TestClaimNext_PicksLowestPending`)
- Skipping non-pending tasks (`TestClaimNext_SkipsNonPending`)
- Dependency gating — blocked until deps complete (`TestClaimNext_RespectsDependencies`)
- No-task-available and empty-list cases
- **Concurrent no-double-assignment**: 3 workers drain 50 tasks under the real
  locking discipline, asserting every task is claimed exactly once
  (`TestClaimNext_ConcurrentNoDoubleAssignment`)

All tests use the extension host to load and execute the WASM module, ensuring real-world behavior.

## Architecture

### Extension Pattern

The tasks extension follows the standard wllr extension pattern:

1. **Initialization (`_init`)**: Registers all tools and subscribes to `before_tool_call` events
2. **Event handling (`_on_event`)**: Receives tool call events and dispatches to handlers
3. **Tool execution**: Handlers process input, update state, and call `tool_result`
4. **Memory management**: Uses `_alloc`/`_free` exports for WASM<->host communication

### Key Components

- **WASM imports**: `host_log`, `host_call` for communicating with the host
- **WASM exports**: `_alloc`, `_free`, `_init`, `_on_event` required by the extension ABI
- **State management**: In-memory maps with mutex protection
- **ID generation**: Sequential counters for predictable, deterministic IDs

## Implementation Notes

### Why Sequential IDs?

- Predictable for testing
- Deterministic
- Simpler than UUIDs (no external dependencies)
- Sufficient for in-memory storage

### Thread Safety

All state access is protected by `sync.RWMutex`:

- `taskListMu` protects the task lists map
- Each `TaskList` has its own `mu` for task operations
- `counterMu` protects ID counters

### Error Handling

All tools validate inputs and return proper error messages via `tool_result` with `is_error: true`.

## Files

```
extensions/tasks/
├── main.go        # Extension implementation
└── tasks.json     # Tool manifest (copied to ~/.wllr/extensions/tasks/)

test/integration/tasks/
└── tasks_test.go  # Integration tests
```

## Future Enhancements

Potential additions:

- Persistence to disk
- Task search/filtering
- Subtasks
- Task assignment
- Due dates
- Task history/audit log
- Export/import functionality
