# History Extension

Bundled (built-in) WASM extension that records each conversation to
append-only JSONL and provides an interactive `/history` command to browse past
sessions and **resume from any point** — replaying the chosen context back into
the agent.

## What it does

- **Records** every turn of the current session to
  `~/.wllr/sessions/<sanitized-cwd>/<timestamp>_<id>.jsonl`, one JSON object per
  line: a session header, then user/assistant messages and tool calls in order.
- **Browses + resumes** via `/history`: a two-step picker that lets the user
  pick a session, then pick the exact message to resume from. The selected
  prefix of the conversation is replayed into the agent's context.

## Recording

Subscribes to four lifecycle events:

| Event | Recorded as |
|-------|-------------|
| `session_start` | session header (creates the file) |
| `before_agent_start` | `message` entry, role `user` |
| `message_end` (assistant) | `message` entry, role `assistant` |
| `before_tool_call` | `tool_call` entry (name + input) |

Files live under a per-cwd directory so sessions are scoped to the project you
were working in. The filename timestamp/id use `time.Now()` + `crypto/rand` so
they are unique even though the WASM runtime's clock may be unreliable (session
list display uses the file mtime instead — see `peekSession`).

## The `/history` flow (browse → choose point → replay)

`/history` runs a **two-step picker**:

1. **Select a session.** Lists up to the 20 most recent sessions (across all
   cwds), newest first, each showing its timestamp and a preview of the first
   user message. (The in-progress current session is excluded.)
2. **Select a resume point.** Lists every message in that session, numbered and
   tagged `you`/`asst` with a one-line preview.

Selecting a message calls `AgentResetHistory` with the messages **up to and
including** that index, so the agent's context becomes exactly that prefix and
the next turn continues from there. Selecting the last message resumes the whole
conversation. A notification reports how many of N messages were replayed.

> The picker is the standard host `ShowPicker` overlay (not a modal). A modal is
> only used for the "no sessions found" / "could not load" messages. The second
> picker is opened from the first picker's selection callback
> (`history:session_selected`), and the replay happens in the second callback
> (`history:message_selected`), coordinated by the `pendingSessionPath` var.

### Replay normalization

`loadMessages` prepares stored entries for the provider API:

- `tool_call` lines and empty-content messages are skipped.
- Consecutive same-role messages collapse to the first (the API requires
  alternation).
- Leading assistant messages are dropped (history must start with a user
  message).

## JSONL schema

| Entry type | Fields |
|------------|--------|
| `session` | `type`, `id`, `timestamp` (RFC3339Nano), `cwd` |
| `message` | `type`, `id`, `timestamp`, `role` (`user`/`assistant`), `content` |
| `tool_call` | `type`, `id`, `timestamp`, `tool_call_id`, `tool_name`, `input` |

## Commands

| Command | Effect |
|---------|--------|
| `/history` | Browse sessions, pick a resume point, replay context up to it |

Internal picker callbacks (not user-facing commands): `history:session_selected`
(session → message picker) and `history:message_selected` (message → replay).

## Permissions

Reads and writes files under `~/.wllr/sessions/`. As a built-in it is trusted
and receives all permissions automatically.

## Files

```
extensions/history/
├── main.go            # wasip1: init, event handlers, pickers (requires the host)
├── sessionio.go       # host-testable: loadMessages + sanitizePath (no build tag)
├── sessionio_test.go  # unit tests for the above (run on host)
├── messageentry.go    # JSONL message entry (shared)
├── sessionheader.go   # JSONL session header (wasip1)
├── toolcallentry.go   # JSONL tool-call entry (wasip1)
├── sessioninfo.go     # session list row (wasip1)
├── storedmsg.go       # normalized message (shared)
├── message.go         # AgentResetHistory wire type (wasip1)
├── pickeritem.go      # ShowPicker item (wasip1)
└── wllrsdk.go         # copied SDK boilerplate (wasip1)
```

### Why `sessionio.go` has no build tag

`main.go` and `wllrsdk.go` are `//go:build wasip1` (they call host imports and
only build for the WASM target). The pure session-parsing logic
(`loadMessages`, `sanitizePath`) is extracted into `sessionio.go` **without** a
build tag so it compiles on the host and can be unit-tested directly — the same
pattern the `tasks` extension uses for `claim.go`/`claim_test.go`.

## Tests

`sessionio_test.go` covers the replay normalization and path handling:

- `TestLoadMessages_BasicOrder` — messages returned in order with roles
- `TestLoadMessages_SkipsToolCallsAndEmpty` — tool calls and empty messages dropped
- `TestLoadMessages_CollapsesConsecutiveSameRole` — enforces alternation
- `TestLoadMessages_DropsLeadingAssistant` — history starts with a user message
- `TestLoadMessages_MissingFile` — error on missing file
- `TestSanitizePath` — cwd → directory-name mapping

Run them from the extension module:

```bash
cd extensions/history && go test ./...
```
