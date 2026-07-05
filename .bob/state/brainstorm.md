# Brainstorm for Ticket #15

## User Goals & Requirements

- **Goal**: Provide full visibility and control over queued agent messages
- **Problem**: Users currently cannot see what messages are queued, how many agents are waiting, or why a message isn't being processed
- **Stakeholders**: End users, developers, operators

### MVP Requirements
1. `/queue` CLI command to inspect queued messages
2. Statusline indicator showing pending message count
3. Mailbox APIs for snapshot, edit, and delete operations

## Design Considerations

### UX
- `/queue` should support filtering by agent, status (pending/delivered/failed), and time range
- Statusline indicator must be non-intrusive but informative (e.g., small badge on the right side)
- Interactive mode: show preview of message payload before deletion

### Concurrency & Reliability
- Queue data must be atomic and consistent across multiple agents
- Handle race conditions when editing/deleting messages mid-delivery
- Support message TTL to prevent indefinite queue bloat

### Extensibility
- Queue backend must be pluggable (in-memory, SQLite, Redis)
- CLI subcommands should allow for future extension (e.g., `/queue stats`, `/queue replay`)

## Proposed /queue Command Interface

```bash
# List all queued messages
/wllr queue list

# Filter by agent name or ID
/wllr queue list --agent=main

# Show status summary
/wllr queue stats

# Delete a message by ID
/wllr queue delete <message-id>

# Replay a failed message
/wllr queue replay <message-id>
```

Flags:
- `--limit N`: limit number of entries returned
- `--sort by-time|by-agent`: sort order
- `--format json|table`: output format

## Statusline Indicator Spec

### Visual Design
- Small badge: `queued(N)` where N = number of pending messages
- Color coding:
  - Green: N ≤ 5
  - Yellow: 5 < N ≤ 20
  - Red: N > 20

### Interaction
- Click to open full queue view
- Hover tooltip: show top 3 agents with pending work

## Mailbox API Requirements

### Snapshot
- Capture current queue state at a point in time
- Include metadata: timestamp, agent count, message IDs

### Edit
- Partial update of message payload (JSON patch style)
- Validate schema compatibility before editing

### Delete
- Hard delete: remove permanently
- Soft delete: mark as `archived` for audit trail

## Edge Cases & Failure Modes

| Case | Handling |
|------|----------|
| Queue overflow (too many pending messages) | Drop oldest or reject new messages with HTTP 429 |
| Message delivery failure loop | Increment retry count, alert on threshold breach |
| Concurrent edit conflict | Use optimistic locking via `version` field |
| Agent shutdown mid-delivery | Requeue or archive pending messages |

## Suggested Tests

### Unit Tests
- `/queue list` returns correct pagination and sorting
- Mailbox edit preserves message ID and timestamps
- Concurrent delete does not cause race condition

### Integration Tests
- Start 10 agents with queued messages → verify statusline shows correct count
- Simulate network partition → ensure queue state survives crash/restart

### End-to-End Tests
- User issues `/queue delete <id>` → message disappears from pending set
- High-volume stress test (1k messages/sec) → no data loss or UI freeze
