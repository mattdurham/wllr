# Brainstorm — Show and Manage Queued Agent Messages (/queue command)

**Ticket:** GitHub issue #15  
**Brainstorm date:** 2026-07-04  
**Agent ID:** main/brainstorm-t15

---

## 1. User Goals & Requirements

### Primary Goal
Users need visibility into and control over the pending message queues (inboxes) of agents in real-time — especially when orchestrating complex multi-agent workflows.

### Underlying Needs

1. **Debugging**: Identify why an agent appears "stuck" — is it waiting on messages, tools, or a missing wakeup?
2. **Inspection**: Inspect pending messages before they're processed to verify correctness or redact sensitive data.
3. **Intervention**: Manually edit/delete messages that were sent incorrectly or are no longer relevant.
4. **Status awareness**: See how many messages are queued when the status line only shows "working" or "idle".

### Feature Requirements

- **`/queue` command**: View queued messages for one or all agents
  - Show agent ID, inbox count, and first few message previews
- **Statusline indicator**: Visual badge or text showing queued message count
  - e.g., `queued:3` or `[3]` when inbox is non-empty
- **Mailbox API**: Programmatic access for extensions to snapshot, edit, and delete queued messages
  - `mailbox_snapshot(agent_id)`: Get full copy of inbox without draining
  - `mailbox_delete(agent_id, index|message_id)`: Remove specific messages
  - `mailbox_edit(agent_id, index|message_id, new_content)`: Update message content

---

## 2. Current Design (As-Is)

### Agent Inbox Structure

**File:** `modules/agent/mailbox.go`

The inbox is abstracted as an unexported `mailbox` type embedded in `Agent`:

```go
type mailbox struct {
    msgs []sdk.Message
    mu   sync.RWMutex
}
```

**Key properties:**

- Thread-safe (uses `sync.RWMutex`)
- Zero-value is ready to use
- Embedded by value in `*Agent`
- Enforces invariant: blank content messages are dropped with warning (Anthropic API rejection)

**Current methods on `Agent`:**

| Method | Purpose |
|--------|---------|
| `AppendInbox(msg)` | Enqueue a message (FIFO) |
| `DrainInbox()` | Atomically return all messages and clear queue |
| `InboxLen()` | Return count of queued messages (no drain) |

### Delivery Model

**File:** `modules/agent/pool.go` — `Deliver(id, msg, wake bool)`

The `Deliver` method is the atomic "append + process" primitive:

```go
func (p *AgentPool) Deliver(id string, msg sdk.Message, wake bool) error
```

- Appends message to inbox
- If `wake=true`, calls `Submit("")` to ensure processing
- Uses drain-until-empty pattern (see NOTES.md §17)

### Message Types

**File:** `modules/sdk/types.go`

```go
type MessageType string

const (
    MessageTypeNormal   // visible to LLM, recorded in history
    MessageTypeSteering // visible in history but filtered from LLM context
    MessageTypeSystem   // Go-level control, never reaches LLM or history
)
```

### Current AgentBridge Interface

**File:** `modules/extension/interfaces.go`

```go
type AgentBridge interface {
    Spawn(ctx context.Context, req SpawnRequest) error
    Close(id string) error
    SendMessage(id string, msg sdk.Message) error
    Deliver(id string, msg sdk.Message, wake bool) error
    Run(id string) error
    List() ([]AgentInfo, error)
    TokenCount() int64
    SetHistory(id string, messages []sdk.Message) error
    MainAgentContextUsage() sdk.ContextUsage
}
```

**Missing operations:**
- No inbox inspection (snapshot/read-only view)
- No inbox mutation (edit/delete queued messages)

---

## 3. Design Considerations

### UX Considerations

| Concern | Decision | Rationale |
|---------|----------|-----------|
| Where to show inbox count | Statusline badge next to working indicator | Minimal disruption; high visibility when useful |
| /queue output format | Tabular: `AGENT_ID  QUEUED  FIRST_MESSAGE_PREVIEW` | Scannable, aligns with existing tool output |
| Message preview length | Truncate to 100 chars with ellipsis | Prevents UI overflow while preserving context |
| Multiple agent view | Show all agents with non-zero inbox | One screen, scrollable if needed |
| Edit/delete safety | Require explicit confirmation for destructive ops | Prevent accidental data loss |

### Concurrency & Thread-Safety

