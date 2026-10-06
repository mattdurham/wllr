# agent — Design Notes

Append-only design decision log. Never delete entries; add an `*Addendum (date):*` if a decision is reversed.

---

## 1. Sequential Inbox Drain — Why Not Concurrent Delivery

*Added: 2026-05-06*

**Decision:** Inbox messages are drained synchronously at the start of each `Submit` call (via `DrainInbox`) and prepended to `priorHistory` before the fantasy.Agent.Stream call. They are NOT delivered in a separate goroutine or injected mid-turn.

**Rationale:** The LLM provider receives a single ordered list of messages per request. Injecting inbox messages mid-turn would require pausing the stream and re-starting the provider request, which is not supported by fantasy's streaming API. Delivering them as prior-history context (prepended before the current prompt) is equivalent to the LLM having "seen" those messages in an earlier exchange — the correct semantic for inter-agent communication. Sequential drain also eliminates race conditions between concurrent senders: all senders use `AppendInbox` (which holds `inboxMu`) and all reads use `DrainInbox` (which atomically clears the inbox). No message is ever lost or duplicated.

**Consequence:** Messages sent to an agent's inbox after `DrainInbox` has been called (i.e., during an active turn) are queued for the next turn. This creates at-most-one-turn-latency for inbox delivery, which is acceptable for the inter-agent coordination use case.

*Addendum (2026-05-27):* The ordering described above changed from **prepend** to **append** — inbox messages are now appended AFTER priorHistory, not prepended before it. See §16 for rationale. The sequential-drain invariant (no concurrent delivery, no lost messages) is unchanged.

---

## 2. New fantasy.Agent Per Turn — Why Not Reuse

*Added: 2026-05-06*

**Decision:** `Agent.Submit` creates a new `fantasy.NewAgent(lm, agentOpts...)` on every turn rather than reusing a long-lived `fantasy.Agent` instance.

**Rationale:** The original `startStream` pattern in `harness/model.go` (pre-pool refactor) also created a new `fantasy.Agent` per user message. This was deliberate: `fantasy.Agent` may hold internal streaming state or conversation context from a previous run that would interfere with the next turn if the same instance were reused. Creating a new agent per turn ensures a clean slate for the streaming call. The conversation history is managed explicitly by `Agent.history` (maintained by the pool package) and passed to `Stream` as `Messages`, so no history context is lost by using a fresh agent.

**Consequence:** One `fantasy.NewAgent` allocation per turn. The `fantasy.LanguageModel` itself is reused (stored on the `Agent` struct) — only the agent wrapper is recreated. This matches the established pattern and keeps memory overhead minimal.

---

## 3. Per-Agent onToken/onDone Callbacks — Why Not Pool-Level Hooks

*Added: 2026-05-06*

**Decision:** Token and completion notifications are delivered via per-agent `SetOnToken`/`SetOnDone` callbacks rather than a pool-level `OnToken`/`OnDone` hook.

**Rationale:** Different agents may require different handling: the main agent's tokens go to the TUI chat window; sub-agent tokens may be logged or discarded. A pool-level hook would need to multiplex by agent ID, which effectively recreates per-agent routing in the caller. Keeping callbacks on the `Agent` struct is simpler, allows different policies per agent, and avoids centralizing routing logic in the pool. The harness wires the main agent's callbacks in `SetProgram`; sub-agent callbacks are wired in the `OnAgentSpawn` closure in `model.go`.

**Consequence:** Callers must wire callbacks before calling `Submit`. If `SetOnToken` is not called, tokens are silently discarded (the `if onToken != nil` check). This is intentional — not all callers need token streaming (e.g. batch-mode sub-agents).

`SetOnTurnStart` follows the same per-agent callback model. It receives the
explicit prompt and the inbox batch actually claimed by that turn before the
provider request starts. The harness uses it to dispatch transcript events for
queued messages at pickup time; inspecting the UI streaming flag is not
sufficient because Bubble Tea updates can lag the agent goroutine.

---

## 4. AgentPool.Send and AgentPool.Cancel — Pool-Level Convenience Methods

*Added: 2026-05-06*

**Decision:** `AgentPool.Send(id, content)` and `AgentPool.Cancel(id)` are added as convenience methods that delegate to `agent.Submit` and `agent.Cancel` respectively.

**Rationale:** The harness `model.go` SubmitMsg and abortStreamMsg handlers need to invoke actions on the main agent via the pool reference. Without these methods, the harness would need to call `pool.Get(id)` and then check for nil before calling the agent method — adding two lines of boilerplate at every call site. The pool methods encapsulate the nil-check and return `ErrAgentNotFound` for unknown IDs, matching the established pattern of other pool methods (e.g., `SendMessage`, `Close`).

**Consequence:** `Send` starts the agent turn in a background goroutine (non-blocking). Callers must not assume the turn is complete when `Send` returns.

---

## 5. AgentPool.ProviderName — Why Stored on Pool Not Passed to harness.New

*Added: 2026-05-06*

**Decision:** The provider display name is stored on `AgentPool` via `SetProviderName`/`ProviderName()` and read by `harness.New` at construction time, rather than passed as a separate parameter to `harness.New`.

**Rationale:** After the model.go refactor, `harness.New` takes `(pool, mainAgentID, h)` — removing the `langModel` and `provName` parameters. The status bar still needs a provider name. Storing it on the pool is natural: the pool already owns the provider (via `SetProvider`), and the name logically accompanies it. This avoids adding a fourth parameter to `harness.New` solely for display purposes, and keeps the display name co-located with the provider.

**Consequence:** `cmd/main.go` must call `pool.SetProviderName(cfg.Provider)` before calling `harness.New`. The harness reads the name once at construction; runtime changes to the provider name are not reflected in the status bar without creating a new model.

---

## 6. Global Token Counter on AgentPool

*Added: 2026-05-06*

**Decision:** All agents share one `atomic.Int64` token counter on the pool.

**Rationale:** The TUI status bar shows total tokens across all active agents. A per-agent counter would require aggregation on every status bar refresh.

**Consequence:** Counter reflects total tokens since pool creation, not per-session.

---

## 7. BaseSystemPrompt Propagated to All Agents on Set

*Added: 2026-05-08*

**Decision:** `SetBaseSystemPrompt` and `AppendBaseSystemPrompt` immediately propagate the new prompt to every currently-registered agent in the pool (under `p.mu.RLock()`), in addition to storing it for future spawns.

**Rationale:** The context extension loads AGENTS.md and calls `set_system_prompt` after extensions have initialized, at which point agents may already be registered (the main agent is registered before extensions are loaded). Without propagation, the base prompt would only take effect for agents spawned after the call, leaving the main agent without the AGENTS.md content. Propagation under `RLock` is safe because `Agent.SetSystemPrompt` / `AppendSystemPrompt` use their own per-field mutex.

**Consequence:** All agents always see the full accumulated base prompt from the point of the most recent `SetBaseSystemPrompt` / `AppendBaseSystemPrompt` call. Agents that were running a turn during the propagation will see the new prompt on their next turn.

---

## 8. modelName Field and contextWindowForModel — Why Per-Agent

*Added: 2026-05-08*

**Decision:** Each `Agent` stores its model name and resolved context window at spawn time. The pool records provider/API or user-resolved windows per model; built-in metadata supplies known Claude/Codex/OpenAI/Gemini values. Unknown models do not receive a guessed default and cannot run until resolved.

**Rationale:** Different model families have very different context windows (128k vs 200k vs 1M tokens). A single hardcoded constant would either be too conservative (wasting compaction on large-context models) or too aggressive (skipping compaction on small-context models). The per-agent field allows sub-agents spawned with a different model to compact at the correct threshold. The substring-matching approach avoids maintaining an exhaustive model name registry while covering the most common cases.

**Consequence:** Unknown model names have no context window. Interactive model selection prompts for a positive token count and persists it per provider/model. Subagents with unresolved model metadata fail their turn with an actionable error so the parent can request resolution.

---

## 9. streamTurn Extracted for Complexity Reduction

*Added: 2026-05-08*

**Decision:** The core streaming loop (calling `fa.Stream`, collecting tokens, forwarding tool calls) is extracted from `Submit` into the unexported `streamTurn` helper.

**Rationale:** `Submit` contains proactive compaction logic, reactive retry logic, history management, and the streaming call. Keeping all of this in one function pushes cyclomatic complexity above threshold and makes the retry path harder to read. Extracting `streamTurn` as a pure function (no state mutations) makes the retry pattern obvious: the same function is called with potentially different `history` slices on the proactive and retry paths.

**Consequence:** `streamTurn` must not mutate `Agent` state directly. All history updates remain in `Submit`.

---

## 10. CancelAll — Batch Cancellation Pattern

*Added: 2026-05-08*

**Decision:** `AgentPool.CancelAll()` snapshots the agents slice under `RLock`, then calls `a.Cancel()` on each agent outside the lock.

**Rationale:** Calling `Cancel()` while holding the pool lock would invert the intended lock order (`pool.mu` → `agent.cancelMu`). The same pattern already exists in `Close` (which releases `p.mu` before calling `a.Cancel()`). Snapshotting under `RLock` and releasing before calling Cancel is consistent and avoids deadlock.

**Consequence:** Agents spawned between the snapshot and the Cancel calls will not be cancelled by that `CancelAll` invocation. This is acceptable because `CancelAll` is used for shutdown and Ctrl+C, where newly spawned agents are extremely unlikely.

---

## 11. Proactive vs. Reactive Compaction — Two-Phase Strategy

*Added: 2026-05-08*

