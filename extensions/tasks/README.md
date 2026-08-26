# Tasks Extension

The tasks extension is a compatibility facade over the host-owned durable task
ledger. It registers `tasklist_create`, `tasks_create`, `tasks_update`,
`tasks_list`, `tasks_get`, and `tasks_claim`, plus `tasks_report` and
`tasks_events_after` for durable worker coordination. It keeps no authoritative
process-local task state.

Every task has opaque `list_id`/`task_id`, a monotonically increasing `version`,
and (after claim) an `attempt_id`. Updates require `expected_version`; stale
writers receive an error. A claim is persisted as `in_progress` and must be
completed with `tasks_report` using the matching attempt. Completed reports
require structured `result`; blocked, failed, and cancelled reports require
`reason` or `error`.

`workspace_mode` accepts `shared`, `worktree`, or `readonly` as metadata
placeholders only. This extension does not allocate worktrees or enforce shell
or filesystem permissions.

After every wake, compaction, reconnect, or suspected missed notification, call
`tasks_events_after(list_id, cursor)` and deduplicate by `event_id`. Cursors are
bounded and replay is authoritative; a wake is only a best-effort prompt. Use
`send_message` for prose, progress, and questions. Do not infer completion from
`TASK_DONE` text or idle state.

Workers should claim, perform the work, report the structured outcome before
going idle, and explicitly retry/requeue only through supported host recovery.
Do not poll, sleep, or use `wait_for_all`.

Build with `make extensions`. Integration coverage is in
`test/integration/tasks/`.