- Inbox operations must remain atomic (`mailbox.append`/`drain`)
- Snapshot API must copy messages (not share reference)
- Edit/delete during active turn: either block or warn user
  - **Decision**: Block edit/delete on running agents; allow on idle agents only

### Extensibility

**Inbox mutation via WASM extensions:**

Extensions should be able to:

1. Inspect inbox without draining (`mailbox_snapshot`)
2. Delete specific messages by index or unique ID
3. Edit message content (with validation: non-empty, proper type)

**Host API additions needed (update AgentBridge):**

```go
type AgentBridge interface {
    Spawn(ctx context.Context, req SpawnRequest) error
    Close(id string) error
    SendMessage(id string, msg sdk.Message) error
    Deliver(id string, msg sdk.Message, wake bool) error
    Run(id string) error
    List() ([]AgentInfo, error)
    TokenCount() int64
    SetHistory(id string, messages []sdk.Message) error
    MainAgentContextUsage() sdk.ContextUsage
    // NEW: Read-only inbox snapshot (thread-safe)
    SnapshotInbox(id string) ([]sdk.Message, error)
    // NEW: Delete message(s) from inbox (thread-safe)
    DeleteFromInbox(id string, byIndex int, byMessageID string) (int, error)
    // NEW: Edit message content in inbox (thread-safe)
    EditInboxMessage(id string, byIndex int, byMessageID string, newContent string) error
}
```

### Edge Cases & Failure Modes

| Case | Current behavior | Proposed handling |
|------|------------------|-------------------|
| Queue overflow (10k+ messages) | Memory grows unbounded | Add soft limit; warn user |
| Message expiry (stale queue items) | No expiry | Add optional TTL on messages |
| Editing deleted message | Panic or undefined | Return error: "message not found" |
| Edit on running agent | Undefined behavior | Block; return error: "agent is busy" |
| Delete while drain-until-empty running | Race condition | Lock inbox during edit/delete |

---

## 4. Proposed `/queue` Command Interface

### Command Syntax

```bash
/queue [agent_id] [--preview N] [--full]
```

### Flags

| Flag | Description |
|------|-------------|
| `-a`, `--all` | Show all agents with non-empty inbox |
| `-p N`, `--preview N` | Truncate preview to N characters (default: 100) |
| `-f`, `--full` | Show full message content (overrides preview) |
| `-j`, `--json` | Output as JSON array |

### Output Formats

**Tabular (default, human-readable):**

```
AGENT_ID      QUEUED  FIRST_MESSAGE_PREVIEW
───────────── ──────  ───────────────────────────────────────────────
main               3  [User] Task started...
worker/1           2  [User] Please review PR #123
```

**JSON (`--json`):**

```json
[
  {
    "agent_id": "main",
    "queued": 3,
    "messages": [
      { "role": "user", "content_preview": "[User] Task started...", ... },
      ...
    ]
  }
]
```

**Single-agent view:**

```bash
/queue worker/1 --full
```

---

## 5. Statusline Indicator Spec

### Visual Design

**Location:** After working indicator, before context usage (if configured)

**Format:** `queued:N` or `[N]`

- ` queued:0` → hidden (no badge)
- ` queued:3` → red foreground, small font
- ` queued:10+` → warning color (orange/red)

**Example statusline transitions:**

```
# Idle:
>> ChatGPT  gpt-4.1  queued:0  ctx:23%

# User sends message:
>> ChatGPT  gpt-4.1  working.  ctx:23%

# Sub-agent sends message to main:
>> ChatGPT  gpt-4.1  queued:1  ctx:23%
```

### Event Triggers

| Event | Action |
|-------|--------|
| `agent_deliver(wake=true)` to main agent | Update badge via `SetStatusLine` or UI patch |
| `agent_deliver(wake=false)` to main agent | Update badge immediately (no wake indicator) |
| `agent_deliver` to sub-agent | Update agent-specific badge in agents pane |

---

## 6. Mailbox API Requirements

### AgentBridge Interface Extensions

**File:** `modules/extension/interfaces.go`

