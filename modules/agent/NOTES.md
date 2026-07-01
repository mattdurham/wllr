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

**Decision:** Each `Agent` stores a `modelName string` field, set from `pool.defaultModelName` at spawn time. `contextWindowForModel` maps model name substrings to known context window sizes and is called per-turn in `Submit`.

**Rationale:** Different model families have very different context windows (128k vs 200k vs 1M tokens). A single hardcoded constant would either be too conservative (wasting compaction on large-context models) or too aggressive (skipping compaction on small-context models). The per-agent field allows sub-agents spawned with a different model to compact at the correct threshold. The substring-matching approach avoids maintaining an exhaustive model name registry while covering the most common cases.

**Consequence:** Unknown model names fall back to `defaultContextWindow` (1,000,000). Extensions that spawn sub-agents with exotic model names should pass the model name explicitly; otherwise the fallback is used.

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

**Decision:** Compaction uses a two-phase approach: proactive compaction (before the API call) triggers when estimated tokens exceed the window minus the output reserve; reactive trimming (after a 400 context-too-long error) trims to the most recent 20 messages and retries once.

**Rationale:** Proactive compaction avoids the round-trip cost of a failed API call. However, the `chars/4` token estimate is intentionally approximate and may undercount; the reactive path handles cases where the estimate was too optimistic. The two phases together provide safety coverage without over-compacting on every turn. The reactive path is a blunt trim (not summarization) because the API already rejected the request — a second LLM call for summarization might itself hit the limit; trimming is guaranteed to reduce size.

**Consequence:** In the worst case, one extra failed API call is made per compaction event on the reactive path. Context from messages older than the most recent 20 is lost permanently on the reactive path (no summary is generated).

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

**Consequence:** `keepMessages=20` is retained only for the reactive fallback (blunt trim
after a 400 context-too-long error), where speed and guaranteed size reduction take
priority over summarization quality.

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

## 18. Real API Token Tracking — Why Replace chars/4 Heuristic

*Added: 2026-05-30*

**Decision:** Replace the chars/4 token estimation for compaction decisions with real API
token counts from `fantasy.AgentResult.TotalUsage`. Store the last turn's usage on `Agent`
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

**Decision:** `streamTurn` now returns the `fantasy.Usage` from the `*fantasy.AgentResult` produced by `fa.Stream` (previously discarded with `_`). `executeTurn` stores it via `a.setLastUsage(usage)` on a successful, non-cancelled turn (zero-valued on error/cancel) and, for the main agent only, calls `pool.dispatchContextUsage(sdk.ContextUsageFromFantasy(usage, contextWindow), didCompact)`.

**Rationale:** `setLastUsage` and `dispatchContextUsage` were defined and specified (see §19 and the sdk `EventContextUsage` contract) but never actually invoked — the streaming turn dropped the result's usage entirely, so `LastUsage()` always returned zero, `MainAgentContextUsage()` reported empty, and `EventContextUsage` never fired. This made the behavior diverge from the documented spec. The fix makes the runtime match the existing contract: usage is captured per turn, failed/cancelled turns report zero (never stale counts), and the dispatcher fires once per completed main-agent turn.

**Consequence:** `streamTurn` signature changed from `(string, error)` to `(string, fantasy.Usage, error)`. A `didCompact` bool tracks whether proactive compaction ran this turn and is forwarded as the dispatcher's `compacted` argument. Context-usage dispatch is restricted to `MainAgentID` so sub-agent turns do not overwrite the main context-window indicator. No public API of the agent package changed; the previously-zero `LastUsage()`/`MainAgentContextUsage()`/`EventContextUsage` values now carry real data.

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

**Decision:** Add `Agent.SetModel(lm fantasy.LanguageModel, modelName string)` and guard `lm`/`modelName` with a new `lmMu sync.RWMutex`. `ModelName()` reads under the lock; `Submit` captures `lm` under the lock; `executeTurn` snapshots `modelName` once via `ModelName()`.

**Rationale:** `/model` was cosmetic — it updated the status display but never changed the model the main agent actually ran (the LM was fixed at spawn). The new model picker needs to genuinely switch the running model. `SetModel` swaps both the LM and the name atomically so the next turn uses the new model, while a turn already in flight finishes on the model it captured. Previously `lm`/`modelName` were read without synchronisation (safe only because they were write-once at spawn); making them mutable at runtime requires the mutex to avoid a data race with in-flight `Submit`/`executeTurn` reads. The provider-request reroute path (§ Provider-Request Interception) already rebuilt the LM per-turn locally; `SetModel` is the persistent counterpart driven by the user rather than an interceptor.

**Consequence:** `Agent` gains `lmMu`; `ModelName()` is now a locked accessor (was a bare field read). No behavior change for spawn-time model assignment. The harness `SelectModelFn` (wired in cmd/main.go) calls `SetModel` on the main agent plus `pool.SetDefaultModelName` (so future sub-agents inherit it) and `pool.SetContextWindow`. Covered by `TestSetModel_SwapsModelForNextTurn` (next turn streams from the swapped LM).
