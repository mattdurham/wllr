# agent — Interface Contracts and Behavioral Invariants

Package `agent` manages sub-agents and teams for the bob harness. Each `Agent` wraps a
`fantasy.LanguageModel` run loop with a message inbox and a shared `AgentPool`.

---

## 1. AgentPool Thread-Safety Contract

`AgentPool` is safe for concurrent use from multiple goroutines.

- All mutations to `agents` and `teams` maps are performed under `p.mu.Lock()`.
- All reads from `agents` and `teams` maps are performed under `p.mu.RLock()`.
- The `tokenCount` field is updated atomically via `sync/atomic`.
- The `providerName` and `defaultModelName` fields are read/written under `p.mu`.
- The `baseSystemPrompt` field has its own `baseSystemPromptMu sync.RWMutex`, separate from the main `mu`, because it can be updated independently without touching agent/team maps.
- Individual `Agent` fields (inbox, cancel, history, onToken, onDone, onToolCall, onTurnStart, toolsFn, systemPrompt) carry their own per-field mutexes. Callers never need to hold pool-level locks when calling agent methods.

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

**Invariant:** Only one goroutine runs per agent turn. `Submit` replaces the stored cancel function atomically before launching the goroutine. Concurrent `Submit` calls are safe: if a turn is already running, the new content is queued to the inbox and the running goroutine drains it on completion (drain-until-empty pattern). See NOTES.md §17.

`Agent.Activity()` is a read-only liveness snapshot. During a running turn,
activity is considered proof of work rather than proof of completion: callers
must treat the agent as working until the turn ends, cancellation is observed,
or a future dead-agent detector reports a dead state. Tool starts set
`ActiveToolName`/`ActiveToolCallID`; matching tool completions clear those
active fields and update `LastToolDoneAt`, while preserving `LastToolName` as
the most recent tool identity.

---

## 3. Message Queue (Inbox) Ordering

The inbox is the unexported `mailbox` type (`mailbox.go`): it owns the message slice and the `sync.RWMutex` guarding it, and is embedded by value in `*Agent` as the `inbox` field. The `Agent.AppendInbox`/`DrainInbox`/`InboxLen` methods are thin forwarders to `mailbox.append`/`drain`/`len`. The mailbox is the **message store only** — it does not own turn-execution state (`isRunning`), which remains the Agent's concern.

`AppendInbox` enqueues messages for delivery before the next turn. `DrainInbox` atomically retrieves and clears all queued messages. `Submit` calls `DrainInbox` at the start of each turn and **appends** inbox messages after the conversation history (inbox messages appear after prior history, making them the most-recent messages visible to the LLM). See NOTES.md §16.

**Invariant:** Inbox messages are delivered in FIFO order and always appear as the most recent messages in the prompt. Messages appended before `Submit` is called are guaranteed to be visible within that turn. Messages appended after `Submit` has called `DrainInbox` will appear in the next turn.

`SetOnTurnStart` registers an optional callback for UI or persistence integrations. It receives the explicit turn content and a copy of the inbox messages after `Submit` successfully claims them and before the provider request starts. Concurrent `Submit` calls that re-queue their drained messages do not trigger the callback until a later turn actually claims them.

**Invariant:** `DrainInbox` is atomic — no message is lost between `AppendInbox` and `DrainInbox` regardless of concurrent calls. This is guaranteed by the mailbox's internal mutex.

**Invariant:** `mailbox.append` drops messages whose content is blank (empty or whitespace-only) and logs a warning, because the Anthropic API rejects empty text content blocks. This is the single enforcement point for the non-empty-content rule.

**Invariant:** the mailbox does not filter by `MessageType`. Filtering is done at `sdkToFantasyMessages` conversion time. `sdk.MessageTypeSystem` messages survive the inbox cycle intact but are never recorded in history (see §9 streamTurn) and never reach the LLM context.

---

## 4. Token Counter Atomicity

`AgentPool.tokenCount` is an `atomic.Int64`. It is incremented by one per text token emitted by any agent in the pool.

- `addTokens(n)` (unexported) is called from agent goroutines via the pool pointer captured at spawn time.
- `AddTokens(n)` (exported) is the publicly accessible equivalent, exposed for testing.
- `TokenCount()` returns the current snapshot and is non-blocking.

**Invariant:** The counter is monotonically increasing and never resets within a pool's lifetime.

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

`AgentPool.ListTeams()` returns a snapshot of all registered team IDs under `p.mu.RLock()`. The snapshot is consistent but not synchronized with concurrent `CreateTeam`/`CloseTeam` calls; callers must tolerate TOCTOU gaps.

