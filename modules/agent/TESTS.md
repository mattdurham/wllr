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

### providerintercept_test.go

| Test | Scenario | Setup | Assertions |
|------|----------|-------|------------|
| `TestProviderIntercept_BlockFailsTurn` | Interceptor blocks the request | interceptor returns block+reason | turn errors with `*ProviderRequestBlockedError`; reason preserved |
| `TestProviderIntercept_NoInterceptorTurnSucceeds` | No interceptor = unchanged path | no interceptor set | turn completes normally |
| `TestProviderIntercept_RedactPreservesHistoryOriginal` | Send-time redaction | interceptor rewrites all message content | turn completes; history keeps ORIGINAL content; redacted text never in history |
| `TestProviderIntercept_RerouteRequestsNewModel` | Model reroute | interceptor returns a new model; recordingProvider | provider asked for the rerouted model ID |

### mailbox_test.go

| Test | Scenario | Setup | Assertions |
|------|----------|-------|------------|
| `TestMailbox_AppendDrainLen` | Basic queue ops | zero-value mailbox | FIFO drain; len tracks state; second drain returns nil |
| `TestMailbox_DropsEmptyContent` | Empty-content guard | append blank/whitespace then real | only non-blank message queued |
| `TestMailbox_ConcurrentAppendDrain` | Race safety | 8 writers ×100, 3 drainers, `-race` | every message drained exactly once; no race |

### placeholder_test.go

| Test | Scenario | Setup | Assertions |
|------|----------|-------|------------|
| `TestPlaceholderForEmptyResponse` | Empty-response placeholder selection | 4 cases (text/empty × cancelled) | non-empty passes through; empty→tool-only; empty+cancelled→cancelled |

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
| `TestDeliver_WhileRunning_DrainsAfterTurn` | Deliver lands mid-turn (gated LM) | Start gated turn, Deliver while running | Queued not started; drained after turn; both messages in history; single onDone |
| `TestDeliver_ShutdownRequestToIdleAgent` | Regression: shutdown to idle agent | Deliver shutdown_request to idle worker | Worker self-closes; creator gets AGENT_SHUTDOWN, no idle notice; clean (no error) |
| `TestIdleNotification_WakesCreator` | Sub-agent idle notifies creator | Worker with creatorID=main, run a turn | Creator woken; creator history contains `is idle` + worker ID |
| `TestIdleNotification_TopLevelAgentDoesNotSelfNotify` | main never self-notifies | Spawn main (no creator), run a turn | main inbox empty after turn (no loop) |
| `TestIdleNotification_SuppressedDuringShutdown` | Shutdown path suppresses idle notice | Deliver shutdown_request, run worker | Creator gets exactly 1 AGENT_SHUTDOWN, 0 idle notices |
| `TestIdleNotification_MultipleWorkersCoalesce` | N workers idle-notify shared creator | 5 workers each run + go idle | Each worker's idle notice in creator history exactly once (no loss/dup) |

## Missing / Recommended Tests

| Priority | Test | Scenario | Assertions |
|----------|------|----------|------------|
| HIGH | `TestSpawner_Spawn_ThinkingBudget` | ThinkingBudget > 0 applies provider options | ProviderOptions set on spawned agent |
| HIGH | `TestSpawner_Spawn_InitialPrompt` | InitialPrompt starts first turn | pool.Send called after spawn |
| MEDIUM | `TestAgent_Cancel_StopsActiveGoroutine` | Cancel during active turn | Turn goroutine exits; onDone called with cancel error |
| MEDIUM | `TestPool_CancelAll_StopsAllAgents` | CancelAll cancels every agent | All active turns cancelled |