**Decision:** Compaction uses a two-phase approach: proactive compaction (before the API call) triggers when estimated tokens exceed the window minus the output reserve; reactive compaction (after a context-too-long error) summarizes older history and retries the aborted turn once.

**Rationale:** Proactive compaction avoids the round-trip cost of a failed API call. However, the `chars/4` token estimate is intentionally approximate and may undercount; the reactive path handles cases where the estimate was too optimistic. The two phases together provide safety coverage without over-compacting on every turn. Reactive compaction uses the same bounded recent-history budget as proactive compaction, then retries the aborted turn once.

**Consequence:** In the worst case, one extra failed API call is made per compaction event on the reactive path. Older context is retained in the generated summary instead of being permanently discarded.

---

## 12. Token-Budget Cut Point — Why Not keepMessages=20

*Added: 2026-05-11*

**Decision:** Replace the fixed `keepMessages=20` message count in `compactHistory` with a
token-budget walk (`findCutPoint`) using `defaultKeepRecentTokens=20_000` tokens.

**Rationale:** A fixed message count is insensitive to message length. A 20-message window
could contain 100 tokens (trivial) or 100,000 tokens (dangerously close to the context
limit). A token budget ensures the kept span is proportionally sized regardless of message
verbosity. The `chars/4` heuristic is deliberately approximate; the budget is sized (20k)
to be well within any current model's window post-compaction. The snap-to-user-boundary
rule (never cut between a user→assistant pair) preserves the invariant that the kept slice
always begins with a user message, which all LLM APIs require.

**Consequence:** The reactive fallback uses the same token-budget compaction path as proactive
compaction, so the fixed message count is no longer used for recovery.

---

## 13. Iterative Compaction Summary — Why Store on Agent Not Return to Caller

*Added: 2026-05-11*

**Decision:** The compaction summary string is stored on `Agent.lastSummary` (with its own
`sync.RWMutex`) rather than returned to the pool or passed through a callback.

**Rationale:** The summary is per-conversation-session state — it belongs on `Agent`, which
already owns `history`. Returning it to the pool or through a callback would require the
pool or harness to track per-agent state that it doesn't otherwise need. Reading
`lastSummary` before launching the `Submit` goroutine (as a local `priorSummary`) ensures
a consistent snapshot even if `Submit` is called again concurrently (though SPECS.md
Section 2 forbids concurrent Submit calls). The `lastSummaryMu` prevents data races on the
field itself.

**Consequence:** `lastSummary` is reset to "" if the agent is re-used across unrelated
tasks. Callers that want to preserve summary context across sessions must persist it
externally (out of scope).

---

## 14. 10% keepRecent Scaling — Why One-Tenth of the Context Window

*Added: 2026-05-11*

**Decision:** In `Submit`, `keepRecentTokens` passed to `compactHistory` is set to
`contextWindow / 10` (integer division), not to the fixed `defaultKeepRecentTokens` constant.

**Rationale:** A fixed 20,000-token keep budget works well for 200k-token models but is
over-conservative for 1M-token models (where 20k is only 2% of the window). Scaling to 10%
of the context window gives a keep budget that grows with the model: 100k tokens for 1M
models, 20k for 200k models. 10% is a rough heuristic that balances two competing concerns:
keep enough recent context that the model has coherent short-term memory of what just
happened, but do not keep so much that compaction almost never triggers (which would waste
the context window on verbatim history instead of a dense summary). At 10% the model always
has at least 90% of its window available for the system prompt, the summary, and new output.

**Consequence:** For large-window models the kept span is generous (100k tokens ≈ hundreds of
medium-length messages). On smaller models the 10% value may coincide with
`defaultKeepRecentTokens`. The `defaultKeepRecentTokens` constant is retained for callers
that invoke `compactHistory` directly and pass `keepRecentTokens=0`.

---

## 15. AgentPool.ListTeams and GetTeamMembers — Pool-Level Team Introspection

*Added: 2026-05-11*

**Decision:** `AgentPool.ListTeams()` and `AgentPool.GetTeamMembers(teamID)` are added as
pool-level convenience methods for enumerating teams and their membership.

**Rationale:** The agents WASM extension exposes a `get_team` tool that the LLM calls to
inspect team state. Without a dedicated `GetTeamMembers` pool method, the host could only
return all agents (wrong) or nothing at all. `ListTeams` mirrors `ListAgents` and enables
the `team_list` host call, allowing the LLM to discover active teams. Both methods take a
read lock and snapshot under that lock, consistent with all other read methods on the pool.

`GetTeamMembers` returns `ErrTeamNotFound` when the team does not exist, consistent with
the existing error variable semantics (`ErrAgentNotFound`, `ErrTeamNotFound`).

**Consequence:** These methods expose team membership at a point in time — membership may
change between the read and subsequent action. Callers must tolerate TOCTOU gaps.

---

## 16. Inbox Ordering Changed from Prepend to Append

*Added: 2026-05-27*

**Decision:** Inbox messages are appended AFTER prior history rather than prepended before it.

**Rationale:** The original prepend design (§1) was incompatible with the empty-prompt
mechanism used by OnAgentRun (harness/model.go). Fantasy's createPrompt rejects an empty
prompt when the last message in the history array is an assistant message — which is always
the case after the first turn when inbox messages are prepended. Appending inbox messages
makes them the most-recent context (which is semantically correct — they ARE more recent
than the prior conversation) and ensures the last message is always a user/inbox message
when the inbox is non-empty, making empty-prompt valid.

**Consequence:** The LLM now sees inbox messages as the most recent context in the message
list, not as earlier context. This is more correct behavior. The "prior context" framing
in §1 is superseded by this decision. §1 is retained as historical context.

---

## 17. isRunning Guard — Drain-Until-Empty Pattern

*Added: 2026-05-27*

**Decision:** `Agent.Submit` uses an `atomic.Bool` (`isRunning`) to detect concurrent calls.
If a turn is already running when `Submit` is called, the new content is appended to the
inbox and `Submit` returns immediately. After each turn completes, the goroutine checks for
new inbox messages (drain-until-empty) and, if any exist, fires `onDone` and restarts
immediately with `context.Background()`.

**Rationale:** SPECS.md §2 previously placed the burden on callers to avoid concurrent
Submit. But the system itself violates this: multiple sub-agents finishing simultaneously
all trigger `pool.Send("main", ...)` which launches concurrent Submits. Without a guard,
concurrent Submits snapshot the same history, run in parallel, and the last writer wins —
silently corrupting the history. The drain-until-empty pattern ensures all queued messages
are processed in order without concurrent goroutines.

**Consequence:** Submit is now safe to call concurrently. Queued messages are processed
sequentially. The onDone callback fires after each sub-turn, so the TUI may see multiple
StreamDoneMsg events from a single logical "agent wakeup." Each StreamDoneMsg finalizes
one response chunk.

