# Task runner extension

The task runner executes one durable task at a time using a fresh sub-agent.
The child must call `mark_task_completed` with structured evidence or
`request_task_review` when it needs a human decision. Completion requests a
graceful shutdown and the next child is not spawned until the old child has
left the live agent list.

Create tasks with the `tasks` extension, then call `task_runner_start` with the
list ID. State is stored in the extension's private store and task truth stays
in the host-owned ledger.