`AgentPool.GetTeamMembers(teamID)` returns the member agent IDs for a team under `p.mu.RLock()` (to locate the team) and `Team.mu.RLock()` (for the membership snapshot). Returns `ErrTeamNotFound` if the team does not exist.

---

## 6. Pool.Send vs. Pool.SendMessage vs. Pool.Deliver

`AgentPool` provides three message delivery methods:

`EnsureMainAgent(ctx)` recreates the primary `main` agent from the configured
provider and default model when it is missing after a fatal model failure. It
is idempotent and does not recreate sub-agents.

- `SendMessage(id, msg sdk.Message)` appends `msg` to the agent's inbox. The agent's next `Submit` call will deliver it as prior context. Non-blocking. Does **not** start a turn.
- `Send(id, content string)` calls `agent.Submit(context.Background(), content)`, which starts a new turn immediately (non-blocking goroutine). The turn drains the inbox first.
- `Deliver(id, msg sdk.Message, wake bool)` is the **atomic deliver-and-process primitive**. It appends `msg` to the inbox and, when `wake` is true, calls `Submit(ctx, "")` so the message is processed immediately (or picked up by drain-until-empty if a turn is already running). It replaces the prior two-call `SendMessage` + `Send`/`Run` pattern at every call site. Returns `ErrAgentNotFound` for unknown IDs and an error for empty content.

`Agent.IsRunning()` and `Agent.InboxLen()` are non-mutating runtime status checks. Use
them for liveness/status reporting. Sending a "ping" message to a running agent only
queues that message for a later turn; it cannot be observed by the child until the active
turn finishes and the inbox is drained.

`AgentPool.SnapshotInbox` is a read-only status operation and remains available while an
agent is running. It returns the messages currently waiting in the inbox; it does not
include messages already drained into the active turn. Mutating inbox operations remain
blocked while a turn is running.

`Agent.Activity()` is a non-mutating runtime status snapshot for intra-turn liveness. It
reports turn start time, last activity time, last tool call time/name, currently active
tool name (best-effort; cleared when the turn finishes), and whether graceful shutdown has
been requested. Activity is updated when a turn starts, text streams, a tool call is
dispatched, a shutdown request is queued, and the turn finishes.

**Invariant:** `Send` and `Deliver` always return immediately. The agent goroutine may be running concurrently with the caller.

**Invariant:** `Deliver(id, msg, wake=true)` guarantees the message is *processed*, not merely queued. A delivered message can never be silently stranded in the inbox by a missing follow-up trigger — this is the failure mode the two-call pattern allowed (e.g. the tasks extension queued a `TASK_DONE` notification but never triggered a turn).

**Invariant:** `Deliver` Submits with empty content. The just-appended inbox message becomes the turn content via the drain path; no synthetic placeholder string (such as the former `"[process pending inbox messages]"`) is ever injected into history.

**Invariant:** `Activity()` is observational only. Reading activity must not enqueue messages, wake the agent, cancel the turn, or mutate history/inbox state.

### Wake Notifier