*Addendum (2026-07-16):* The post-turn drain now also runs after **failed** turns
(`ctxErr == nil` is the only gate; cancellation still terminates the chain and
preserves the queue per issue #48). Previously a provider error stranded messages
queued mid-turn in the inbox until the next explicit Submit — for sub-agents,
potentially forever, since nobody else submits on their behalf. Loop safety comes from
Submit's drain-first ordering: every turn consumes its inbox batch before it can fail,
so the chain only continues while genuinely new messages arrive; a persistent error
costs one failed turn per arriving batch. Idle notifications became success-only at
the same time: an errored settle already wakes the creator via the spawner's
`agent_failed` notification, and an idle ping would double-wake. Regression coverage:
`TestDeliver_ErrorTurn_StillDrainsQueued` and
`TestDeliver_ErrorTurn_DrainFailureTerminatesChain`.

*Addendum (2026-10-05):* Two turn-goroutine exit paths bypassed `finishTurn` entirely
and wedged the agent as permanently "running" — `isRunning` was never released, so
every later `Submit` silently re-queued and the agent was unrecoverable without a
restart: (1) `executeTurn`'s nil-language-model early return fired `onDone` directly
(`pool.Spawn` does not validate the LM, so a nil-model agent is reachable — e.g. a
model factory that returns `nil, nil`); (2) the `Submit` goroutine's panic recover
fired `onDone` directly. Both now route through `finishTurn`. The recover path only
takes over when `isRunning` is still set (the panic preceded `finishTurn`), logs the
panic with a full stack, and passes `childCtx.Err()` so a panic during a cancelled
turn still preserves the queue (issue #48). Because a panic aborts `executeTurn`
before history recording, a panicking turn leaves no prompt or reply in history —
queued inbox messages still drain into a follow-up turn. Regression coverage:
`TestSubmit_NilModel_ReleasesRunning` and
`TestSubmit_PanicInTurn_ReleasesRunningAndDrains`.

## 18. Real API Token Tracking — Why Replace chars/4 Heuristic

*Added: 2026-05-30*

**Decision:** Replace the chars/4 token estimation for compaction decisions with real API
token counts from the peak provider step in `fantasy.AgentResult`. Store the last turn's usage on `Agent`
as `lastUsage fantasy.Usage` and expose it via `Agent.LastUsage()`. Expose context window
usage to the harness and extensions via `AgentPool.MainAgentContextUsage()`, `sdk.ContextUsage`,
and `EventContextUsage`.

**Rationale:** The chars/4 heuristic systematically underestimates token counts for
code-heavy conversations (identifiers and symbols cost more than 0.25 tokens/char on
average) and can underestimate heavily by 30–50% in practice. Real API counts allow the
percentage-based compaction trigger (`shouldCompactByUsage`) to fire at the correct time
rather than too late, reducing context-length errors.

**Consequence:** The first turn still uses the heuristic because `lastUsage` is zero before
the first API call completes. This chicken-and-egg bootstrap is unavoidable and documented
in SPECS.md §9. Subsequent turns use real counts, which are more accurate.

---

## 19. contextUsageDispatcher Callback — Why Not Direct DispatchEvent on Host

*Added: 2026-05-30*

**Decision:** After each completed agent turn, context window usage is forwarded to the harness
via a `contextUsageDispatcher` callback registered on `AgentPool` (via `SetContextUsageDispatcher`),
rather than by calling `DispatchEvent` directly on the extension host from within the agent package.

**Rationale:** The agent package cannot import the extension package (which owns `DispatchEvent`)
without creating a circular import: extension → agent (to get the pool) → extension. The callback
pattern breaks this cycle: the harness or `cmd/main.go` wires the dispatcher at startup, passing a
closure that holds a reference to the extension host. The agent package only depends on the `sdk`
package (for `ContextUsage`), which has no dependency on extension or harness.

**Consequence:** The dispatcher is an optional hook — if `SetContextUsageDispatcher` is never called,
`dispatchContextUsage` is a no-op. This means tests that don't need extension dispatch don't need to
wire one up. The harness is responsible for ensuring the dispatcher is set before the first agent turn.

---

## 20. MessageType filtering and CreatorID tracking

*Added: 2026-05-31*

**Decision:** Add `sdk.MessageType` support to `sdkToFantasyMessages` and history recording. Add `creatorID string` to `Agent` populated from `SpawnOpts.CreatorID`.

**Rationale:** Agent coordination (shutdown, AGENT_SHUTDOWN) requires messages that must never reach the LLM. Previously all inbox messages were treated uniformly. The `MessageType` field (added to `sdk.Message`) allows `sdkToFantasyMessages` to skip `system` and `steering` type messages, and history recording to skip `system` messages on the drain-turn path. `CreatorID` is needed so the shutdown flow can route AGENT_SHUTDOWN back to the correct parent. It is set from `extension.SpawnRequest.CallerID`, which is populated by the WASM agents extension from the calling agent's ID.

**Consequence:** `SpawnOpts.CreatorID string` is added. `Agent.CreatorID() string` accessor is exported. `extension.SpawnRequest.CallerID string` is added. `host.go` `handleAgentSpawn` parses `caller_id` from JSON. `extensions/agents/main.go` `handleCreateAgent` passes `scope` as `caller_id`. System messages in the drain-turn path are not written to `a.history`.

---

## 21. finishTurn graceful shutdown — pendingShutdownFrom field

*Added: 2026-05-31*

**Decision:** Add `Agent.pendingShutdownFrom string` field. When `finishTurn` finds a `shutdown_request` system message alongside normal pending messages, store the sender ID in `pendingShutdownFrom` and re-queue only the normal messages (not the shutdown_request itself). The next `finishTurn` cycle checks `pendingShutdownFrom` after draining normal work.

**Rationale:** The drain-until-empty pattern (§17) means `Submit("")` is called for each batch of pending normal messages. At the top of `Submit`, `DrainInbox` is called, consuming everything in the inbox. If the shutdown_request were re-injected into the inbox alongside normal messages, `Submit`'s initial drain would consume it, and the drain turn's `finishTurn` would never see it. Storing the sender in a field on the `Agent` instead of re-queuing the message as an inbox entry keeps the shutdown request alive across drain turns without violating the inbox drain invariant. `pendingShutdownFrom` is only read and written in `finishTurn`, which runs after `isRunning.Store(false)` — at most one `finishTurn` call is active at any time, so no additional mutex is needed.

**Consequence:** `Agent` struct gains `pendingShutdownFrom string`. `finishTurn` now has three exit paths: (a) normal drain-turn re-queue when normal messages are pending, (b) graceful shutdown when only a deferred shutdown_request remains, (c) the original error/cancel path. `onDone` is still called exactly once. `AgentBridge.SendMessage` signature changed from `(id, message string)` to `(id string, msg sdk.Message)` to allow the `type` field to be passed from WASM through the host bridge. `extensions/agents/main.go` `handleShutdownAgent` now sends a system message via `agent_send_message` (with `type: "system"`) and triggers `agent_run` instead of calling `agent_close` directly.

---

## 22. Token usage propagation and context-usage dispatch wired into the turn

*Added: 2026-06-29*

**Decision:** `streamTurn` now returns the peak provider-step usage from the `*fantasy.AgentResult` produced by `fa.Stream` (previously discarded with `_`). Fantasy's `TotalUsage` sums every tool-loop step, so it is cumulative billing telemetry rather than context occupancy. `executeTurn` stores the peak usage via `a.setLastUsage(usage)` on a successful, non-cancelled turn and, for the main agent only, calls `pool.dispatchContextUsage(sdk.ContextUsageFromFantasy(usage, contextWindow), didCompact)`.

**Rationale:** `setLastUsage` and `dispatchContextUsage` were defined and specified (see §19 and the sdk `EventContextUsage` contract) but never actually invoked — the streaming turn dropped the result's usage entirely, so `LastUsage()` always returned zero, `MainAgentContextUsage()` reported empty, and `EventContextUsage` never fired. This made the behavior diverge from the documented spec. The fix makes the runtime match the existing contract: usage is captured per turn, and the dispatcher fires once per completed main-agent turn.

**Consequence:** `streamTurn` signature changed from `(string, error)` to `(string, fantasy.Usage, error)`. A `didCompact` bool tracks whether proactive compaction ran this turn and is forwarded as the dispatcher's `compacted` argument. Context-usage dispatch is restricted to `MainAgentID` so sub-agent turns do not overwrite the main context-window indicator. No public API of the agent package changed; the previously-zero `LastUsage()`/`MainAgentContextUsage()`/`EventContextUsage` values now carry real data.

*Addendum (2026-10-05):* Failed and cancelled turns no longer zero `lastUsage` — the
last-known value is retained (and re-dispatched for main), because the context does not
shrink when a turn fails: zeroing made the statusline drop to `ctx:0` after any provider
error and silently disabled the usage-threshold compaction trigger. A successful turn
whose provider reported zero tokens across input/cache fields also retains the prior
value. This reverses the "failed/cancelled turns report zero (never stale counts)"
wording above; failed-turn accounting lives in `TurnUsage.Err`, which is the telemetry
channel the original concern was actually about.

---

## 23. Deliver primitive and automatic idle notification (subagent comms hardening)

*Added: 2026-06-30*

**Decision:** Add `AgentPool.Deliver(id, msg, wake bool)` — an atomic "append to inbox and (optionally) start a turn" primitive — and a `SetWakeNotifier` callback. Add automatic idle notification in `finishTurn`: when a sub-agent with a non-empty `creatorID` goes idle (clean transition, no pending, no shutdown), it `Deliver`s a model-visible `[agent '<id>' is idle …]` message to its creator with `wake=true`. The bundled `agents` (`send_message`, `shutdown_agent`) and `tasks` (`TASK_DONE`) extensions now call a new `agent_deliver` host method instead of the two-call `agent_send_message` + `agent_run` pattern. The harness `Run` bridge now Submits empty content (`""`) instead of the synthetic `"[process pending inbox messages]"` placeholder.

**Rationale:** The subagent communication layer had three latent fragilities, all rooted in "deliver a message" being expressed as two independent, optional host calls:

1. **Lost wakeups.** The `tasks` extension sent `TASK_DONE` via `agent_send_message` but never called `agent_run`, so the notification sat unprocessed in the owner's inbox until the owner happened to run for some other reason. `agent_deliver` makes delivery-and-processing atomic, eliminating this class of bug.
2. **Unimplemented idle wakeup.** The `agents` extension's system-prompt guidance promised "you will be woken when agents complete," but no code delivered that — a sub-agent that finished silently never woke the orchestrator, risking a permanent stall. The new `finishTurn` idle notification makes the documented contract real.
3. **History pollution.** `agent_run` triggered a turn with the literal content `"[process pending inbox messages]"`, which was recorded verbatim as a user message the model could see. Submitting empty content uses the existing drain path so the real inbox message is the turn content.

The user chose the simple "always notify" design over per-turn suppression or an opt-out spawn flag: every running→idle transition of a sub-agent with a creator notifies that creator. Double-notifications (explicit `send_message` + idle) are coalesced by the creator's drain-until-empty into one turn, so the cost is negligible and the behaviour is predictable.

**Consequence:** `AgentBridge` gains a `Deliver(id, msg, wake)` method (implemented by `harnessAgentBridge`, `earlyAgentBridge`, and all test doubles). `sdk` gains `MethodAgentDeliver` (`"agent_deliver"`) and the host gains `handleAgentDeliver`. `Agent` gains `SetCreatorID` (for tests/non-spawner wiring). The wake notifier replaces the harness's inline `agentWakeupMsg`-on-`Run` so that idle notifications and result deliveries also surface the TUI streaming indicator. `agent_send_message` and `agent_run` remain for backward compatibility and for the harness's user-driven main-agent turn, but extension authors should prefer `agent_deliver`. The idle-notification fires from the sub-agent's `finishTurn` goroutine, so `Deliver`/`AppendInbox`/`Submit` on the creator must remain goroutine-safe (they are — same pattern as the existing AGENT_SHUTDOWN send).

---

## 24. Control-only wake short-circuit (shutdown-to-idle correctness fix)

*Added: 2026-06-30*

**Decision:** `executeTurn` now short-circuits straight to `finishTurn` (no LLM call) when the turn has empty content AND every drained inbox message is a Go-level control message (system/steering), detected via the new `allControlMessages` helper. Additionally, `finishTurn` gained a `consumed []sdk.Message` parameter and scans it for a `shutdown_request` so a shutdown delivered to an idle agent is recovered rather than lost.

**Rationale:** Discovered while writing correctness tests for the §23 Deliver work. `shutdown_agent` was changed to use `agent_deliver` (wake=true). For an **idle** agent, `Deliver` calls `Submit(ctx, "")`, which drains the just-queued `shutdown_request` as the turn's content. But system messages are filtered from LLM context (`sdkToFantasyMessages`), so the turn would call the provider with an empty prompt and history, erroring with "prompt can't be empty when there are no messages". Because `finishTurn` only runs its shutdown/drain logic on a non-errored, non-cancelled turn, the erroring turn skipped shutdown handling entirely — the `shutdown_request` was silently lost and the agent was stranded in the pool forever. The §23 work introduced this regression for the idle case; the prior `agent_send_message`+`agent_run` path with the `"[process pending inbox messages]"` placeholder accidentally avoided it by always having non-empty content. The short-circuit makes control-only wakes (shutdown of an idle agent) act on the control message without a pointless, failing LLM round-trip; the `consumed` scan ensures the request is seen by `finishTurn` even though it was drained by `Submit` rather than the post-turn `DrainInbox`.

**Consequence:** `finishTurn`'s signature gains a trailing `consumed []sdk.Message` argument (the inbox messages this turn consumed as content). `allControlMessages` is a new package-private helper. Regression coverage: `TestDeliver_ShutdownRequestToIdleAgent` (idle agent self-closes, creator gets AGENT_SHUTDOWN not idle), plus `TestIdleNotification_SuppressedDuringShutdown` and `TestDeliver_WhileRunning_DrainsAfterTurn`. This is a behavior fix, not an API change; the existing running-agent shutdown path (covered by `TestFinishTurn_ShutdownRequest_*`) is unaffected because that path drains via the post-turn `DrainInbox`.

---

## 25. mailbox type + empty-response placeholder consolidation (Tier 2/3 cleanup)

*Added: 2026-06-30*

**Decision:** Extract the agent's pending-message queue into an unexported `mailbox` type (`mailbox.go`) that owns the message slice and its mutex, embedded by value as the `Agent.inbox` field. `Agent.AppendInbox`/`DrainInbox`/`InboxLen` become thin forwarders to `mailbox.append`/`drain`/`len`; the standalone `inboxMu sync.RWMutex` field is removed. Separately, consolidate the two synthetic assistant-turn placeholder strings into named constants (`placeholderCancelled`, `placeholderToolOnly`) behind a single `placeholderForEmptyResponse(collected, cancelled)` helper.

**Rationale:** Tier 3 of the subagent deep-dive observed that "inbox state" was an ad-hoc slice+mutex pair inlined on `Agent`, with the empty-content guard duplicated inline in `AppendInbox`. Promoting it to a `mailbox` type gives the queue one clear owner, makes the empty-content invariant a single enforced point (`mailbox.append`), and makes the store unit-testable in isolation (concurrent append/drain under the race detector) without spinning up a full Agent. This is the conservative slice of the "Mailbox abstraction" idea: it unifies the *store* without entangling turn-trigger/`isRunning` semantics, which deliberately stay on the Agent (merging those would have widened the change surface for little gain). Tier 2 #5: the `[tool calls only]` / `[response cancelled]` literals were inlined in `executeTurn`; hoisting them to constants behind a pure helper removes the duplication, documents intent, and makes the selection logic directly testable.

**Consequence:** `Agent` loses the `inbox []sdk.Message` and `inboxMu sync.RWMutex` fields, gaining `inbox mailbox`. No public API change — `AppendInbox`/`DrainInbox`/`InboxLen` keep their signatures and behavior. New unit tests: `mailbox_test.go` (append/drain/len, empty-content drop, concurrent append/drain) and `placeholder_test.go` (the four placeholder cases). `mailbox` must not be copied after first use (holds a mutex); it is only ever embedded in `*Agent`, which is pointer-referenced, so `go vet` copylocks stays clean.

---

## 26. Provider-request interception (interceptor contract phase 2)

*Added: 2026-06-30*

**Decision:** Add `ProviderRequestInterceptor` and `AgentPool.SetProviderRequestInterceptor`. `executeTurn` runs the `before_provider_request` transform chain (via a local `buildStream` helper) immediately before streaming: an interceptor can redact the outgoing messages, reroute the model, or block the request. The harness installs the interceptor in `SetProgram`, routing to `extHost.DispatchEventChain` (avoiding an agent→extension circular import). `wllrsdk.go` gains `OnInterceptProviderRequest`. The old observe-only `before_provider_request` dispatch in `submitToAgent` is removed — interception now happens at the real provider-call site where messages + model exist.

**Rationale:** Phase 2 of the interceptor-contract design (docs/plans/2026-06-30-interceptor-contract-design.md), covering the PII-redaction and cheap/frontier-routing use cases. The prior `before_provider_request` event fired in `submitToAgent` was decoupled from the turn and observe-only — it could not edit the messages or change the model, and it ran before the agent built the actual request. Moving it into `executeTurn` is the design's "real plumbing" wrinkle: that is where the message slice and model are materialized, so it is the only place a redaction/reroute can affect the genuine provider call. `buildStream` is written so the no-interceptor path is byte-identical to before (history+content unchanged), and only folds content into the message list when an interceptor is actually installed — keeping the overwhelmingly common case allocation- and behavior-neutral.

**Consequence:** `AgentPool` gains a `providerRequestInterceptor` field (guarded by `dispatchMu`, like the other dispatchers) plus `interceptProviderRequest`/`hasProviderRequestInterceptor`. New `ProviderRequestInterceptor` type and `ProviderRequestBlockedError` (in providerintercept.go). Redaction is send-time only — history keeps the original content (asserted by `TestProviderIntercept_RedactPreservesHistoryOriginal`). Reroute rebuilds the turn's `fantasy.Agent` from `pool.LanguageModelForModel`; a build failure falls back to the original model. A block fails the turn with `*ProviderRequestBlockedError` through the normal `finishTurn` error path. Tests in providerintercept_test.go: block, no-interceptor passthrough, redact-preserves-history, reroute-requests-new-model. The harness `submitToAgent` no longer dispatches `before_provider_request` (moved into the turn); `before_agent_start` is unchanged.

---

## 27. Runtime model switching — Agent.SetModel

*Added: 2026-06-30*

**Decision:** Add `Agent.SetModel(lm fantasy.LanguageModel, modelName string, contextWindow ...int64)` and guard the LM, model name, and resolved context window with a new `lmMu sync.RWMutex`. `Submit` captures the complete model snapshot under the lock so compaction cannot size a turn for a different model than the LM that streams it.

**Rationale:** `/model` was cosmetic — it updated the status display but never changed the model the main agent actually ran (the LM was fixed at spawn). The new model picker needs to genuinely switch the running model. `SetModel` swaps both the LM and the name atomically so the next turn uses the new model, while a turn already in flight finishes on the model it captured. Previously `lm`/`modelName` were read without synchronisation (safe only because they were write-once at spawn); making them mutable at runtime requires the mutex to avoid a data race with in-flight `Submit`/`executeTurn` reads. The provider-request reroute path (§ Provider-Request Interception) already rebuilt the LM per-turn locally; `SetModel` is the persistent counterpart driven by the user rather than an interceptor.

**Consequence:** `Agent` gains `lmMu`; `ModelName()` and `ContextWindow()` are locked accessors. The harness `SelectModelFn` calls `SetModel` with the resolved model-specific window, updates the per-model pool metadata, and persists user-entered values. Covered by model snapshot and per-model context tests.

---

## 28. Runtime provider-options switching — Agent.SetProviderOptions

*Added: 2026-06-30*

**Decision:** Add `Agent.SetProviderOptions(fantasy.ProviderOptions)` and a `providerOpts` field guarded by the existing `lmMu`. It is seeded from `opts.ProviderOptions` at spawn; `Submit` snapshots it under `lmMu.RLock()` (alongside `lm`) and assigns the snapshot to the per-turn `opts.ProviderOptions`.

**Rationale:** The `/thinking` picker needs to change the reasoning level (Anthropic thinking budget / OpenAI reasoning effort / Gemini thinking budget) of the already-running main agent, not just at spawn. Provider options were previously write-once via SpawnOpts. Reusing `lmMu` (rather than a new mutex) keeps the "model + its request options are swapped together, atomically, between turns" story in one place and matches the SetModel design (§27). A nil value clears options so "thinking off" fully removes the provider option rather than sending a zero budget.

**Consequence:** `Agent` gains `providerOpts`; `Submit` reads it under `lmMu` and overrides the captured `opts.ProviderOptions` for the turn. Spawn-time behavior is unchanged (providerOpts seeds from opts.ProviderOptions). The harness `SelectThinkingFn` (cmd/main.go) maps a level → provider options (cmd/thinking.go) and calls `SetProviderOptions` on the main agent. Covered by `TestSetProviderOptions_AppliedToNextTurn`.

---

## 29. Spawner tool-call observer for sub-agent UI attribution

*Added: 2026-07-02*

**Decision:** Add `Spawner.SetToolCallObserver` so harness code can receive sub-agent tool-call starts with the spawned agent ID.

**Rationale:** Sub-agent tokens should remain silent in the main transcript, but hiding their tool starts made the tool activity pane and logs hard to reconcile with the subagents window. The host already reports completions through `AfterToolCall`; starts needed equivalent attribution.

**Consequence:** The observer is optional and nil-safe. Existing spawner behavior is preserved unless the harness installs an observer.

---

## 30. Intra-turn activity snapshots for sub-agent liveness

*Added: 2026-07-04*

**Decision:** Add `Agent.Activity()` and track turn start, last activity, last tool call, active/last tool name, and graceful shutdown request state on each agent.

**Rationale:** Orchestrators were treating unchanged completed-turn history as evidence that a sub-agent was stuck, even while the sub-agent was actively executing tools inside a long turn. Completed history is too coarse for supervising agentic work; liveness must include intra-turn signals such as text deltas, tool dispatches, and queued shutdown requests.

**Consequence:** `Agent` gains activity state guarded by `activityMu` plus an atomic shutdown-request flag. Activity updates are observational and do not affect turn execution, inbox ordering, or history. Status surfaces can now distinguish "running and recently active" from "running but quiet for a long time" without pinging the child.

*Addendum (2026-07-05):* Tool completion is now recorded via `MarkToolCallDone`, which updates `LastToolDoneAt` and clears the active tool fields when the completed tool matches the active call. A running turn remains "working" for status purposes; a completed tool only means the agent moved past that tool, not that the child is idle or stuck.

---

## 31. Compaction observability — counters, cost, and triggers

*Added: 2026-08-24*

**Decision:** `compactHistory` returns a `CompactionResult` (history, summary, messages folded in, summarization-call usage, latency, trigger kind) and `executeTurn` calls `Agent.observeCompaction(result)` after each run, which increments a per-session `compactionCount` and emits one structured `slog.Info` record per successful compaction.

**Rationale:** Compaction successes were previously invisible — only failures logged, and the summarization LLM call's token cost was discarded. Operators could not tell how often autocompaction fires, what it costs, or which trigger (heuristic estimate vs usage threshold vs reactive context-limit retry) fired.

**Consequence:** `didCompact` is derived from `Summary != ""` rather than assumed true, so no-op compactions (history fit the budget, or no valid user boundary) never count, never log, and never set `Compacted` in the `EventContextUsage` payload. The counter is turn-goroutine-local (one turn at a time — no lock needed) and monotonic for the session lifetime. `EventContextUsage` gains an additive `compactions` field (omitempty) for extension charts. The `compaction_summary` stream is a separate `fantasy.NewAgent(lm)` call — its usage is reported from `res.TotalUsage` and is not folded into the turn's `lastUsage`, so compaction cost does not skew the percentage trigger.

*Addendum (2026-10-05):* Compaction became user-visible and the fake markers are gone. Successful main-agent compactions now dispatch an immediate `EventContextUsage` with a non-nil `CompactionNotice` (trigger, messages folded, optional post-compaction chars/4 estimate) — the harness turns that into a chat notification (`EventNotify`: "🧹 Context compacted: N message(s) summarized (trigger: …)") and an up-to-date statusline number while the turn is still running. The end-of-turn dispatch now passes a nil notice, replacing the old turn-end `compacted` flag. The fake `onToken("[Compacting context…]")` / `"[Context limit reached — compacting and retrying…]"` markers were removed: they streamed as assistant tokens, so the session file recorded them as part of the model's reply. Each successful compaction is also recorded in the canonical transcript ("[Context compacted: N messages summarized …]") so recall can surface that older detail was folded away. The bundled history extension records a `compaction` JSONL entry (metadata only — resume replays `message` entries exclusively) when it sees the post-compaction event. Separately, `MainAgentContextUsage` now divides by the main agent's own resolved window instead of the pool's default-model window, so the displayed percentage always matches the window the agent's turns and compaction actually use.

---

## 32. Configured endpoints for local sub-agents

*Added: 2026-09-19*

**Decision:** The pool passes its current provider to the optional model factory on every model request, including requests without an endpoint. The command layer uses the requested local model's `wllr.local_models` entry to select its endpoint and API key. An explicit endpoint must match that entry.

**Rationale:** Local models can have different configured endpoints and credentials. Reusing the main agent's provider for a different local model would send the request to the wrong server, while accepting a free-form endpoint would bypass the configured model list.

**Consequence:** Unknown local model names and mismatched endpoints fail at spawn time. A configured local model can be selected by name alone. Non-local models continue to use the pool's current provider.

---

## 33. In-turn compaction for long tool loops

*Added: 2026-09-20*

**Decision:** Use Fantasy's `PrepareStep` hook to summarize the active message
transcript when the next provider step is nearing the model window. Keep a
rolling summary plus messages produced after it, without replaying tool calls.

**Rationale:** A turn can execute many tools after its initial compaction check.
Those tool results live inside Fantasy until the turn finishes, so compacting
the agent's persisted history after a context error cannot shrink that active
transcript. The previous provider usage plus a conservative estimate of newly
appended tool results provides a timely trigger.

**Consequence:** The summary request is bounded and charged separately from
the turn. A failure stops the turn before the next provider call. The summary
is used within the current tool loop; the ordinary history recording path
continues to own future-turn context.

---

## 34. Host-installed sub-agent model resolver

*Added: 2026-09-21*

**Decision:** Add `AgentPool.SetSubagentResolver` and `ResolveSubagentModel`. A
spawn request that names a model resolves through the existing `modelFactory`;
only an omitted model consults the resolver. The resolver returns both the
`LanguageModel` and the resolved model name.

**Rationale:** Model tiers (issue #43) let the user tag a cheap "low" model on a
different provider than the session model, so sub-agents can run cheaply while
the main agent plans on an expensive one. The pool's `modelFactory` is bound to
the session provider and cannot build a model from another provider; expressing
cross-provider defaulting inside the pool would mean importing provider
construction into `modules/agent`. Returning the resolved name alongside the
model is required because `Spawner` derives the compaction window from the model
name — a tier model must size against its own window, not the session model's.

**Consequence:** Explicit `model` in `create_agent` keeps winning (checked
before the resolver), and a nil resolver reproduces the previous default-model
behavior exactly. The host (`cmd`) owns tier lookup and provider construction,
so the agent package stays free of provider-specific imports.

*(Follow-up, same day.)* The host rejects a tier model with no resolvable
context window instead of spawning it: `Spawner` sizes compaction from the
resolved model name, so a window-less tier model would stream with incomplete
metadata. A local tier model resolves its window through
`resolveLocalModelWindow`, which runs endpoint discovery when the window is not
already known; a sub-agent tier that still cannot resolve falls back to the
session model rather than failing every spawn.

## 35. Plain-value observer seams for usage and lifecycle

*Added: 2026-09-22*

**Decision:** Report per-turn token usage and pool membership as plain value
types (`TurnUsage`, `AgentLifecycle`) through observer callbacks installed with
`SetUsageObserver` / `SetLifecycleObserver`. The agent package does not import a
metrics library; the host decides what to record.

**Rationale:** Prometheus metrics are a host concern, and the agent package is
deliberately dependency-light. The existing `SetContextUsageDispatcher` already
established this seam for the status bar, so usage reporting reuses the pattern
rather than introducing a coupling. Passing plain values also keeps the
instrumentation testable without a metrics registry.

**Consequence:** A completed turn reports a start signal and then a completion,
so in-flight turns are observable; a failed turn is reported as a turn with no
usage rather than dropped, so turn counts match what ran. Lifecycle events are
reported after releasing `p.mu` — an observer is host code and must never run
under the pool lock.

Two attribution fixes fell out of this work:

- A provider-request interceptor that reroutes a turn to another model now
  updates `modelName` for the turn, so usage (and the existing context-usage
  dispatch) is attributed to the model that actually served the request rather
  than the one the turn started with.
- The workflow previously reported usage only for the main agent; the observer
  now reports every agent, which is what makes per-sub-agent accounting possible.

## 36. Sub-agent token observer

*Added: 2026-09-22*

**Decision:** Add `Spawner.SetTokenObserver`, which receives each sub-agent's
streamed text together with the agent that produced it. When unset, sub-agent
tokens stay discarded as before.

**Rationale:** Sub-agent output was routed to a no-op (`SetOnToken(func(_ string){})`)
so only the main agent's text reached the transcript. Reading an agent's history
covers completed turns, but a running agent's in-flight text appears nowhere
until its turn ends. Keying the observer by agent ID lets a focused view render
live output while leaving the main transcript untouched.

**Consequence:** The harness creates one batcher per agent, so coalescing
windows are independent and a slow agent cannot delay a fast one. The batcher's
program field is optional: a nil program marks dispatch-only operation, which is
how sub-agent text reaches extensions without emitting a main-chat `TokenMsg`.
The per-agent flusher added later (§47) covers the tail those batchers held.

## 37. Close and Cancel cascade to descendants

*Added: 2026-09-22*

**Decision:** `Close(id)` removes `id` and its whole subtree; `Cancel(id)` stops
the in-flight turn of `id` and every descendant, leaving the agents in the pool.

**Rationale:** Previously both acted on a single agent, so stopping a parent
orphaned its children — they kept running work whose result had nowhere to go,
since their lifecycle target was gone. The agent tree is a hierarchy in which a
child exists only to serve its parent, so stopping the parent must stop the
subtree. Applying this to the root as well keeps the model uniform: the root is
the top node, not a privileged one.

**Consequence:** Descendants are identified by the same `"<parent>/<name>"`
convention `Spawn` uses to derive child IDs, tested with the separator so
`main/x2` is not treated as a child of `main/x`. `Descendants(id)` exposes the
set without acting on it, for callers that need to report the impact. Lifecycle
observers fire once per affected agent, so metrics see each removal.

## 38. Spawn must not start the first turn on the caller's stack

*Added: 2026-09-22*

**Decision:** `Spawner.Spawn` sends the initial prompt from a detached
goroutine rather than calling `pool.Send` inline.

**Rationale:** Spawn executes inside the spawning extension's WASM call — the
agents extension calls `create_agent`, which reaches `agent_spawn`, which reaches
Spawn. Starting the turn inline runs `onTurnStart` before Spawn returns, and the
host's turn-start handler dispatches an event back into that same extension. The
extension host serializes calls per extension with a non-reentrant mutex, so the
inner dispatch waits on a lock the outer frame holds and cannot release. The
result is a permanent deadlock that also blocks every later call into that
extension (observed as `create_agent` and `list_agents` hanging forever, three
goroutines stuck 26 minutes).

**Consequence:** Turn start is detached, so the first turn begins just after
Spawn returns. Callers that need the turn to have started before returning must
not assume it. Fixing this at the source protects every `onTurnStart` callback,
not just the prompt observer that exposed it.

**Note:** `go test -race` cannot detect this class of bug. It is a deadlock, not
a data race — all access is correctly synchronized through a mutex that is simply
never released. The regression test asserts liveness (Spawn returns while the
callback contends on a held lock), which is the only reliable detector.

## 39. Protocol messages are model-visible but not conversation

*Added: 2026-09-22*

**Decision:** Add `sdk.MessageTypeProtocol` for the lifecycle notifications the
host sends to a parent (`agent_idle`, `agent_failed`). They are delivered as
before — model-visible — but the transcript skips them.

**Rationale:** These were delivered with no type, which makes them `normal`.
That is right for the model: the orchestrator must see `agent_idle` to learn a
child finished, and it is what wakes the parent. It is wrong for the transcript,
which rendered each one as a user bubble containing raw JSON. Focusing a
sub-agent therefore showed a wall of `{"event":"agent_idle",...}` envelopes
instead of the conversation.

Neither existing type fits. `system` is not recorded in history and never
reaches the model, which would break orchestration. `steering` is recorded but
filtered from the LLM context, which would also break it. Protocol is the third
case: reaches the model, stays out of the transcript.

**Consequence:** `sdkToFantasyMessages` deliberately does **not** filter
`protocol` — that is the whole point. The transcript filters it in three places
(history replay, the main agent's turn-start dispatch, and the sub-agent prompt
observer), so a lifecycle envelope can never be mistaken for a user prompt.

## 40. Nothing agent-side may call back into the host on a host call's stack

*Added: 2026-09-22*

**Decision:** Every agent callback that can dispatch into the extension host —
turn start, token, context usage — runs on its own goroutine, never inline on the
caller's stack. `Submit`'s turn goroutine waits for the start callback so output
ordering is preserved.

**Rationale:** This is the third deadlock of the same shape. `Submit` is
reachable from a host call (`agent_deliver`, `agent_run`, `agent_send_message`),
so its stack may already be inside an extension's WASM call. The extension host
serializes per extension with `callMu`, which is not reentrant and cannot be:
WASM linear memory is shared, so parallel calls would race on SDK globals. Any
callback that dispatches from that stack therefore waits on a mutex held by its
own caller, and the process wedges. Fixing call sites one at a time kept missing
instances (Spawn's initial turn, the token flush, now turn start), so the
invariant is stated once and enforced where the callbacks are invoked.

**Consequence:** A caller of `Deliver`/`Send` may observe the turn beginning
just after it returns rather than before. Nothing reads turn state back through
those calls, so this is not load-bearing. Ordering between the prompt and the
assistant's output is preserved by the turn goroutine waiting on the start
callback.

**Testing note:** `-race` cannot detect this. It is a deadlock, not a data race —
all access is correctly synchronized through a mutex that is never released.
Tests assert liveness instead: a call must return while a lock the callback needs
is held. That assertion fails in seconds when the inline call is reinstated.

---

## 41. Canonical transcript and recall — compaction is lossy, retrieval is not

*Added: 2026-09-23*

**Decision:** Keep an in-memory, append-only **canonical transcript** on each
`Agent` alongside `a.history`, and expose it to the model through a `recall`
tool. Compaction continues to rewrite `a.history` exactly as before; it never
touches the transcript.

**Rationale:** Compaction is deliberately lossy — that is what makes a long
session runnable — but it was lossy with no recovery path. `executeTurn` does
`history = result.History; a.history = history`, so the originals were gone from
the agent's memory the moment a summary replaced them. A summary is the wrong
shape for exact detail: a model that needs the precise error text, command, or
path it saw forty turns ago cannot reconstruct it from prose, and asking it to
try produces confident invention.

The persisted session file (`extensions/history`) was the only surviving record,
but it was insufficient in two ways and unsuitable in a third. It recorded only
the root agent's messages, and only tool *inputs* — never tool output — so an
exact command result or error message was unrecoverable even from disk. And it
lives behind a WASM extension in a separate `wasip1` module, which is the wrong
place for something the turn path needs to read.

This follows the VCC idea the issue cites: keep the canonical transcript intact
and searchable, and let the model retrieve exact material with source pointers
rather than trusting a summary. The transcript is the retrieval source; the
summary stays the model's working context.

**Why in-memory rather than reusing the session file.** The transcript is written
where conversation is recorded — the same two points in `executeTurn` that write
`a.history` — so the two can never disagree about what constitutes conversation,
and recording needs no file I/O, no parsing, and no sandbox access. It also
extends the record where the file was weakest: tool results.

**Why the tool lives in `modules/agent`.** The agent package already imports
`fantasy`, so the recall tool is a `fantasy.AgentTool` built over the agent's own
transcript with no new dependency and no cross-module callback. Registering it as
a host native tool would have forced the host to reach into a running agent's
state, and routing it through a WASM extension would have put the transcript on
the far side of an ABI. The harness appends it per agent in `withRecallTool`,
which covers sub-agents as well as main because every agent compacts.

**Why the budget is a hard bound, not an estimate.** An unbounded recall would
reintroduce the exact failure compaction exists to prevent, so `RenderRecall`
reserves space for its own header and footer before filling content and clamps
the assembled result. The test asserts the invariant directly (`len(out) <=
budget*4`) rather than asserting that truncation happened. A non-positive budget
falls back to the default instead of meaning "unbounded" — an unresolved context
window is not a licence to dump the session.

**Consequence:** `Agent` gains `canonical`/`canonicalMu` and
`CanonicalTranscript()`; the transcript is created lazily so a zero-value `Agent`
constructed in a test behaves like a spawned one. `streamTurn` gains an
`OnToolResult` callback (the tool-result half was previously discarded) and
`toolResultText` extracts text, error, or media-accompanying text. History
recording is unchanged; the transcript mirrors it and additionally records tool
calls and results. `withRecallTool` appends the tool unless an extension already
registered the name, so an extension keeps ownership of `recall`.

**Deliberately out of scope:** persisting the canonical transcript to disk (the
session file already covers the durable, user-facing record), surfacing recall
in the UI beyond the existing tool-call pane and compaction notice, and
transcript-scoped recall for the open `#42` acceptance item about the statusline.

## 42. Clearing the inbox is ungated; index edits are not (2026-09-24)

Issue #48 asked for a way to discard queued messages from the UI, which
required a bulk clear that works while a turn is running — the exact window in
which a user looks at the queue pane and changes their mind.

**Decision:** `Agent.ClearInbox` / `mailbox.clear` are deliberately **not**
gated on `IsRunning`, unlike `DeleteFromInbox` and `EditInboxMessage`. The gate
on the index-based methods exists because a by-index target is only meaningful
against a quiescent snapshot — mid-turn, the snapshot the user is looking at
may already have been drained and replayed, so an edit/delete by index can hit
the wrong message or silently no-op. Clearing has no such target: it discards
*everything*, and the safety argument is structural rather than temporal.
`Submit` drains the inbox at turn start; the post-turn drain re-checks it at
turn end. A concurrent clear simply makes both drains observe an empty queue,
so cleared messages are never replayed into a later turn. The only loss case
is a message appended in the same instant as the clear — acceptable for a
discard operation, and unavoidable without cross-operation transactions the
UI does not need.

**Cancel semantics (issue #48 option (a)):** Esc-cancel deliberately preserves
the queue. `finishTurn` skips the drain on canceled turns (failed turns now drain
like successful ones — see the §17 addendum), so after a cancel the queued messages
stay visible in the pane and the user — not
the harness — decides: drain them into the next submit (pre-existing
`Submit` behavior), or discard them via ctrl+x / the `[ clear ]` button /
`/queue clear`. Auto-draining after a cancel (option (b)) was rejected because
it re-triggers a turn immediately after an interrupt — exactly what Esc was
meant to stop; auto-clearing (option (c)) was rejected because Esc should not
destroy typed input the user cannot undo.

Covered by `inbox_clear_test.go` (cancel preserves the queue; clear works
mid-turn and nothing replays afterwards) and `mailbox_test.go`
(`TestMailbox_ClearVsDrainRace`: concurrent clear/drain lose and duplicate
nothing).

**Queue-cancellation guidance in the agent-identity suffix:** every sub-agent's
system prompt (Spawner.Spawn) now teaches the queue model — messages sent
mid-turn queue for the next turn — and the cancel path: `queue_peek()` lists
pending messages, `queue_cancel()` discards all (or `queue_cancel(index: N)`
one), and cancelled messages cannot be recovered. The identity suffix is the
one site every spawn path shares (agents extension, task-runner, teams), so
the guidance lives there rather than only in the create_agent prompt assembly.
The tools themselves are registered by the queue extension; the ownership rule
(self or descendants only) is enforced extension-side — see
extensions/queue/README.md.

**Stable inbox IDs + selective gating for mid-turn single-message cancel:**
subagents calling `queue_cancel(index: N)` on their own queue always failed —
an agent is by definition running when it makes the call, and the by-index
delete path was gated on `IsRunning`. Fix is two parts. (1) `mailbox.append`
assigns an ID (`q<n>`) to every message that arrives without one, so
`queue_peek` can hand the agent a stable selector. (2) `DeleteFromInbox` and
`EditInboxMessage` now gate only their by-index paths: indexes shift as
messages arrive and are only meaningful against a quiescent snapshot, while
IDs are stable — a mid-turn by-ID op either loses the race to the drain (finds
nothing) or wins (the drain misses the message); either way the outcome is
exact (delivered or cancelled/edited, never duplicated or replayed). The
pool-level blanket `IsRunning` gates were removed in favour of the Agent's
selective checks. Pre-existing bugs fixed along the way, all in the queue
extension: its local `queueCall` returned the raw host_call envelope instead
of the unwrapped Result (same bug the agents extension had fixed for
agent_list — every /queue command parsed zero values), `mailbox_snapshot`'s
`{"messages": [...]}` wrapper was parsed as a bare array, and `by_index` was
sent as a JSON string where the host expects a number (broke `/queue delete`,
`/queue edit`, and single-message `queue_cancel`).

Covered by `inbox_clear_test.go` (by-ID delete and edit work while running;
by-index stays gated; mailbox assigns/preserves IDs), `mailbox_test.go`
(`TestMailbox_AppendAssignsIDs`), and the queue extension's native tests
(envelope unwrap, ownership rule, input validation).

## 43. Context breakdown buckets follow the request, not the transcript (2026-09-24)

The /context command needed an honest answer to "what is consuming my context?"
The tempting answer — walk everything including tool results — would have been
wrong twice over. First, `a.history` does not contain tool traffic: tool calls
and results go to the canonical transcript, and each Submit builds a fresh
fantasy agent, so between turns tool bytes are simply not in context (recall
is the recovery path, per §41). Second, system/steering messages sit in
history but are filtered by `sdkToFantasyMessages` before every request, so
counting them as context would overstate usage forever.

The breakdown therefore reports what the NEXT request will contain (system +
tools + user/assistant history, chars/4) alongside the provider-reported
`LastRequest` input, and reports the canonical transcript as
"recallable, not resident". The estimate/report gap is surfaced as a
reconciliation note rather than scaled away: scaling would silently attribute
within-turn tool traffic to buckets that do not contain it.

Covered by `contextbreakdown_test.go` (bucket math, filter exclusion, SpawnOpts
resolution parity with Submit, canonical-not-created, pool delegate,
window passthrough) and the harness's `contextmodal_test.go` (modal sections,
gap note thresholds, comma formatting).

*Addendum (2026-09-24):* per-tool, per-type, and per-message attribution was
added to the same snapshot. Tools get `ToolsByTool` (individually-rounded
chars/4, heaviest first — the aggregate stays total-chars/4, so per-tool
columns may trail it by up to ToolCount−1 tokens of rounding); history gets
`LargestMessages` (top-N heaviest user/assistant messages, so one pasted
document is visible above role totals — system/steering are excluded by `Type`
because a steering message carries RoleUser); and the canonical side gets
`CanonicalByKind` (messages vs tool calls vs results),
`CanonicalMessagesByRole`, and `CanonicalByTool` (tool traffic attributed to
the producing tool). The canonical attribution is chars, not tokens: the
transcript is never sent to the provider, so estimating tokens for it would be
a fiction. Nil maps mean a pre-attribution snapshot and render totals only.

## 44. The prompt ledger is honest because every mutation path updates it (2026-09-25)

Attributing the system prompt per source (built-in rules, prompt files,
AGENTS.md, skills list) had two candidate designs: ask each extension to
label its own appends, or label at the host. The host-side design won: the
append_system_prompt handler already knows the calling extension, so the
host passes its name to `AppendSystemPromptFrom` — no extension cooperation,
no SDK copy churn, and old extensions get labeled automatically.

The subtle part is session-start ordering. Extensions dispatch
alphabetically (agents → context → skills), and `SetBaseSystemPrompt`
REPLACES the base prompt, so the agents extension's early appends are
clobbered by the prompt extension's Set. The ledger must tell the same
story: Set resets it (plus a fallback component), the prompt extension's
`set_system_prompt_components` report replaces it with the real
decomposition, and later appends add entries. Whatever survived into the
real prompt is exactly what the ledger describes. (The clobbering itself is
pre-existing behavior — the /context modal now makes it VISIBLE: if the
agents guidance is not in the prompt, it is not in the ledger.)

## 45. The loop guard nudges; it does not force (2026-09-29)

Looping behavior (the same tool call or a short call cycle repeating with no
progress) burns real tokens and stalls sub-agents that an orchestrator must
then babysit. The fix lives entirely at fantasy's PrepareStep seam — the same
one the tool-loop compactor uses — because that is the only place wllr can
both SEE the completed steps of the current turn and CHANGE what the model
receives next.

Three design points worth recording:

1. **Injection does not persist, and that is the feature.** Fantasy applies
   PrepareStepResult.Messages to that step's request only; the ongoing
   conversation accumulates only assistant+tool-result content. So an
   injected reorient message reappears on every step while the loop
   continues and vanishes the moment the model breaks the pattern — no
   stacking, no cleanup, no state.
2. **Input-sensitive matching is the false-positive defense.** Keying on
   (tool name, normalized JSON input) means the everyday
   read→edit→read→verify rhythm never fires (its edit inputs differ), while
   a stuck model re-sending the identical payload does. The consecutive
   rule (two identical calls in a row) is aggressive by intent: with no
   intervening call there is no state change to justify an identical retry.
   The escalation ladder stays advisory: nudge first, never force. A
   DisableAllTools force-stop was considered and rejected for now — with
   tools suppressed, a model that still emits a tool call takes fantasy's
   suppressed-call path, whose loop interaction is unverified.
3. **Ordering with compaction matters.** The guard runs AFTER the tool-loop
   compactor, so when compaction replaces the outgoing list with a summary
   the guard message is appended on top instead of being discarded. Both
   signals coexist: "your context was compacted" and "you are looping" —
   which is exactly when a model most needs to reorient.

Cross-turn loops (agent ends its turn, gets re-prompted, repeats the same
tool sequence across turns) are out of scope here; they need a
cross-turn memory the per-turn guard does not have.

**Addendum (2026-09-29, same day): the guard moved to the agents extension.**
The advisory injection worked (proven live) but the review of the extension
architecture showed a better seam: the agents extension's `before_tool_call`
interceptor can DENY the call outright — the proven permissions enforcement
path — which upgrades nudge→force and puts the behavior where all agent-facing
policy lives (and where per-extension config fits the config-isolation
architecture). The pool-level injection code was removed; the detection logic
(normalization, consecutive + cycle rules) was ported to
`extensions/agents/loopguard.go` with two deliberate changes: the consecutive
threshold defaults to 3 (not 2) because a denied call is stronger than a
nudge — one identical retry after a transient failure should be forgiven —
and denial is scoped/configurable (`scope: all|subagents`). The blocked call
is rolled back out of the buffer so denial retries cannot consume window
slots. The fantasy PrepareStep seam remains correct for future in-band
message shaping; this guard no longer uses it.

---

## 46. Tokens-per-second measures the provider stream, not the turn (2026-09-29)

The statusline shows generation speed as tokens/second. The naive denominator
— turn wall-clock — is wrong in exactly the cases users care about: a turn
that calls three tools and waits on two sub-agents would report a fraction of
the real generation speed, and the number would swing with unrelated work.
The fix is span-based timing pinned to fantasy's step lifecycle, which is the
only seam that knows exactly when a provider stream starts and ends
(`OnStepStart`/`OnStepFinish`, invoked around each provider call at fantasy
agent.go's step loop).

Decisions worth recording:

1. **The silent tail is excluded.** A closed span contributes only up to its
   last token. A stream that stalls mid-response is not generating during the
   stall; counting it would understate speed exactly when something is wrong.
2. **The step's chars estimate dies with its span.** The first implementation
   kept `spanChars` after `OnStepFinish`, so the live rate double-counted
   every completed step (est. chars/4 on top of the step's real output
   tokens) — halving the displayed rate. Resetting on close, plus a separate
   never-reset `totalChars` for the no-usage fallback, is the accounting that
   survives both mid-turn reads and end-of-turn freezing.
3. **`max`, never `+`, when usage is missing.** The chars/4 fallback covers
   ALL spans; adding it to partially reported steps would double-count. The
   estimate is only ever a stand-in for absent usage.
4. **Frozen, not cleared, at turn end.** Like the ctx display, the last
   measured rate stays visible while idle; zero only hides (no measurement,
   sub-100ms span, instant fake stream). The 100ms floor keeps a microsecond
   test double from reporting 100,000 t/s.
5. **Own mutex (`streamTpsMu`).** Usage (`lastUsageMu`) and speed are written
   at different points of the turn and read by different tick paths; sharing
   a lock would couple them for no benefit.

The harness poll (100ms tick → `pool.MainAgentTps()` → `"tps"` status key)
and the wasm `sl-tps` segment are pure consumers; all timing lives here.

**Addendum (2026-10-06): the sparkline answers "what is it doing right now",
the number answers "how fast overall".** The cumulative average converges and
cannot show a burst or a stall, so the tracker also keeps a per-second
history of the trailing 1s windowed rate (chars/4 over recorded token-arrival
samples) and renders it as block bars appended to the same `"tps"` status
value — one status key, so old wasm copies render the bars with zero
extension changes. Sampling is driven by the existing UI tick (at most one
sample per second of poll time, 20 kept), happens only while a span is open
with a token (between spans the bars freeze — gaps are not generation time,
same rule as the number's denominator), and the bars freeze at turn end next
to the number (`lastSpark`, same `streamTpsMu`). Bars scale to the window
max, so the shape is relative speed; a flat-zero stretch is the visual
signature of a stall.

**Addendum (2026-10-06, later still): sampling is wall-clock.** The original
rule sampled only while a span was open with a token, freezing the bars
during TTFT and tool gaps — but "no tokens" is exactly when the user watches
the sparkline, and a frozen frame reads as a broken render, not as idle.
Sampling now runs whenever the turn is active: each silent second records a
zero sample and the bars scroll off in real time. The number keeps its
span-based denominator; the bars are the wall-clock view. Visibility during
pre-token silence stays with the harness (`formatTpsLive` hides the segment
while the number is zero).

---

## 47. Sub-agent token flush at segment boundaries and turn end (2026-10-06)

**Decision:** Add `Spawner.SetTokenFlushObserver(func(agentID string))`. The
spawner invokes it when a sub-agent dispatches a tool call and unconditionally
when the sub-agent's turn ends. The harness implements it by flushing that
agent's token batcher, so a segment's final <75ms of tokens reach the focused
transcript immediately instead of waiting for the next segment — or, for the
last segment, never arriving at all.

**Rationale:** The dispatch-only batchers (`dispatchSegmentedTokens`) only
send when a token arrives ≥75ms after the previous send. A sub-agent response
streamed word-by-word holds its last words in the buffer at turn end, and the
batcher's flush was previously **discarded** (`onToken, _ :=`), so nothing
ever delivered them: the focused transcript rendered the response permanently
one fragment short (the same cut-off-mid-sentence symptom seen on the main
agent, but permanent rather than delayed). Flushing at tool-call dispatch also
fixes the mid-turn lag: narration before a long tool call stays incomplete
until the tool finishes otherwise.

**Consequence:** `Spawner` gains the `tokenFlushFn` field, setter, and two
invocation sites (OnToolCall wrapper, OnDone wrapper before the error check).
`dispatchSegmentedTokens` returns `(dispatch, flushAgent)` and records each
agent's flush. The main agent needs no new API — `wireMainAgentCallbacks`
already owned its batcher and flushes it via `toolCallForwarder`. Regression
coverage: `TestSpawnerTokenFlushDeliversTailAtTurnEnd` (complete text reaches
the token observer), harness `TestTokenBatcher_HoldsTailUntilFlush`,
`TestToolCallForwarder_FlushesTailBeforeToolCall`, and
`TestWireMainAgentCallbacks_TurnFlushesTailAtToolCall` (real turn: full text
delivered before the ToolCallStartMsg).

## 48. /steer delivers mid-turn by re-injecting on every step (2026-10-06)

**Decision:** Steering messages (`MessageTypeSteer`, `/steer <text>`) are
delivered into the running turn through the fantasy `PrepareStep` hook rather
than a new turn. `steerInjector` (steer.go) wraps the tool-loop compactor's
prepare: it first drains only steer-typed inbox messages, then lets the
compactor run (so compaction still works and steers survive it by being
appended after the compacted payload), and appends every steer delivered this
turn to the outgoing request — **on every subsequent prepare**, not once.

**Rationale:** fantasy's agent loop rebuilds each step's input from the fixed
initial prompt plus messages accumulated from step content
(`stepInputMessages := append(initialPrompt, responseMessages...)`). An
injected message exists only in the request it was prepared for; if prepare did
not re-add it, the steer would be visible to step N but vanish from step N+1 —
the model would "forget" mid-turn guidance one tool call later. Re-injecting
from the injector's own accumulated list keeps the guidance present for the
rest of the turn, and `executeTurn` records that same list into history and the
canonical transcript afterward, so the persisted position (between the turn's
prompt and its response) matches where it entered context.

Two drain rules matter. Only steer-typed messages are taken at the step
boundary: the inbox may simultaneously hold a `shutdown_request` whose handling
lives in finishTurn — a full drain there would strand shutdowns forever. And
the injector copies fantasy's message slice before appending: the slice is
backed by fantasy's own array, which it reuses across steps, so in-place
appends corrupt the next step's input.

**Consequence:** `sdk` gains `MessageTypeSteer`; `mailbox` gains
`drainSteer` (selective drain, order-preserving); `Agent` gains
`SetOnSteer` + `onSteerFn` and `streamTurn` takes a `*steerInjector` (two
existing test call sites updated); the spawner gains `SetSteerObserver` so
focused sub-agent transcripts render deliveries. The idle path needs no new
code: steer is model-visible, so Submit's normal drain delivers it as
conversation, and `allControlMessages` correctly does not treat it as control.
Coverage: steer_test.go (selective drain, type survives Submit's requeue,
end-to-end delivery at a gated tool boundary with history/canonical/onSteer
assertions, idle-agent consumption, per-step re-injection without input
mutation).

**Addendum (2026-10-06, live ctx mid-turn):** `lastUsage` used to be written only at turn
end, and `EventContextUsage` only dispatched there — so a session whose main turn ran for
hours (an orchestrator in a sleep/status-poll wait loop) never showed the statusline ctx
segment at all, and even normal sessions froze the number at the previous turn boundary
until the next one completed. `streamTurn`'s `OnStepFinish` now calls
`observeStepUsage(step.Usage, pool, contextWindow)` after every provider step: the stored
value keeps the peak-within-turn rule (grow-only on the input side, mirroring
`contextUsageFromResult`), and the main agent dispatches `EventContextUsage` per step so
the extension learns the window and the live value mid-turn. Ordering (probed): step N's
`OnStepFinish` fires after step N's tools complete and before step N+1's tools start, so
the refresh lands between an orchestrator's tool calls. Failed turns retain the failed
attempt's peak — a step that actually ran did occupy that context, matching the existing
retain-on-error philosophy. The harness paints the segments from its 100ms stream tick
(see harness NOTES), so ctx appears within ~1s of the first step instead of at turn end.
Coverage: observestepusage_test.go (mid-turn liveness through a two-gate scripted turn,
sub-agent no-dispatch guard, zero-usage/peak-retention rules).

## 49. Hard kill is Close; the root is uncloseable (2026-10-06)

`pool.Close(id)` is the hard kill the /agents tree's `x` key (and the
`agents:kill` extension command) route to: it removes the agent **and its whole
subtree** from the pool immediately and cancels every member's turn context, so
in-flight tool calls that honor `ctx.Done()` (all real tools do) end at once.
That is a different contract from `shutdown_request`, which queues a control
message and lets the agent finish its turn and self-close gracefully. Two rules
make the kill safe to expose in the UI: (1) the root agent is rejected with
`ErrRootAgentClose` — it owns the session lifecycle and exits with the harness,
so no tree selection or team close can remove it (the graceful path never hits
this because the root has no creator to send it a shutdown); (2) descendants
die with the parent because their work has nowhere to go. `Cancel(id)` remains
the non-destructive variant: turns stop, agents stay in the pool. Tests in
kill_test.go prove the turn-interrupt (a blocked ctx-aware tool returns on
kill), the subtree cascade including mid-turn descendants, and the root guard.

## 50. Fresh-session ctx seeds a chars/4 baseline instead of 0 (2026-10-06)

`lastUsage` was zero until the first provider step reported, so a fresh session's
statusline showed ctx 0 (or nothing) even though the system prompt, tool definitions,
and the user's prompt were already on the wire — the operator reasonably read 0 as a
calculation bug. The fix reuses the estimator the compaction path already trusts:
`executeTurn` calls `seedUsageEstimate(contextEstimate(...))` before each `streamTurn`
attempt while `usageReported` is false, storing a chars/4 baseline as `lastUsage` and
dispatching it (main agent) so the extension learns the window at turn start, not
step 1.

Two rules make the seed honest rather than sticky:

1. **Replacement, not peak.** The seed intentionally overestimates (chars/4 — the same
   bias that keeps compaction safe). A peak rule against it would pin the display high
   until real usage grew past the overestimate. So the first real `observeStepUsage`
   replaces the stored seed outright (`usageIsEstimate` flag), and only subsequent
   steps peak.
2. **Seeding is one-shot per session.** Once real usage exists, seeding is permanently
   off. Re-seeding at every turn start would bounce the display up to the overestimate
   each turn before step 1 corrected it; the previous turn's final reported input is
   already a good stand-in (context persists across turns), and it is replaced within
   one step anyway.

Failed/cancelled first turns retain the seed (the retained-value rule stores the best
known value and `markUsageReported` is not called), so the next attempt re-seeds —
still correct, since nothing authoritative ever arrived.

The `/context` modal would otherwise have labeled the seed "provider-reported";
`ContextBreakdown.LastRequestIsEstimate` distinguishes the two and the modal says
"chars/4 baseline — no provider report yet". The seed also feeds the usage-threshold
compaction trigger via `LastUsage()` — correct by construction: if the baseline
estimate really is over 80% of the window, the heuristic trigger
(`shouldCompactWithTools`, same formula) would fire anyway.

Deliberately out of scope: seeding at spawn time. Before the first `Submit` there is
no resolved tool set (`toolsFn` may not be installed) and no prompt — the estimate
would be fiction. A fresh idle session shows no ctx segment until the first turn
begins, which is truthful: nothing has been sent yet.
