# queue extension

Owns everything queued-message related: the `/queue` slash command for the
user, and the agent-facing `queue_peek` / `queue_cancel` tools.

## Slash command

```
/queue                      - List all agents with pending messages
/queue <agent-id>           - Show inbox for specific agent
/queue list                 - List all agents with pending messages
/queue delete <agent-id> <index> - Delete message by 1-based index
/queue clear <agent-id>     - Discard ALL queued messages (works while a turn is running)
/queue edit <agent-id> <index> <content> - Edit message by 1-based index
```

## Agent tools

A message sent to an agent while it is working is queued and replayed when the
current turn ends. Two registered tools let agents manage that queue
themselves:

- **`queue_peek(agent_id?)`** — list the target's pending messages with their
  0-based index, sender role, and content. Each message carries an ID assigned
  at queue time (or a caller-supplied one).
- **`queue_cancel(agent_id?, index?, message_id?)`** — cancel queued messages.
  With no selector it discards the whole queue; with a `message_id` (from
  `queue_peek`) it cancels exactly one. The whole-queue clear and the
  message-id cancel work while a turn is running; the index cancel requires
  the agent to be idle (indexes shift as messages arrive, so an index is only
  meaningful against a quiescent snapshot). Cancelled messages cannot be
  recovered.

### Ownership rule

An agent may target **itself or its own descendants** (agent IDs are built as
`<creator>/<name>`, so a descendant is one the caller's ID prefixes with `/`).
A sub-agent therefore cannot discard the orchestrator's queue or a sibling's;
the root agent (`main`, the default when a call carries no agent context) may
target anything.

This rule is enforced here, in the extension. The underlying host calls
(`mailbox_snapshot`, `mailbox_delete`, `mailbox_clear`) are deliberately
ungated: the user's `/queue` command and the harness UI (`ctrl+x`, the
`[ clear ]` button) are user actions and may target any agent.

## Cancel semantics

Esc-cancelling a turn preserves the queue (issue #48 option (a)); only an
explicit clear — by the user or by an agent via `queue_cancel` — discards
queued messages.