`SetWakeNotifier(fn func(id string))` installs a callback invoked with the agent ID whenever `Deliver` wakes that agent (`wake=true`). The harness uses it to drive the TUI streaming indicator for the main agent (sends `agentWakeupMsg`). The callback runs on the delivering goroutine (which may be a sub-agent's `finishTurn` goroutine), not the bubbletea loop — implementations must be goroutine-safe. Guarded by `dispatchMu` alongside `contextUsageDispatcher`.

---

## 7. Cancel Semantics

`Agent.Cancel()` cancels the active turn's context. It is a no-op if no turn is running. `AgentPool.Cancel(id)` is the pool-level equivalent.

`AgentPool.CancelAll()` cancels the active turn of every agent in the pool. It takes a snapshot of agents under `RLock`, then calls `Cancel()` on each without holding the lock. This prevents lock contention during batch cancellation.

**Invariant:** `Cancel` is idempotent. Calling `Cancel` on an agent with no active turn (including an agent that has already been closed via `pool.Close`) is a no-op. The stored cancel function is checked for nil before invocation.

**Invariant:** `Cancel` does not remove the agent from the pool. The agent remains registered and may be re-submitted.

---

## 8. System Prompt Hierarchy

Each agent has a two-level system prompt:

1. **Base prompt** (`a.systemPrompt`): set or appended via `SetSystemPrompt` / `AppendSystemPrompt`. Populated at spawn time from the pool's accumulated `baseSystemPrompt` (AGENTS.md, skills, etc.).
2. **Agent-specific prompt** (`opts.SystemPrompt`): set in `SpawnOpts` at spawn time and never changes.

On each `Submit` call, the resolved system prompt sent to the LLM is:

- `base + "\n\n" + specific` when both are non-empty
- `base` when only base is non-empty
- `specific` when only specific is non-empty (including when base is empty)

**Invariant:** `SetSystemPrompt` fully replaces the base prompt; `AppendSystemPrompt` appends with a `"\n\n"` separator. Neither modifies `opts.SystemPrompt`.

### Pool-Level Base System Prompt

`AgentPool` accumulates a base system prompt via `SetBaseSystemPrompt` and `AppendBaseSystemPrompt`. Changes are propagated immediately to all currently-registered agents by calling the corresponding method on each agent under `p.mu.RLock()`. Newly spawned agents inherit the current base prompt from the pool at spawn time.

**Invariant:** After `SetBaseSystemPrompt(prompt)` returns, every agent currently in the pool has its individual `systemPrompt` field equal to `prompt`.

**Invariant:** After `AppendBaseSystemPrompt(text)` returns, every agent currently in the pool has `text` appended to its individual `systemPrompt`.

---

## 9. Context Window and Compaction

Each `Agent` stores a `modelName string` (and its `lm fantasy.LanguageModel`) for context window lookup and streaming. Both are guarded by `lmMu sync.RWMutex` and may be swapped at runtime via `SetModel(lm, modelName)` (used by the `/model` picker). `ModelName()` reads under the lock. On each `Submit` call, the agent captures `lm` under `lmMu.RLock()` and, inside the turn, snapshots `modelName` once via `ModelName()`; `contextWindowForModel(modelName)` determines the model's known input context limit.

**Invariant:** `SetModel` is safe to call concurrently with turns. A turn already in flight finishes on the model it captured; the next `Submit` picks up the swapped model. `lm` and `modelName` are always read/written together under `lmMu`.

`Agent` also stores `providerOpts fantasy.ProviderOptions` (the provider-specific request options, e.g. extended-thinking budget / reasoning effort), seeded from `opts.ProviderOptions` at spawn and guarded by the same `lmMu`. `SetProviderOptions(po)` swaps them at runtime (used by the `/thinking` picker); a nil value clears them (thinking off). `Submit` snapshots `providerOpts` under `lmMu.RLock()` alongside `lm` and overrides `opts.ProviderOptions` with the snapshot for that turn.

**Invariant:** `SetProviderOptions` is safe to call concurrently with turns; a turn in flight finishes on the options it captured, the next `Submit` picks up the swap. `providerOpts` is always read/written under `lmMu`.

Token estimation uses the `chars/4` heuristic (`estimateTokens`, `estimateStr`).
`contextWindowForModel` returns the model's known window from the generated
catalog table (`models.generated.go`, substring match) and falls back to
`defaultContextWindow` (1,000,000) for unknown model names. An explicit
`pool.SetContextWindow` override always takes precedence over the table

### Proactive Compaction

Before each API call, `shouldCompact` checks whether the estimated context (history + system
prompt + next message) exceeds `contextWindow - reserveTokens` (16,384 tokens reserved for
output). If so, `compactHistory` is called first.

`compactHistory(ctx, lm, history, priorSummary string, keepRecentTokens int64, trigger string) (CompactionResult, error)`:

1. Applies a token-budget walk via `findCutPoint(rest, keepRecentTokens)` (default 20,000
   tokens) to determine how many recent messages to keep verbatim. `keepRecentTokens=0` uses
   the default.
2. `findCutPoint` walks backwards from the newest message, accumulating `len(content)/4`
   tokens per message. When the budget is exhausted, it snaps forward (starting at the
   bust point itself) to the nearest `RoleUser` message boundary, ensuring the kept slice
   always begins with a user message. Returns 0 when the entire history fits in the budget
   (no compaction needed), or when no user boundary exists after the bust point (skip
   compaction rather than produce a malformed kept slice).
3. Asks the LLM to produce a structured summary of all older messages using
   `compactionSummaryPrompt`. When `priorSummary` is non-empty, the prompt is prefixed with
   the prior summary so the model produces an incremental update.
4. Scans the to-be-compacted messages for absolute file paths via `extractFilePaths` and
   appends a "Files referenced in compacted span" list to the summary message.
5. Replaces the old messages with a single `sdk.RoleUser` summary message, then appends
   the kept recent messages. Returns a `CompactionResult`.
6. Updates `a.history` and `a.lastSummary` under their respective mutexes if compaction
   succeeds.
7. If the LLM call fails or returns an empty summary, returns the original history and an
   empty summary string.

`CompactionResult` fields: `History` (post-compaction history; input unchanged on no-op or
failure), `Summary` (raw summary text; empty on no-op or failure), `Messages` (count of
history messages folded into the summary), `Usage` (token cost of the summarization call,
from the stream result's `TotalUsage`), `Latency` (wall-clock duration of the summarization
call), and `Trigger` (the `CompactionTrigger*` kind that caused the run — carried in the
result so callers log it without re-deriving it). No-op runs (history under 2 messages,
everything fits the budget, or no valid user boundary) are built by `noOpCompaction`: zero
`Summary`, zero `Usage`/`Messages`/`Latency`.

**Invariant:** `Summary == ""` means "compaction did not happen". Callers must not increment
compaction counters or emit successful-compaction log records for such results.

**Invariant:** If compaction fails, the original history is used unchanged and the turn
proceeds normally.

**Invariant:** The cut point always lands on a `RoleUser` message boundary. The kept slice
never starts with an `RoleAssistant` message.

**Invariant:** When `compactHistory` is called from `Submit`, `keepRecentTokens` is set to
`contextWindow / 10` (10% of the model's context window). For a 1M-token model this is
100,000 tokens; for a 200k model it is 20,000 tokens. This scaling ensures the kept recent
span is proportional to the available context regardless of model tier.

### Iterative Summary

`Agent` stores `lastSummary string` (protected by `lastSummaryMu sync.RWMutex`). On each
`Submit` call, `lastSummary` is read before the goroutine launches and passed to
`compactHistory` as `priorSummary`. When compaction produces a non-empty summary,
`lastSummary` is updated under `lastSummaryMu.Lock()`.

**Invariant:** The first compaction (`lastSummary == ""`) behaves identically to a
standalone compaction.
**Invariant:** `lastSummary` is only written inside the `Submit` goroutine, immediately
after a successful `compactHistory` call, under `lastSummaryMu.Lock()`.

### Reactive Fallback

After a turn, if the API returns a context-too-long error (`isContextTooLong`), the agent compacts
the history and retries the aborted turn exactly once. This is the fallback path when proactive
compaction was not triggered or not sufficient. The retry uses the same recent-history budget as
proactive compaction and preserves the generated summary in the agent state.

**Invariant:** The reactive retry happens at most once per turn. If the retry also fails with a context error, the error is reported to `onDone` and the turn ends.

**Invariant:** If reactive compaction fails, no retry is attempted. The compaction error is logged,
shown to the user, and reported to `onDone`.

### Percentage-Based Compaction Trigger

In addition to the chars/4 heuristic, `Submit` checks a percentage-based trigger before falling
back to the heuristic. The trigger uses the real API token counts from the most recently completed
turn (`a.lastUsage`) rather than an estimate. The heuristic also includes serialized tool
definitions, since provider requests count those definitions against the context window.

`shouldCompactByUsage(lastUsage fantasy.Usage, contextWindow int64, thresholdPct float64) bool`:

- Returns `true` when `lastUsage.InputTokens / contextWindow >= thresholdPct`.
- Returns `false` when `lastUsage.InputTokens == 0` (first turn — no prior usage data).
- Returns `false` when `contextWindow == 0` (window not configured).
- Returns `false` when `thresholdPct <= 0`.

The check order in `Submit` is:

1. If `pool.CompactConfig().Enabled` and `shouldCompactByUsage(a.LastUsage(), contextWindow, cfg.ThresholdPct)` → compact with trigger `CompactionTriggerUsage` ("usage_threshold").
2. Else if `shouldCompactWithTools(history, sysPrompt, content, tools, contextWindow)` → compact with trigger `CompactionTriggerProactive` ("proactive") (chars/4 heuristic including serialized tool definitions).
3. Else → no proactive compaction.

Reactive compaction after a context-too-long error uses `CompactionTriggerReactive` ("reactive").

### Compaction Observability

After a successful `compactHistory` call, `executeTurn` calls `a.observeCompaction(result)`,
which increments the per-session `compactionCount` and emits a structured log record
(`"agent: context compaction completed"`) carrying agent ID, model, trigger, messages
compacted, summary size, the summarization call's input/output tokens, latency, and the
cumulative session compaction count. This makes compaction frequency, cost, and trigger
mix queryable from logs without a metrics endpoint.

`compactionCount` is a per-`Agent` monotonic counter (session lifetime, never reset).

**Invariant:** `observeCompaction` increments `compactionCount` and emits the log record
only when `result.Summary != ""`. No-op compactions and failures increment nothing; failures
are surfaced through the existing `slog.Error` path.

**Invariant:** `compactionCount` is only written from the agent's turn goroutine (one turn
at a time), so it needs no lock; `CompactionCount()` is the read accessor.

**Invariant:** The first turn always uses the heuristic because `lastUsage` is zero-valued until
the first `streamTurn` call completes. This is the chicken-and-egg bootstrap case.

**Invariant:** If compaction fails, the turn is aborted before the provider request. The error is
logged and delivered through the normal turn error path, and the user sees an explicit context
compaction failure message.

### AutoCompact Configuration

`CompactConfig` holds the percentage-based trigger configuration:

```go
type CompactConfig struct {
    Enabled      bool    // default true
    ThresholdPct float64 // fraction (0.0–1.0); default 0.80
}
```

`AgentPool` reads `WLLR_COMPACT_THRESHOLD` at construction:

- Unset or empty → `ThresholdPct = 0.80`, `Enabled = true`.
- Numeric string (e.g. `"90"` or `"0.90"`) → parsed as percentage if > 1 (divide by 100), else as fraction.
- Unparseable → default 0.80.

`SetCompactConfig(cfg CompactConfig)` replaces the configuration at any time (thread-safe via `p.mu`).

**Invariant:** `WLLR_COMPACT_THRESHOLD` is read once at `NewPool()` time. Changing the env var
after pool creation has no effect.

### lastUsage — Real API Token Tracking

`Agent` stores the token usage from the most recently completed turn:

```go
lastUsage   fantasy.Usage
lastUsageMu sync.RWMutex
```

`setLastUsage(u fantasy.Usage)` is called in the `Submit` goroutine after every `streamTurn`
call — including the reactive retry. On error, `setLastUsage(fantasy.Usage{})` stores a zero
value so a failed turn does not contaminate the next turn's compaction decision.

`LastUsage() fantasy.Usage` is a read-safe accessor used by `MainAgentContextUsage()` and
the percentage-based compaction check.

**Invariant:** `LastUsage()` returns a zero-valued `fantasy.Usage` before the first turn
completes or when the last turn returned an error.

**Invariant:** `lastUsage` is only written from inside the `Submit` goroutine, preventing
concurrent writes.

### EventContextUsage Dispatch

After each successful turn, the pool's `contextUsageDispatcher` callback is invoked:

```go
pool.dispatchContextUsage(cu sdk.ContextUsage, compacted bool, thresholdPct float64, compactions int)
```

The callback is set by the harness via `pool.SetContextUsageDispatcher` and forwards
`EventContextUsage` to WASM extensions via the extension host, avoiding a circular import
between the `agent` and `extension` packages.

`compacted` is `true` when `compactHistory` ran successfully during the turn.
`compactions` is the dispatching agent's cumulative successful-compaction count
(`a.compactionCount`) — additive observability data for the `EventContextUsage` payload.

**Invariant:** `dispatchContextUsage` is only called on successful turns (`err == nil`).
On error or cancellation it is not called, consistent with the pattern for other events.

### Provider-Request Interception

A `ProviderRequestInterceptor` (set via `SetProviderRequestInterceptor`) runs the
`before_provider_request` transform chain immediately before each agent turn
streams to the provider. It receives the agent ID, the messages about to be
sent, and the model, and returns `(outMessages, outModel, blocked, reason)`.
The harness installs an implementation that routes to the extension host's
`DispatchEventChain`, avoiding an agent→extension circular import.

In `executeTurn`, a local `buildStream` helper applies it:

- **No interceptor installed:** `buildStream` returns `(history, content)`
  unchanged — the default turn path is byte-identical (the provider call still
  uses `Messages: history, Prompt: content`). This keeps the common case
  allocation- and behavior-identical.
- **Interceptor installed:** `content` is folded into the outgoing message list
  (`history + {user: content}`), the chain transforms it, and the turn streams
  with `Messages: redacted, Prompt: ""`.
- **Reroute:** when the interceptor returns a different non-empty model, a new
  `LanguageModel` is built via `pool.LanguageModelForModel` and a fresh
  `fantasy.Agent` replaces the turn's `fa`. A model-build failure falls back to
  the original model (best-effort reroute, never fails the turn).
- **Block:** the turn finishes immediately with a `*ProviderRequestBlockedError`
  carrying the reason; no provider call is made. It flows through `finishTurn`
  like any turn error (no drain, `onDone(err)`).

**Invariant:** redaction is **send-time only**. History records the *original*
user content (`a.history` append uses `content`, not the redacted messages), so
a redacting interceptor never mutates stored history or future-turn context.

**Invariant:** the interceptor is consulted once per `streamTurn` attempt,
including the reactive-fallback retry (so a trimmed retry is re-interceptable).

**Invariant:** a malformed transformed payload at the harness seam is tolerated
(`interceptProviderRequest` keeps the original messages/model); a buggy
interceptor can never crash a turn.

### streamTurn Helper

`streamTurn` is the internal helper extracted from `Submit` to keep cyclomatic complexity below threshold. It:

1. Calls `fa.Stream` with history, content, and callbacks.
2. Counts each text delta as one token via `pool.addTokens(1)`.
3. Forwards text deltas to `onToken` (if set).
4. Forwards tool calls to `onToolCall` (if set), skipping provider-executed tool calls (`toolCall.ProviderExecuted == true`).
5. Returns the collected text, real `fantasy.Usage` from `result.TotalUsage`, and any error.
6. On error, returns a zero-valued `fantasy.Usage`.

**Message filtering before LLM calls:** `sdkToFantasyMessages` is called on the history slice before constructing the `AgentStreamCall`. It skips any message whose `Type` is `sdk.MessageTypeSystem` or `sdk.MessageTypeSteering`. These messages are consumed by the Go runtime and must never reach the provider.

**History recording on drain-turn path:** When `content == ""` (drain-turn: inbox messages are the prompt), system messages (`MessageTypeSystem`) are NOT appended to `a.history`. This prevents control messages from accumulating in history and being sent to the LLM on future turns. Steering messages (`MessageTypeSteering`) are recorded in history but still filtered by `sdkToFantasyMessages`.

**Empty-response placeholders:** Providers reject empty text content blocks and require strictly alternating user/assistant messages, so an assistant turn is never recorded as an empty string. `placeholderForEmptyResponse(collected, cancelled)` returns `collected` when non-empty, otherwise `placeholderCancelled` (`"[response cancelled]"`) when the turn's context was cancelled, or `placeholderToolOnly` (`"[tool calls only]"`) for a turn that did tool work but produced no text. These are the single source of truth for the placeholder strings (`agent.go`).

### Spawner Tool-Call Observer

`Spawner.SetToolCallObserver(fn)` installs an optional callback for tool calls made by agents spawned through that spawner. The callback receives `(agentID, toolCallID, toolName, input)` and is invoked from the sub-agent turn goroutine through `Agent.SetOnToolCall`.

**Invariant:** Sub-agent tokens remain silent by default; only tool-call lifecycle starts are surfaced through this observer. A nil observer is valid and preserves the previous no-op behavior.

---

## 10. AgentPool.ProviderName

`SetProviderName(name string)` stores a human-readable display string for the configured provider. `ProviderName()` reads it. Both are guarded by `p.mu`.

**Invariant:** `ProviderName` returns the empty string if `SetProviderName` has not been called. The harness `New()` reads this value at construction time to initialize the status bar.

---

## 11. SpawnOpts

```go
type SpawnOpts struct {
    InheritBasePrompt *bool
    SystemPrompt      string
    Name              string
    ModelName         string
    Tools             []fantasy.AgentTool
    ThinkingBudget    int
    ProviderOptions   fantasy.ProviderOptions
    NotifyParentID    string
    TurnTimeout       time.Duration
}
```

- `InheritBasePrompt`: if nil or pointing to `true`, the agent inherits the pool's accumulated base system prompt (AGENTS.md, tool list, action rules). Set to `false` for focused sub-agents that don't need the full orchestration context.
- `SystemPrompt`: the agent-specific prompt appended after the base prompt on every turn.
- `Name`: human-readable display name for the agent; used in logs and agent list responses.
- `ModelName`: overrides the pool's default model name for context-window sizing during compaction. If empty, the pool default is used.
- `Tools`: static tool list. If `SetToolsFn` is called on the agent after spawn, the dynamic function takes priority over `Tools`.
- `ThinkingBudget`: enables extended thinking with the given token budget. Only supported on Anthropic models. Zero means disabled.
- `ProviderOptions`: passed directly to `fantasy.WithProviderOptions` on each turn. Used for provider-specific settings such as extended thinking.
- `NotifyParentID`: if non-empty, the pool automatically sends a completion message to this agent ID when the spawned agent's final turn ends, giving the parent a guaranteed wakeup.
- `TurnTimeout`: overrides the per-turn context deadline. Zero uses the default (30 minutes). Negative disables the timeout entirely.
- `CreatorID`: the ID of the agent that issued the `create_agent` call that spawned this agent. Empty string for top-level agents. Set by `Spawner` from `extension.SpawnRequest.CallerID`; available via `Agent.CreatorID() string`.

## 12. Spawner

```go
type Spawner struct { ... }

func NewSpawner(pool *AgentPool, toolsFn ToolsFn, notifyFn NotifyFn) *Spawner
func (s *Spawner) Spawn(ctx context.Context, req extension.SpawnRequest) error
```

`Spawner` creates sub-agents in a pool with appropriate callbacks and conventions. It encapsulates:

- Agent-identity system prompt suffix injection (`## Your Agent Identity` section with agent ID).
- Parent ID derivation from the `/` convention in `req.ID` (e.g. `"main/coder"` → parent `"main"`).
- Provider-option construction for extended thinking (`ThinkingBudget > 0`).
- Token suppression for sub-agents (sub-agent tokens are never forwarded to the main chat).
- OnDone wiring: sub-agent errors are logged, the notify function is called, and a failure message is sent to the main agent.
- ToolsFn wiring: the dynamic tool function is called on each sub-agent turn.

**Invariant:** `Spawn` returns an error immediately if `s.pool == nil`.

**Invariant:** If `req.InitialPrompt` is non-empty, `pool.Send(req.ID, req.InitialPrompt)` is called immediately after spawn to start the first turn.

**Invariant:** Sub-agent error notifications call `pool.Send("main", ...)` to surface failures to the orchestrator. Errors from this `Send` that are not `ErrAgentNotFound` are logged at error level.

---

## 13. Error Variables

| Error              | Returned by                    | Condition                                |
|--------------------|-------------------------------|------------------------------------------|
| `ErrAgentExists`   | `Spawn`                       | Agent ID already registered              |
| `ErrAgentNotFound` | `Close`, `SendMessage`, `Send`, `Cancel`, `Team.AddMember` | Agent ID not in pool |
| `ErrTeamExists`    | `CreateTeam`                  | Team ID already registered               |
| `ErrTeamNotFound`  | `CloseTeam`                   | Team ID not in pool                      |

---

## 14. Graceful Shutdown Protocol

An agent is shut down gracefully by sending a `system` message with JSON content
`{"event":"shutdown_request","from":"<callerID>"}` to its inbox, then triggering a turn
via `pool.Send`. The agent's `finishTurn` detects the shutdown_request and handles it
without abrupt cancellation.

### Message flow

```
Orchestrator                         Worker (target)
─────────────                        ───────────────
shutdown_agent(workerID)
  → agent_send_message(id, payload, type="system")
  → agent_run(id)                    [turn starts or next turn triggered]
                                     [turn completes normally]
                                     finishTurn detects shutdown_request
                                       → pool.SendMessage(creatorID, AGENT_SHUTDOWN)
                                       → pool.Close(a.id)
                                       → onDone(nil)
Orchestrator inbox receives:
  {event:"AGENT_SHUTDOWN", agent_id:"workerID"}
```

### finishTurn shutdown detection

`finishTurn` runs after `isRunning` transitions to false. It:

1. Drains the inbox for messages that arrived **during** the current turn.
2. Scans for any system message whose JSON content has `event == "shutdown_request"`.
   Non-matching system messages are passed through as `normalPending`.
3. Also scans the `consumed` slice (the inbox messages `Submit` drained as **this**
   turn's content) for a `shutdown_request`. This recovers a shutdown delivered to an
   **idle** agent, which `Submit` drains directly (not the post-turn `DrainInbox`).
   Without this scan the system message would be filtered from history and lost.
4. Also checks `a.pendingShutdownFrom` — a field set in a previous `finishTurn` cycle
   when a shutdown_request was deferred (see drain-until-empty below).
5. If **normal messages coexist** with a shutdown_request: re-queues the normal messages
   to the inbox, sets `a.pendingShutdownFrom = shutdownFrom`, and calls `Submit("")` for
   a drain turn. The shutdown is deferred until all normal work is done.
6. If **only the shutdown_request** remains (no normal pending, no new inbox messages):
   - Marshals `{"event":"AGENT_SHUTDOWN","agent_id":"<id>"}` as a system message.
   - Calls `pool.SendMessage(shutdownFrom, agentShutdownMsg)` to notify the creator.
   - Calls `pool.Close(a.id)` to remove self from pool (idempotent, safe from finishTurn goroutine).
   - Calls `onDone(nil)` exactly once and returns.

### Invariants

**Invariant:** `pendingShutdownFrom` is only read and written inside `finishTurn`, which
runs after each `isRunning.Store(false)`. Because at most one goroutine can hold
`isRunning == true` at a time, no additional mutex is needed for `pendingShutdownFrom`.
`Submit` must never access this field.

**Invariant:** Drain turns triggered from `finishTurn` use the original context passed to
`Submit`, not `context.Background()`. This allows harness shutdown to cancel in-flight
drain turns rather than running them to their full 30-minute timeout.

**Invariant:** A `shutdown_request` delivered to an **idle** agent (via
`Deliver(wake=true)`) is recovered from the `consumed` inbox slice in `finishTurn`, not
lost. `executeTurn` short-circuits a control-only wake (empty content + all drained
messages are system/steering) straight to `finishTurn` without an LLM call — otherwise
the empty prompt would error and skip shutdown handling. See `allControlMessages`.

**Invariant:** The shutdown_request is never re-injected into the inbox. It is consumed
by `finishTurn` and persisted in `a.pendingShutdownFrom` if deferral is needed. This
ensures Submit's initial `DrainInbox` cannot consume the deferred shutdown before the
next `finishTurn` can act on it.

**Invariant:** `onDone` is called exactly once per logical session. The shutdown path
calls `onDone(nil)` and returns; the normal path at the bottom of `finishTurn` is
therefore not reached. No double-`onDone` is possible.

**Invariant:** `pool.Close` is idempotent. Calling it from `finishTurn` after
`isRunning.Store(false)` is safe — the agent is no longer running when Close is called,
and Close acquires `p.mu.Lock` without any pool lock being held by the agent goroutine.

**Invariant:** System messages are never recorded in `a.history` on the drain-turn path
(§9). An AGENT_SHUTDOWN message sent by the worker to the creator's inbox is a system
message; it is filtered from LLM context when the creator's next turn runs.

---

## 15. Idle Notification (Wakeup Contract)

When a sub-agent completes a turn and goes idle, it notifies its creator so the
orchestrator can review results or shut the agent down. This makes the
"spawn → work → the orchestrator is woken when the agent finishes" pattern work
without the orchestrator polling.

### When it fires

Inside `finishTurn`, on the clean-idle transition: the turn completed with no error
and no cancellation, **no normal pending messages remain**, and **no shutdown is
pending**. This is the same branch that would otherwise fall through to the final
`onDone(err)`. It is the symmetric counterpart to the AGENT_SHUTDOWN path.

### What it sends

`pool.Deliver(creatorID, msg, wake=true)` where `msg` is a **normal** (model-visible)
user-role message:

```
[agent '<id>' is idle — review its results with get_agent_status or shut it down with shutdown_agent]
```

Unlike AGENT_SHUTDOWN (a `system` message filtered from LLM context), the idle
notification is intentionally model-visible: the orchestrator's model must see that a
sub-agent finished in order to act on it.

### Invariants

**Invariant:** The idle notification fires only when `a.creatorID != ""`. Top-level
agents (e.g. `main`, whose `creatorID` is empty) never self-notify, so there is no
notification loop. `main` waking itself is impossible by construction.

**Invariant:** The notification fires once per running→idle transition, not continuously.
An orchestrator that wakes, inspects, and does nothing leaves the sub-agent idle and
quiet until it is given new work (which produces a new transition on completion).

**Invariant:** The notification uses `Deliver` with `wake=true`, so the creator is both
informed (message in its inbox/history) and woken (a turn is started or drain-until-empty
picks it up). A delivery failure other than `ErrAgentNotFound` is logged and ignored;
`ErrAgentNotFound` (creator already closed) is silently tolerated.

**Invariant:** A sub-agent that explicitly `send_message`s its creator during the turn
*and then* goes idle produces both the explicit message and the idle notification. These
are coalesced by the creator's drain-until-empty into a single turn. This is intentional
(simple, always-notify behaviour); no per-turn suppression is performed.

**Invariant:** `creatorID` is set by `Spawner.Spawn` (from `SpawnRequest.CallerID`) or via
`Agent.SetCreatorID`. It must be set before the agent's first turn completes for the
notification to fire on that turn.
