---
type: Decision
title: The history extension is the sole session store
description: The core session.Journal was removed; the history WASM extension records and replays sessions.
tags: [session, history, sessions]
timestamp: 2026-07-01T13:10:47Z
---

**Decision:** Session recording lives only in the `history` built-in extension
(JSONL under `~/.wllr/sessions/`, with browse + resume-point replay). The former
core `session.Journal` (and `LoadSession`, which had no production callers) was
removed.

**Rationale:** The extension strictly superseded the core journal — it records
tool calls too and provides the `/history` UI. Two parallel recording paths were
redundant and risked divergence.

**Consequence:** Do not reintroduce a core journal. Session persistence changes
belong in the `history` extension. The harness `OnUserMessage`/`OnMessageEnd`
hooks remain available for other consumers.

# Applies To

- [session package](../packages/session.md), [history extension](../packages/ext-history.md)

# Origin

modules/session/NOTES.md §6; commit 29c7e32.