```go
type AgentBridge interface {
    Spawn(ctx context.Context, req SpawnRequest) error
    Close(id string) error
    SendMessage(id string, msg sdk.Message) error
    Deliver(id string, msg sdk.Message, wake bool) error
    Run(id string) error
    List() ([]AgentInfo, error)
    TokenCount() int64
    SetHistory(id string, messages []sdk.Message) error
    MainAgentContextUsage() sdk.ContextUsage
    // SnapshotInbox returns a copy of the inbox messages without draining.
    // Thread-safe. Returns empty slice if agent has no queued messages.
    SnapshotInbox(id string) ([]sdk.Message, error)
    
    // DeleteFromInbox removes messages from an agent's inbox.
    // At least one of byIndex or byMessageID must be provided.
    // Returns count of deleted messages, or error.
    DeleteFromInbox(id string, byIndex int, byMessageID string) (int, error)
    
    // EditInboxMessage updates a message's content by index or ID.
    // Content must be non-empty (Anthropic invariant).
    EditInboxMessage(id string, byIndex int, byMessageID string, newContent string) error
}
```

### Implementation Constraints

1. **Thread-safety**: All operations must hold inbox lock
2. **Validation**: `newContent` must be non-empty (Anthropic invariant)
3. **Error handling**:
   - Unknown agent → `error: "agent not found"`
   - Index out of range → `error: "index out of range"`
   - Running agent (edit/delete) → `error: "cannot modify inbox while agent is busy"`
   - Missing identifier (both byIndex and byMessageID empty) → `error: "must specify index or message_id"`
4. **Message ID generation**: Add optional `ID` field to `sdk.Message`
   - Format: `<agent_id>_<sequence>`
   - Increment per agent on append

### sdk.Message Structure Enhancement

**Current:**

```go
type Message struct {
    Role    Role        `json:"role"`
    Content string      `json:"content"`
    Type    MessageType `json:"type,omitempty"`
}
```

**Enhanced (add ID):**

```go
type Message struct {
    ID      string      `json:"id,omitempty"`       // NEW: unique per-append
    Role    Role        `json:"role"`
    Content string      `json:"content"`
    Type    MessageType `json:"type,omitempty"`
}
```

### WASM Extension SDK Changes

**File:** `extensions/wllrsdk.go` (additions)

```go
// MailboxSnapshot returns a copy of an agent's inbox without draining.
func MailboxSnapshot(agentID string) ([]Message, error) {
    params := map[string]string{"agent_id": agentID}
    raw := hostCall("mailbox_snapshot", params)
    if raw == nil {
        return nil, fmt.Errorf("mailbox_snapshot: no response")
    }
    var resp struct {
        Messages []Message `json:"messages"`
        Error    string    `json:"error,omitempty"`
    }
    if err := json.Unmarshal(raw, &resp); err != nil {
        return nil, err
    }
    if resp.Error != "" {
        return nil, fmt.Errorf("mailbox_snapshot: %s", resp.Error)
    }
    return resp.Messages, nil
}

// MailboxDelete removes messages from an agent's inbox.
func MailboxDelete(agentID string, byIndex int, byMessageID string) (int, error) {
    params := map[string]any{"agent_id": agentID}
    if byIndex >= 0 {
        params["index"] = byIndex
    }
    if byMessageID != "" {
        params["message_id"] = byMessageID
    }
    raw := hostCall("mailbox_delete", params)
    if raw == nil {
        return 0, fmt.Errorf("mailbox_delete: no response")
    }
    var resp struct {
        Success      bool  `json:"success"`
        DeletedCount int   `json:"deleted_count,omitempty"`
        Error        string `json:"error,omitempty"`
    }
    if err := json.Unmarshal(raw, &resp); err != nil {
        return 0, err
    }
    if !resp.Success {
        return 0, fmt.Errorf("mailbox_delete: %s", resp.Error)
    }
    return resp.DeletedCount, nil
}

// MailboxEdit updates a message's content.
func MailboxEdit(agentID string, byIndex int, byMessageID string, newContent string) error {
    if strings.TrimSpace(newContent) == "" {
        return fmt.Errorf("mailbox_edit: content must be non-empty")
    }
    params := map[string]any{
        "agent_id":   agentID,
        "new_content": newContent,
    }
    if byIndex >= 0 {
        params["index"] = byIndex
    }
    if byMessageID != "" {
        params["message_id"] = byMessageID
    }
    raw := hostCall("mailbox_edit", params)
    if raw == nil {
        return fmt.Errorf("mailbox_edit: no response")
    }
    var resp struct {
        Success bool   `json:"success"`
        Error   string `json:"error,omitempty"`
    }
    if err := json.Unmarshal(raw, &resp); err != nil {
        return err
    }
    if !resp.Success {
        return fmt.Errorf("mailbox_edit: %s", resp.Error)
    }
    return nil
}
```

