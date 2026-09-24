# agent — Test Specifications

Sub-agent spawn tests verify that model requests, including an optional endpoint,
reach the pool's model factory. Command-layer tests verify that configured local
models select their own endpoint and credentials and reject unknown models or
mismatched endpoint overrides.

## Existing Tests

### agent_test.go

| Test | Scenario | Setup | Assertions |
|------|----------|-------|------------|
| `TestAgent_AppendInbox_MessagesDeliveredBeforeNextTurn` | Inbox drains into next turn | Spawn agent, AppendInbox, Submit | Inbox messages present in turn context |
| `TestAgent_Submit_NotifiesTurnStart` | Turn claims inbox messages | Spawn agent, AppendInbox, SetOnTurnStart, Submit | callback receives the queued message before completion |
| `TestAgent_Submit_ConcurrentCallQueuesContent` | Concurrent Submit queues safely | Slow LM + concurrent Submit | Second Submit queues; snapshot exposes it while first runs; processed after first |

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

### subagent_test.go (model switching)

| Test | Scenario | Assertions |
|------|----------|-----------|
| `TestSpawnOpts_ModelName_OverridesPoolDefault` | Spawn with explicit model | `ModelName()` reflects override |
| `TestSpawnOpts_ModelName_Empty_UsesPoolDefault` | Spawn with empty model | `ModelName()` is pool default |
| `TestSetModel_SwapsModelForNextTurn` | `SetModel` at runtime (/model picker path) | `ModelName()` updates; next turn streams from the swapped LM |
| `TestSetProviderOptions_AppliedToNextTurn` | `SetProviderOptions` at runtime (/thinking picker path) | Options absent on turn 1; present on turn 2 after set; cleared on turn 3 after nil |

### concurrent_submit_test.go

| Test | Scenario | Setup | Assertions |
|------|----------|-------|------------|
| `TestSubmit_ConcurrentCallQueuesContent` | Second Submit queues while first runs | Slow LM holds turn | Second Submit returns immediately; SnapshotInbox exposes content while running; content is processed |
| `TestSubmit_ConcurrentCall_HistoryNotCorrupted` | History integrity under concurrency | Two concurrent Submits | Histories non-overlapping; no data race |

### spawner_test.go

| Test | Scenario | Setup | Assertions |
|------|----------|-------|------------|
| `TestSpawner_Spawn_Basic` | Basic sub-agent spawn | Pool + Spawner | Agent exists in pool with correct name |
| `TestSpawner_Spawn_ParentIDConvention` | Parent ID derived from scoped ID | Various ID formats | parentID extracted correctly |
| `TestSpawner_Spawn_AgentIdentitySuffix` | System prompt gets identity suffix | Spawn with base prompt | Agent spawned (suffix verified via pool.Get) |
| `TestSpawner_Spawn_UnknownModel` | Unknown model returns error | Non-existent model name | Error returned |

`Spawner.SetToolCallObserver` is covered indirectly by harness tool activity tests; the nil-observer path is exercised by existing spawner tests.

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
| `TestIdleNotification_WakesCreator` | Structured lifecycle protocol | Child idles with creator | Creator receives `agent_idle` JSON containing child and creator IDs and is woken |
| `TestSpawner_FailureNotificationTargetsCreator` | Nested child failure | Child with non-main creator fails | Creator receives `agent_failed` JSON with error, not hard-coded `main` |
| `TestIdleNotification_SuppressedDuringShutdown`, `TestDeliver_ShutdownRequestToIdleAgent` | Shutdown acknowledgement | Child processes shutdown request | Creator receives `AGENT_SHUTDOWN` through wake-enabled delivery |

### toolloopcompaction_test.go

| Test | Scenario | Setup | Assertions |
|------|----------|-------|------------|
| `TestStreamTurnCompactsGrowingToolTranscript` | Real Fantasy tool loop approaches its context window | Mock provider reports 190k input tokens, tool returns a large result | One bounded summary call runs; next provider call receives the summary without the large tool result |
| `TestToolLoopCompactorSummarizesBeforeNextStep` | Tool output grows the active prompt beyond the threshold | Provider usage near 80%, large new tool result | Next step receives a summary; following step retains later result without summarizing again |
| `TestToolLoopCompactorStopsOnSummaryFailure` | Summarizer returns no text | Usage above threshold | PrepareStep returns an explicit error instead of sending an oversized request |
| `TestToolLoopCompactorUsesMessageSizeWhenProviderOmitsUsage` | Compatible endpoint reports zero usage | Large provider-facing prompt | Bounded message-size estimate still triggers compaction |

### compactionobs_test.go

