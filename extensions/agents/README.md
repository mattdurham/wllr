# Agents Extension

The agents extension exposes tools for spawning child agents, sending messages,
and grouping agents into teams. Tool schemas are registered at startup; the full
input and output contract reference is
[`docs/tool-contracts.md`](../../docs/tool-contracts.md#agents-extension).

Successful results are JSON strings. Validation and host-operation failures mark
the tool call as failed with plain-text messages such as
`create_agent: name is required`.

## Tools

- `create_agent` creates a scoped child agent ID and starts its first turn.
- `shutdown_agent` queues a shutdown request for a child agent.
- `list_agents` returns live agents with running and pending-message state.
- `create_team`, `add_to_team`, `get_team`, and `shutdown_team` manage teams.
- `send_message` queues a message and wakes the target agent.