---

## 7. Suggested Tests

### Unit Tests (package agent)

**File:** `modules/agent/inbox_edit_delete_test.go` (new)

```go
func TestMailbox_Snapshot_Isolation(t *testing.T) {
    mb := &mailbox{}
    mb.append("test", sdk.Message{ID: "1", Content: "one"})
    mb.append("test", sdk.Message{ID: "2", Content: "two"})

    snapshot := mb.snapshot() // NEW method
    if len(snapshot) != 2 {
        t.Fatalf("expected 2 messages, got %d", len(snapshot))
    }

    // Mutate snapshot — original mailbox must be unchanged
    snapshot[0].Content = "modified"
    
    // Verify original unchanged
    mb.mu.RLock()
    if mb.msgs[0].Content != "one" {
        t.Errorf("snapshot is not a copy")
    }
    mb.mu.RUnlock()
}

func TestMailbox_DeleteByIndex(t *testing.T) {
    mb := &mailbox{}
    mb.append("test", sdk.Message{ID: "1", Content: "one"})
    mb.append("test", sdk.Message{ID: "2", Content: "two"})
    mb.append("test", sdk.Message{ID: "3", Content: "three"})

    count, err := mb.deleteByIndex(1) // remove "two"
    if err != nil {
        t.Fatalf("deleteByIndex failed: %v", err)
    }
    if count != 1 {
        t.Errorf("expected to delete 1 message, got %d", count)
    }

    mb.mu.RLock()
    if len(mb.msgs) != 2 {
        t.Errorf("expected 2 messages after delete, got %d", len(mb.msgs))
    }
    if mb.msgs[1].Content != "three" {
        t.Errorf("wrong message at index 1 after delete")
    }
    mb.mu.RUnlock()
}

func TestMailbox_EditByIndex(t *testing.T) {
    mb := &mailbox{}
    mb.append("test", sdk.Message{ID: "1", Content: "one"})
    
    err := mb.editByIndex(0, "TWO")
    if err != nil {
        t.Fatalf("editByIndex failed: %v", err)
    }

    mb.mu.RLock()
    if mb.msgs[0].Content != "TWO" {
        t.Errorf("edit failed: content=%q", mb.msgs[0].Content)
    }
    mb.mu.RUnlock()
}
```

### Integration Tests (package harness)

**File:** `modules/harness/queue_test.go` (new)

```go
func TestQueueCommand_Basic(t *testing.T) {
    // 1. Spawn agent
    lm := testutil.NewFakeLM()
    pool := agent.NewPool()
    _, err := pool.Spawn("worker", lm, agent.SpawnOpts{...})
    
    // 2. Send multiple messages to worker inbox
    for i := 0; i < 3; i++ {
        pool.SendMessage("worker", sdk.Message{...})
    }
    
    // 3. Trigger /queue worker
    cmd, ok := registry.Get("queue")
    
    // 4. Verify output contains queued count and previews
}

func TestQueueCommand_AllAgents(t *testing.T) {
    // Similar to above, but verify all agents with non-zero inbox are shown
}

func TestMailboxHost_DeleteWhileRunning(t *testing.T) {
    // Verify delete/edit returns error when agent is running
}
```

### WASM Extension Tests

**File:** `extensions/agents/queue_test.go` (new)

```go
func TestWASM_MailboxSnapshot(t *testing.T) {
    // 1. Register test agent via host
    // 2. Append inbox messages manually
    // 3. Call mailbox_snapshot via host_call
    // 4. Verify returned messages match expected structure
}

func TestWASM_MailboxDeleteByIdempotent(t *testing.T) {
    // Deleting a non-existent message should be safe (return success=false)
}
```

---

## 8. Implementation Plan

### Phase 1: Infrastructure (Pre-requirements)

1. **Add `ID` field to `sdk.Message`**
   - Generate unique IDs on `mailbox.append`
   - Format: `<agent_id>_<seq>`
   - Update tests

2. **Implement `mailbox` helper methods**
   - `snapshot() []sdk.Message`
   - `deleteByIndex(idx int) (int, error)`
   - `editByIndex(idx int, content string) error`
   - Thread-safe with existing mutex

3. **Update AgentBridge interface**
   - Add `SnapshotInbox`, `DeleteFromInbox`, `EditInboxMessage`
   - Update all implementations (pool.go, test bridge)

### Phase 2: CLI & UI

