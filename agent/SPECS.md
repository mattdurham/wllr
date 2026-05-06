# agent — Interface Contracts and Behavioral Invariants

Package `agent` manages sub-agents and teams for the bob harness. Each `Agent` wraps a
`fantasy.LanguageModel` run loop with a message inbox and a shared `AgentPool`.

---

## 1. AgentPool Thread-Safety Contract

`AgentPool` is safe for concurrent use from multiple goroutines.

- All mutations to `agents` and `teams` maps are performed under `p.mu.Lock()`.
- All reads from `agents` and `teams` maps are performed under `p.mu.RLock()`.
- The `tokenCount` field is updated atomically via `sync/atomic`.
- The `providerName` field is read/written under `p.mu` (not a separate lock) to stay consistent with agent/team reads.
- Individual `Agent` fields (inbox, cancel, history, onToken, onDone) carry their own per-field mutexes. Callers never need to hold pool-level locks when calling agent methods.

**Invariant:** No pool operation blocks on an in-progress agent turn. Pool operations that call `a.Cancel()` release `p.mu` before invoking Cancel to avoid lock ordering issues.

---

## 2. Agent ID Uniqueness and Lifecycle

**Invariant:** Agent IDs are unique within a pool. `Spawn` returns `ErrAgentExists` if an agent with the same ID is already registered. There is no implicit eviction — the caller must call `pool.Close(id)` before re-using an ID.

An Agent transitions through three observable states:

```
idle ──(Submit called)──▶ running ──(turn complete)──▶ idle
                                                          │
                                     (pool.Close called)──▶ closed
```

- **idle**: no active goroutine. `Submit` may be called.
- **running**: `Submit`'s goroutine is active. `Cancel` may be called.
- **closed**: the agent has been removed from the pool via `pool.Close`. Further `Submit` calls are possible but the language model field `lm` may be nil.

**Invariant:** Only one goroutine runs per agent turn. `Submit` replaces the stored cancel function atomically before launching the goroutine. Callers are responsible for not calling `Submit` concurrently.

---

## 3. Message Queue (Inbox) Ordering

`AppendInbox` enqueues messages for delivery before the next turn. `DrainInbox` atomically retrieves and clears all queued messages. `Submit` calls `DrainInbox` at the start of each turn and prepends inbox messages to the conversation history.

**Invariant:** Inbox messages are delivered in FIFO order. Messages appended before `Submit` is called are guaranteed to be visible within that turn. Messages appended after `Submit` has called `DrainInbox` will appear in the next turn.

**Invariant:** `DrainInbox` is atomic — no message is lost between `AppendInbox` and `DrainInbox` regardless of concurrent calls. This is guaranteed by the `inboxMu` mutex.

---

## 4. Token Counter Atomicity

`AgentPool.tokenCount` is an `atomic.Int64`. It is incremented by one per text token emitted by any agent in the pool.

- `addTokens(n)` is called from agent goroutines via the pool pointer captured at spawn time.
- `TokenCount()` returns the current snapshot and is non-blocking.

**Invariant:** The counter is monotonically increasing and never resets within a pool's lifetime. No agent goroutine reads the counter; it is write-only from agent goroutines.

**Invariant:** `AddTokens` is the exported equivalent of `addTokens` and increments by exactly `n`. Both use `atomic.Int64.Add`, which is atomic on all supported architectures.

---

## 5. Team Invariants

A `Team` is a lightweight membership set — it does not own goroutines or resources beyond the member ID set.

**Invariant:** Team IDs are unique within a pool. `CreateTeam` returns `ErrTeamExists` if a team with the same ID is already registered.

**Invariant:** Team membership is independent of agent lifecycle. `RemoveMember` does NOT close or cancel the agent — it only removes the agent ID from the team's member set.

- `Team.AddMember(agentID)` returns `ErrAgentNotFound` if the agent is not registered in the pool at call time.
- `Team.RemoveMember(agentID)` is a no-op if the agent is not a member. It does NOT close or cancel the agent.
- `Team.Close(ctx)` cancels all member agents via `pool.Close` and clears the member set. Agents not found in the pool (already closed) are silently skipped.
- `AgentPool.CloseTeam(id)` removes the team from `p.teams` before calling `t.Close`, preventing double-close races.

**Invariant:** Team membership is guarded by `Team.mu` (separate from the pool-level `p.mu`). Reads and writes to `members` always hold `Team.mu` in the appropriate mode.

**Invariant:** A team can only contain agent IDs that were valid at the time of `AddMember`. The team does not track agent lifecycle events; an agent closed after `AddMember` will produce `ErrAgentNotFound` from `pool.Close` during `Team.Close`, which is silently ignored.

---

## 6. Pool.Send vs. Pool.SendMessage

`AgentPool` provides two message delivery methods:

- `SendMessage(id, msg sdk.Message)` appends `msg` to the agent's inbox. The agent's next `Submit` call will deliver it as prior context. Non-blocking.
- `Send(id, content string)` calls `agent.Submit(context.Background(), content)`, which starts a new turn immediately (non-blocking goroutine). The turn drains the inbox first.

**Invariant:** `Send` always returns immediately. The agent goroutine may be running concurrently with the caller.

---

## 7. Cancel Semantics

`Agent.Cancel()` cancels the active turn's context. It is a no-op if no turn is running. `AgentPool.Cancel(id)` is the pool-level equivalent.

**Invariant:** `Cancel` is idempotent. Calling `Cancel` on an agent with no active turn (including an agent that has already been closed via `pool.Close`) is a no-op. The stored cancel function is checked for nil before invocation.

**Invariant:** `Cancel` does not remove the agent from the pool. The agent remains registered and may be re-submitted.

**Invariant:** After `Cancel`, the agent's `onDone` callback is invoked with `context.Canceled` as the error. The conversation history records the partial assistant turn only if text was collected before cancellation.

---

## 8. AgentPool.ProviderName

`SetProviderName(name string)` stores a human-readable display string for the configured provider. `ProviderName()` reads it. Both are guarded by `p.mu`.

**Invariant:** `ProviderName` returns the empty string if `SetProviderName` has not been called. The harness `New()` reads this value at construction time to initialize the status bar.
