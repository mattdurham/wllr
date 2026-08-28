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