4. **Implement `/queue` command**
   - Output formats (tabular, JSON)
   - Flag parsing
   - Single vs all agent views

5. **Statusline indicator**
   - Badge rendering logic
   - Event listeners (`agent_deliver`)

### Phase 3: Extensions & Documentation

6. **WASM SDK wrappers**
   - `MailboxSnapshot`
   - `MailboxDelete`
   - `MailboxEdit`

7. **Documentation**
   - `/queue` command reference
   - Mailbox API guide for extension authors
   - Examples (redaction, cleanup, debugging workflows)

---

## 9. Edge Cases & Failure Modes (Detailed)

### Queue Overflow Protection

**Problem:** Infinite inbox growth (memory exhaustion, UI sluggishness)

**Solution:**

```go
const (
    maxInboxSize = 10_000 // soft limit
)

func (b *mailbox) append(agentID string, msg sdk.Message) {
    if strings.TrimSpace(msg.Content) == "" {
        slog.Warn("agent: dropping inbox message with empty content", ...)
        return
    }
    
    b.mu.Lock()
    defer b.mu.Unlock()
    
    if len(b.msgs) >= maxInboxSize {
        // Log warning, drop oldest message
        slog.Warn("agent: inbox overflow, dropping oldest", "agent", agentID)
        b.msgs = append([]sdk.Message{msg}, b.msgs[:len(b.msgs)-1]...)
        return
    }
    
    b.msgs = append(b.msgs, msg)
}
```

### Message Expiry

**Problem:** Stale messages accumulate (e.g., pending tasks from old workflows)

**Proposal (Phase 2):**

```go
type Message struct {
    ID        string      `json:"id,omitempty"`
    Role      Role        `json:"role"`
    Content   string      `json:"content"`
    Type      MessageType `json:"type,omitempty"`
    CreatedAt time.Time   `json:"created_at,omitempty"` // NEW
}

// Add optional TTL to mailbox (configurable)
type MailboxConfig struct {
    MaxAge time.Duration // e.g., 1 hour
}

func (b *mailbox) appendWithTTL(agentID string, msg sdk.Message, ttl time.Duration) {
    // Set CreatedAt on first append
    if msg.CreatedAt.IsZero() {
        msg.CreatedAt = time.Now()
    }
    
    // Call regular append
    b.append(agentID, msg)
}
```

### Concurrent Edit/Delete During Active Turn

**Problem:** User tries to edit/delete while agent is processing

**Solution:**

```go
func (a *Agent) EditInboxMessage(index int, newContent string) error {
    if a.IsRunning() {
        return fmt.Errorf("cannot edit inbox while agent is running")
    }
    
    if strings.TrimSpace(newContent) == "" {
        return fmt.Errorf("content must be non-empty")
    }
    
    // ... mailbox.editByIndex
}
```

---

## 10. Related Issues & Precedents

| Issue | Relevance |
|-------|-----------|
| Drain-until-empty pattern (NOTES.md §17) | Inbox mechanics foundation |
| Graceful shutdown protocol (SPECS.md §14) | System message handling |
| Inbox ordering (NOTES.md §16) | Message position matters for editing |
| Sub-agent idle notification (SPECS.md §15) | Statusline integration |

---

## 11. Open Questions

| Question | Decision needed |
|----------|-----------------|
| Should edit/delete require agent idle? | Yes (simpler, safer) |
| Should snapshot return `[]sdk.Message` or a copy? | Copy (defensive) |
| Should inbox have a max size limit? | Yes, 10k messages (soft) |
| Should message IDs be user-specified or auto-generated? | Auto-generated (sequence-based) |
| Should /queue support filtering by message type? | Phase 2 (not MVP) |

---

## 12. References

- **Agent package SPECS.md:** `modules/agent/SPECS.md` (inbox ordering, drain-until-empty)
- **Agent package NOTES.md:** `modules/agent/NOTES.md` ( §17 drain pattern)
- **SDK message type:** `modules/sdk/types.go`
- **Current inbox implementation:** `modules/agent/mailbox.go`
- **Deliver primitive:** `modules/agent/pool.go` → `Deliver(id, msg, wake)`
- **Inbox ordering test:** `modules/agent/inbox_ordering_test.go`
- **AgentBridge interface:** `modules/extension/interfaces.go`

---

**Brainstorm complete.** Next step: draft implementation plan for `/queue` command and mailbox host API.
