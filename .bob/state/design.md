# Model Context Window Resolution

**Goal:** Ensure every agent uses a trustworthy context window for compaction, obtaining it from provider metadata, built-in model knowledge, or an explicit user value before the model can run.

**Architecture:** Introduce a shared resolved model metadata record containing the model ID and context window. Provider/API and local endpoint metadata feed the resolver; built-in metadata covers known model families; unresolved models are represented in model selection as requiring a context-window prompt. Store the resolved value with each agent/turn instead of using one pool-wide value, and propagate model identity through subagent spawning.

**Tech Stack:** Go, Bubble Tea model selection flows, fantasy providers, existing config persistence, Go unit/race tests.

---

## Requirements

- A model must not start with an arbitrary default context window.
- Resolution precedence is provider/API metadata, built-in known metadata, persisted user-entered per-model metadata, then an interactive required prompt.
- Explicit user configuration remains authoritative for that model.
- Main agents and subagents may use different models and must compact against their own windows.
- A model/LM/context metadata snapshot must remain internally consistent for a turn.
- Existing model picker and local-model setup flows should collect the value at selection time.
- Context metadata should be persisted per provider/model so subsequent selections do not prompt again.
- Update module specs, notes, API/tool documentation where behavior or contracts change.
- Add tests for known, API/local, persisted, unknown/prompt-required, and mixed-window subagent cases.

## Non-goals

- Implementing a tokenizer or exact provider token accounting.
- Automatically discovering context limits by sending trial requests.
- Changing worktree or task-ledger behavior.

## In-turn context growth (2026-09-20)

**Observed failure:** A provider rejected a later tool-loop request at 262,202
tokens against a 262,144-token window. The existing preflight runs once before
Fantasy's multi-step tool loop; its reactive retry can only compact persisted
history, not the tool results accumulated inside that loop.

**Design:** Use Fantasy's per-step preparation hook to check the previous
provider-reported input usage plus the newest step messages before every next
request. When the configured threshold (capped at a safety level) is reached,
summarize the active transcript with the same model. Send that summary as a
user message and retain subsequent step messages verbatim. Repeat when needed
within the same turn. Keep the original stored history behavior. Abort the turn
with an explicit compaction error if summarization fails; do not send an
oversized request or replay already executed tools.

## Context Usage Metric Correction (2026-08-27)

Fantasy's `AgentResult.TotalUsage` sums input tokens across every provider
step in a tool loop. It is useful cumulative billing telemetry, but it is not
the prompt size or context occupancy for the turn. The context metric will use
the maximum `StepResult.Usage.InputTokens` observed in the turn, which reports
the peak prompt sent to the provider and remains safe when the loop grows.

The existing cumulative usage remains available inside the stream result for
telemetry and is not used for compaction thresholds or the context statusline.
Failed and cancelled turns continue to clear the stored usage. The public
`ContextUsage` wire shape is unchanged; this corrects the meaning of its
`InputTokens` field to match the existing specification.

## Ordered Task Runner Design (2026-09-17)

**Goal:** Let a user create an ordered durable task list whose tasks are each
performed by a fresh sub-agent, with explicit completion and user-visible review
pauses.

**Architecture:** Add an optional `task-runner` WASM extension. It uses the
existing task ledger for durable task state and the existing agent host calls
for child lifecycle. The runner owns one active child at a time. A child must
call `mark_task_completed` or `request_task_review`; completion reports the
task, requests graceful shutdown, and starts the next task only after the
shutdown acknowledgement. Review reports a blocked task and calls `Notify`,
which makes the issue visible in the main chat. Runner state is persisted in
the extension store so restart/compaction does not lose the active list.

**Contract:** Tasks remain in the existing `pending`, `in_progress`,
`completed`, `blocked`, `failed`, or `cancelled` ledger states. Review is a
runner workflow state represented by `blocked` plus a structured review reason;
this avoids changing the ledger ABI for the first implementation. Task
prompts include the task identity, description, completion contract, and
review contract. The runner requires the tool caller's `agent_id` from the
`before_tool_call` payload and rejects stale or non-owned completions.

**Non-goals:** Worktree creation, arbitrary parallel scheduling, automatic
retries, or changing the existing task ledger status vocabulary.