| Test | Scenario | Setup | Assertions |
|------|----------|-------|------------|
| `TestCompactHistory_UsageSurfaced` | Summarization cost is observable | `compactTestLM` emitting fixed usage; 212×400-char history over budget | result carries the summarize call's input/output tokens, trigger, and messages_compacted; no-op run reports zero usage and empty summary |
| `TestObserveCompaction_Summary_IncrementsCounter` | Per-session counter counts real compactions | agent + successful `compactHistory` | `CompactionCount()` increments once per observed summary |
| `TestObserveCompaction_NoOp_DoesNotCount` | No-op compactions increment nothing | agent + no-op `compactHistory` (fits budget) | counter stays 0 |
| `TestExecuteTurn_CompactionCounterIncrementsAndDispatches` | Full-turn observability wiring | main agent, 200k window, seeded over-budget history + usage above 0.80 threshold | turn completes, counter = 1, dispatcher sees `compacted=true` and `compactions=1` |

### activity_test.go

| Test | Scenario | Setup | Assertions |
|------|----------|-------|------------|
| `TestAgentActivity_TracksRunningTurnAndToolCall` | Activity snapshot records intra-turn work | Fake LM emits text and a tool call | turn/activity/tool timestamps are set; last tool name recorded; active tool clears after turn |
| `TestAgentActivity_ToolCompletionClearsActiveTool` | Tool completion updates liveness | Internal agent activity state with active tool | completion clears active tool/call and records last-tool-done timestamp |
| `TestAgentActivity_ShutdownRequestedWhileRunning` | Graceful shutdown request is visible before stop | Gated running agent receives shutdown_request | `Activity().ShutdownRequested` is true while request is queued |

## Missing / Recommended Tests

| Priority | Test | Scenario | Assertions |
|----------|------|----------|------------|
| HIGH | `TestSpawner_Spawn_ThinkingBudget` | ThinkingBudget > 0 applies provider options | ProviderOptions set on spawned agent |
| HIGH | `TestSpawner_Spawn_InitialPrompt` | InitialPrompt starts first turn | pool.Send called after spawn |
| MEDIUM | `TestAgent_Cancel_StopsActiveGoroutine` | Cancel during active turn | Turn goroutine exits; onDone called with cancel error |
| MEDIUM | `TestPool_CancelAll_StopsAllAgents` | CancelAll cancels every agent | All active turns cancelled |

## Usage and lifecycle observers (usage_observer_test.go)

| Level | Test | Scenario | Assertion |
|-------|------|----------|-----------|
| High | `TestUsageObserverReportsTurn` | one successful turn | a start signal then a completion with agent, model, and token counts |
| High | `TestUsageObserverReportsFailure` | a turn that errors | reported as a turn with `Err` set and no tokens attributed |
| High | `TestUsageObserverCoversSubagents` | main + sub-agent turns | the observer fires for both, enabling per-agent accounting |
| Medium | `TestLifecycleObserverReportsSpawnAndClose` | spawn then close | reports the added and removed agent with the post-change live count |

| High | `TestSpawnerTokenObserverCarriesAgentID` | a spawned sub-agent streams text | the observer fires with the producing agent's ID, enabling live focused views |

| High | `TestSpawnDoesNotBlockOnTurnStartCallback` | spawn while the caller holds a lock the turn-start callback needs | Spawn returns instead of invoking the callback on the spawning stack (regression for the create_agent deadlock) |

## Canonical transcript and recall

### transcript_test.go

