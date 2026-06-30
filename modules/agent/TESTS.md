# agent — Test Specifications

## Existing Tests

### agent_test.go

| Test | Scenario | Setup | Assertions |
|------|----------|-------|------------|
| `TestAgent_AppendInbox_MessagesDeliveredBeforeNextTurn` | Inbox drains into next turn | Spawn agent, AppendInbox, Submit | Inbox messages present in turn context |
| `TestAgent_Submit_ConcurrentCallQueuesContent` | Concurrent Submit queues safely | Slow LM + concurrent Submit | Second Submit queues; processed after first |

### inbox_ordering_test.go

| Test | Scenario | Setup | Assertions |
|------|----------|-------|------------|
| `TestInboxMessages_AppendedAfterPriorHistory` | Inbox appended after prior history | Agent with history + inbox | Last message is inbox message |
| `TestInboxMessages_EmptyPromptValidAfterAppend` | Empty prompt valid when inbox non-empty | Prior assistant history + inbox msg | onDone receives nil error |

### concurrent_submit_test.go

| Test | Scenario | Setup | Assertions |
|------|----------|-------|------------|
| `TestSubmit_ConcurrentCallQueuesContent` | Second Submit queues while first runs | Slow LM holds turn | Second Submit returns immediately; content queued |
| `TestSubmit_ConcurrentCall_HistoryNotCorrupted` | History integrity under concurrency | Two concurrent Submits | Histories non-overlapping; no data race |

### spawner_test.go

| Test | Scenario | Setup | Assertions |
|------|----------|-------|------------|
| `TestSpawner_Spawn_Basic` | Basic sub-agent spawn | Pool + Spawner | Agent exists in pool with correct name |
| `TestSpawner_Spawn_ParentIDConvention` | Parent ID derived from scoped ID | Various ID formats | parentID extracted correctly |
| `TestSpawner_Spawn_AgentIdentitySuffix` | System prompt gets identity suffix | Spawn with base prompt | Agent spawned (suffix verified via pool.Get) |
| `TestSpawner_Spawn_UnknownModel` | Unknown model returns error | Non-existent model name | Error returned |

### deliver_test.go

| Test | Scenario | Setup | Assertions |
|------|----------|-------|------------|
| `TestDeliver_WakesIdleAgent` | `Deliver(wake=true)` queues AND processes | Spawn idle agent, Deliver | Delivered message appears in history (turn ran) |
| `TestDeliver_NoWakeQueuesOnly` | `Deliver(wake=false)` queues without a turn | Spawn agent, Deliver wake=false | onDone never fires; `InboxLen()==1` |
| `TestDeliver_EmptyContentRejected` | Empty content guard | Deliver whitespace content | Error returned |
| `TestDeliver_UnknownAgent` | Unknown ID | Deliver to ghost ID | `ErrAgentNotFound` |
| `TestDeliver_WakeNotifierFires` | Wake notifier callback | SetWakeNotifier, Deliver wake=true | Notifier called once with the agent ID |
| `TestIdleNotification_WakesCreator` | Sub-agent idle notifies creator | Worker with creatorID=main, run a turn | Creator woken; creator history contains `is idle` + worker ID |
| `TestIdleNotification_TopLevelAgentDoesNotSelfNotify` | main never self-notifies | Spawn main (no creator), run a turn | main inbox empty after turn (no loop) |

## Missing / Recommended Tests

| Priority | Test | Scenario | Assertions |
|----------|------|----------|------------|
| HIGH | `TestSpawner_Spawn_ThinkingBudget` | ThinkingBudget > 0 applies provider options | ProviderOptions set on spawned agent |
| HIGH | `TestSpawner_Spawn_InitialPrompt` | InitialPrompt starts first turn | pool.Send called after spawn |
| MEDIUM | `TestAgent_Cancel_StopsActiveGoroutine` | Cancel during active turn | Turn goroutine exits; onDone called with cancel error |
| MEDIUM | `TestPool_CancelAll_StopsAllAgents` | CancelAll cancels every agent | All active turns cancelled |
