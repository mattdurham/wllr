---
type: Built-in Extension
title: history (built-in)
description: Records each conversation to JSONL and provides /history — browse a session, pick a resume point, and replay context up to it.
resource: ./extensions/history
tags: [built-in, history, sessions, replay, jsonl]
timestamp: 2026-07-01T13:10:47Z
---

The `history` built-in extension is the **sole session store**. It records every
turn (messages + tool calls) to append-only JSONL under
`~/.wllr/sessions/<sanitized-cwd>/`, and `/history` runs a two-step picker:
pick a session, then pick the exact message to resume from — replaying that
prefix of context back into the agent via `AgentResetHistory`.

# Source

- [extensions/history](../../extensions/history) — includes its own [README](../../extensions/history/README.md)
- Host-testable logic split into `sessionio.go` (untagged) with `sessionio_test.go`

# Uses

- Lifecycle events (session_start, before_agent_start, message_end, before_tool_call),
  ShowPicker, AgentResetHistory, file I/O

# Related

- [History is the sole session store](../decisions/history-is-sole-session-store.md)
- [session package](session.md)