| Test | Scenario | Setup | Assertions |
|------|----------|-------|------------|
| `TestTranscript_RecordMessage_AssignsStableSequentialIDs` | ID scheme is stable and ordered | record user/assistant/user | IDs are `u1`, `a2`, `u3`; `Len` is 3 |
| `TestTranscript_IDsAreUniqueAcrossKinds` | source pointers never collide | record message, tool call, tool result, message | every ID non-empty and unique |
| `TestTranscript_RecordMessage_RejectsEmpty` | empty content is not a record | record `""`, `"   "`, `"\n\t"` | empty ID returned; `Len` stays 0 |
| `TestTranscript_RecordToolCall_KeepsEmptyInput` | a call with no args still happened | record tool call with empty input | entry recorded |
| `TestTranscript_SnapshotIsChronologicalAndCopied` | snapshot cannot corrupt storage | record two entries, mutate the snapshot | order preserved; stored entry unchanged by the mutation |
| `TestTranscript_SearchByTextIsCaseInsensitiveSubstring` | primary retrieval mode | mixed-case content, lowercase query | one match, correct entry |
| `TestTranscript_SearchByToolMatchesOnlyThatTool` | tool filter isolates calls/results | two tools plus a message | only that tool's entries; messages excluded |
| `TestTranscript_SearchByPath` | path-oriented retrieval | two edit calls with different paths | only the matching path |
| `TestTranscript_SearchByRangeIsInclusive` | range bounds are inclusive | three entries, `from=2,to=3` | seqs 2 and 3 returned |
| `TestTranscript_SearchRangeOnly` | range alone is a valid query | five entries, `from=4` | the last two |
| `TestTranscript_SearchLimitKeepsTotal` | truncation is reportable | ten matches, `limit=3` | 3 returned, `total` still 10 |
| `TestTranscript_SearchNoMatchReturnsNilAndZero` | miss is not an error | unrelated query | nil matches, total 0 |
| `TestTranscript_NilTranscriptIsSafe` | zero-value robustness | nil `*Transcript` | all methods safe, no panic |
| `TestRenderRecall_RespectsTokenBudgetExactly` | hard output bound | 20 large tool results, 500-token budget | `len(out) <= budget*4` |
| `TestRenderRecall_IncludesSourcePointers` | entries are citable | tool call + result | output contains IDs, kind headers, and content |
| `TestRenderRecall_StatesProvenance` | model knows the material predates its summary | one message | output states canonical/pre-compaction provenance |
| `TestRenderRecall_NoMatchesIsActionable` | miss gives recovery guidance | empty matches | actionable text, within budget |
| `TestRenderRecall_TruncatesSingleOversizeEntry` | oversize entry stays retrievable | one 50k-char result, 200-token budget | entry present, truncated, within budget |
| `TestRenderRecall_SignalsTruncatedMatches` | partial results are labelled | 50 matches, 300-token budget | output says more matched |
| `TestRecallBudgetForWindow` | budget derivation | 0, -1, 1M, 4k, 2k windows | default, default, cap, scaled, floor |
| `TestRenderRecall_NonPositiveBudgetUsesDefault` | zero budget is not unbounded | budgets 0 and -1 | output within the default budget |

### recall_test.go

| Test | Scenario | Setup | Assertions |
|------|----------|-------|------------|
| `TestRecallTool_InfoShape` | schema contract | agent recall tool | name `recall`; all six filters present; `Required` empty |
| `TestRecallTool_Run_FindsExactPreCompactionDetail` | the core retrieval case | seeded transcript, query `port 5433` | exact output text plus source pointer returned |
| `TestRecallTool_Run_FilterByTool` | filter by tool | seeded transcript, `tool=exec` | call and result returned; message excluded |
| `TestRecallTool_Run_FilterByPath` | filter by path | extra edit call | matching path returned |
| `TestRecallTool_Run_MessageRange` | range retrieval | `from=2,to=4` | seqs 2–4 returned; seq 1 excluded |
| `TestRecallTool_Run_RequiresAtLeastOneFilter` | no unfiltered dumps | `{}`, blank query | error response naming the requirement |
| `TestRecallTool_Run_RejectsMalformedInput` | input validation | truncated JSON | error response naming invalid JSON |
| `TestRecallTool_Run_RejectsInvertedRange` | input validation | `from=9,to=2` | error response naming the range rule |
| `TestRecallTool_Run_RejectsNegativeBounds` | input validation | `from=-1` | error response |
| `TestRecallTool_Run_EmptyTranscriptIsNotAnError` | empty state is legitimate | agent with no entries | readable result, not an error |
| `TestRecallTool_Run_BoundedByContextWindow` | window scales the budget | 200 large entries, 2k vs 1M windows | both within budget; small returns strictly less |
| `TestRecallTool_ReadsTranscriptLazily` | tool built before the transcript | construct tool, record later | sees entries recorded afterwards |
| `TestRecallTool_NilAgentIsReportedNotPanicked` | defensive | tool with no agent | error response, no panic |

### canonical_test.go

| Test | Scenario | Setup | Assertions |
|------|----------|-------|------------|
| `TestExecuteTurn_CompactionPreservesCanonicalTranscript` | **acceptance:** compaction must not destroy the transcript | seeded transcript, 212-message history over budget, usage above threshold | compaction ran; detail gone from `History()`; transcript intact; recall still returns the exact detail |
| `TestExecuteTurn_CanonicalTranscriptDoesNotEnterHistory` | transcript is not compaction input | transcript entry then a turn | marker never appears in `History()` |
| `TestExecuteTurn_RecordsMessagesInCanonicalTranscript` | both sides of a turn are recorded | one turn | two entries: user then assistant, correct kinds/roles |
| `TestExecuteTurn_RecordsToolCallAndResultInCanonicalTranscript` | **command output is retrievable** | scripted client-side tool call returning a failure | call and result entries recorded; result text exact; findable by tool |
| `TestCanonicalTranscript_SurvivesAcrossTurns` | append-only across turns | two turns | two new entries; earlier turn still retrievable |
| `TestCanonicalTranscript_ExcludesSystemMessages` | control traffic is not conversation | inbox mixing a system and a protocol message, empty-prompt drain turn | system content absent; something else recorded (test is not vacuous) |
