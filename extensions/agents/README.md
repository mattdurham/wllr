# Agents Extension

The agents extension exposes tools for spawning child agents, sending messages,
and grouping agents into teams. Tool schemas are registered at startup; the full
input and output contract reference is
[`docs/tool-contracts.md`](../../docs/tool-contracts.md#agents-extension).

Successful results are JSON strings. Validation and host-operation failures mark
the tool call as failed with plain-text messages such as
`create_agent: name is required`.

## Tool-loop guard

This extension also owns the tool-loop guard: a `before_tool_call` interceptor
that watches each agent's recent tool calls (main and sub-agents) and BLOCKS a
call when the recent pattern repeats, so a stuck model cannot burn tokens
making identical calls. The block reason reaches the model as the tool error:

```
[loop guard] the identical exec call has now been made 3 times in a row and
was blocked. Identical calls produce identical results — repeating this
pattern cannot make progress. Stop and reorient now: ...
```

Detection is input-sensitive: a call is identified by tool name plus its
normalized JSON input, so the legitimate read→edit→read→verify workflow never
fires (its edit inputs differ). Two triggers over the recent window:

- **Consecutive** — the last `min_repeats` (default 3) calls are identical.
  One identical retry after a transient failure is forgiven; the third means
  the model is stuck. Blocked calls are rolled back out of the buffer, so
  denial retries cannot consume window slots.
- **Cycle** — the tail of the window repeats as 2+ identical blocks of a
  period up to `max_period` (default 5). Longer cycles are treated as
  legitimate per-file batches.

Blocked calls never execute; the model must take a materially different
approach or end its turn with a summary.

### Configuration

Optional — missing config uses the defaults (guard enabled, 10-call window,
3-call consecutive threshold, all agents):

```yaml
# ~/.wllr/extensions/agents/config.yaml
loop_guard:
    enabled: true      # false disables the guard entirely
    window: 10         # recent calls examined for a pattern
    min_repeats: 3     # consecutive identical calls before blocking
    max_period: 5      # longest cycle length considered a loop
    scope: all         # "subagents" exempts the main agent
```

A malformed config file disables the guard and logs an error (a broken config
must not silently run with guessed settings).

## Tools

- `create_agent` creates a scoped child agent ID and starts its first turn. Its
  optional `model` field selects a model. For local models, the configured
  `wllr.local_models` entry supplies the endpoint and API key. The optional
  `endpoint` must match that entry; omitted fields use the current defaults.
- `shutdown_agent` queues a shutdown request for a child agent.
- `list_agents` returns live agents with running and pending-message state.
- `create_team`, `add_to_team`, `get_team`, and `shutdown_team` manage teams.
- `send_message` queues a message and wakes the target agent.
- `queue_peek` / `queue_cancel` (queue extension) inspect and cancel messages
  queued for an agent — your own queue by default, or a descendant's by
  `agent_id`; never the orchestrator's or a sibling's. See
  extensions/queue/README.md.

When coordinating durable work, use the tasks extension as the source of truth:
pass `list_id`, `task_id`, and `attempt_id`, report outcomes with
`tasks_report`, and reconcile missed wakes with `tasks_events_after`. Deduplicate
events by `event_id` and use `version` for CAS updates. `workspace_mode`
(`shared`, `worktree`, or `readonly`) is currently metadata only. Use
`send_message` for prose and progress; report before going idle. Never poll,
sleep, or use `wait_for_all`, and inspect liveness before retrying work.

## Transcript ownership and callbacks

This extension owns the `chat` scene area — the main transcript — and re-renders
it from agent history. Message boxes carry a one-line bottom margin
(`messageBoxProps`) so consecutive messages render separated; the same props
serve live streaming and history replay, so the two always look identical.

Two internal `on_command` callbacks drive that:

- `agents:focus` (agent ID) — `/agents` tree selection: switches the focused
  agent, rebuilds the transcript from its history, and notifies.
- `agents:transcript_rebuild` (agent ID; empty means the root agent) — fired by
  the harness after a history restore rewrites the main agent's history, so the
  restored conversation is visible. Rebuilds only; no focus switch, no
  notification.
