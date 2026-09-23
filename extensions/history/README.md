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
were working in. The session directory and header timestamp come from **host
ground truth** (`session_start` payload `cwd`/`started_at`, falling back to the
`host_info` host_call) because the WASM sandbox has no working directory
(`os.Getwd` returns `/`) and an unreliable clock — without this, every session
landed in a single `""` subdirectory with 2022 timestamps. The filename id uses
`crypto/rand` for uniqueness.

## The `/history` flow (browse → choose point → replay)

`/history` runs a **two-step picker**:

1. **Select a session.** A **two-pane split view** (ccresume-style): the left
   half lists up to the 20 most recent sessions **for the current project**
   (the per-cwd directory the active session writes into), newest first, each
   showing its timestamp and a preview of the first user message; the right
   half renders the highlighted conversation as a `you:`/`asst:` transcript.
   **Typing filters the list live** — the query matches the label, path,
   first-message preview, *and the full conversation text*. `pgup`/`pgdn`
   scroll the preview pane; `enter` proceeds; `esc` cancels. The in-progress
   current session is excluded. `/history all` widens the listing to every
   folder's sessions. Listing is done **host-side** via the `list_sessions`
   host_call with real host mtimes and previews, because the WASM sandbox
   cannot reliably enumerate or stat the host filesystem.
2. **Select a resume point.** Lists every message in that session, numbered and
   tagged `you`/`asst` with a one-line preview.

Selecting a message calls `AgentResetHistory` with the messages **up to and
including** that index, so the agent's context becomes exactly that prefix and
the next turn continues from there. Selecting the last message resumes the whole
conversation. A notification reports how many of N messages were replayed.

The restored conversation is **re-rendered into the transcript**: after
`AgentResetHistory`, the harness resets the chat area and fires
`agents:transcript_rebuild` with the main agent id, and the transcript-owning
agents extension rebuilds the view from the agent's history (the same
`RebuildTranscriptFor` used by `/agents` focus, without the focus switch or
notification). Without that rebuild the replay lands in context only and the
chat goes blank.

> The session browser is the host `ShowPickerSplit` overlay (the standard
> picker in its two-pane split mode), not a modal. A modal is only used for the
> "no sessions found" / "could not load" messages. The second picker is opened
> from the first picker's selection callback (`history:session_selected`), and
> the replay happens in the second callback (`history:message_selected`),
> coordinated by the `pendingSessionPath` var.
>
> The transcript preview is capped (`transcriptPreview` in sessionio.go): each
> message contributes at most `maxPreviewMsgLines` wrapped lines and the whole
> preview at most `maxPreviewLines`, with `…` markers at each cut, so the
> `show_picker` payload stays bounded for 20 listed sessions.

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
| `/history` | Browse the current project's sessions, pick a resume point, replay context up to it |
| `/history all` | Same, but list sessions from every folder |

Internal picker callbacks (not user-facing commands): `history:session_selected`
(session → message picker) and `history:message_selected` (message → replay).

## Permissions

Reads and writes files under `~/.wllr/sessions/`. As a built-in it is trusted
and receives all permissions automatically.

## Files

```
extensions/history/
├── main.go            # wasip1: init, event handlers, pickers (requires the host)
├── sessionio.go       # host-testable: loadMessages + transcriptPreview + sanitizePath (no build tag)
├── sessionio_test.go  # unit tests for the above (run on host)
├── messageentry.go    # JSONL message entry (shared)
├── sessionheader.go   # JSONL session header (wasip1)
├── toolcallentry.go   # JSONL tool-call entry (wasip1)
├── storedmsg.go       # normalized message (shared)
├── message.go         # AgentResetHistory wire type (wasip1)
├── pickeritem.go      # ShowPicker item incl. split-mode Preview (wasip1)
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

## Host-side listing (`list_sessions`)

The `/history` picker calls the `list_sessions` host_call (host handler in
`modules/extension/host.go`). The host walks `~/.wllr/sessions` (root-level
`.jsonl` files plus files one subdirectory deep), stats each file for its real
mtime, extracts the first non-empty user message as a preview, sorts newest
first, and caps at `limit` (default 25), excluding the current session file.
An optional `dir` param scopes the walk to a single session directory — the
default `/history` passes its own per-cwd directory so only the current
project's sessions are listed; `/history all` omits it. This requires
`file_read`; the bundled extension is trusted and receives it automatically.
Message *loading* (`loadMessages`) still happens in-guest — the guest
filesystem is readable for file contents; only stat/mtime and reliable
enumeration needed the host.

## What is recorded

Only the root agent's directly-sent prompts and the assistant's replies. A
`before_agent_start` event with `queued: true` is inbox-delivered work — a
sub-agent's task arriving, or a lifecycle notification — and is **not** recorded
as a user message, because once stored it is indistinguishable from a prompt the
user typed. Sub-agent turns are attributed by `agent_id` and skipped for the same
reason.
