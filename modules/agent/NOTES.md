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
