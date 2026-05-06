# agent — Design Notes

Append-only design decision log. Never delete entries; add an `*Addendum (date):*` if a decision is reversed.

---

## 1. Sequential Inbox Drain — Why Not Concurrent Delivery

*Added: 2026-05-06*

**Decision:** Inbox messages are drained synchronously at the start of each `Submit` call (via `DrainInbox`) and prepended to `priorHistory` before the fantasy.Agent.Stream call. They are NOT delivered in a separate goroutine or injected mid-turn.

**Rationale:** The LLM provider receives a single ordered list of messages per request. Injecting inbox messages mid-turn would require pausing the stream and re-starting the provider request, which is not supported by fantasy's streaming API. Delivering them as prior-history context (prepended before the current prompt) is equivalent to the LLM having "seen" those messages in an earlier exchange — the correct semantic for inter-agent communication. Sequential drain also eliminates race conditions between concurrent senders: all senders use `AppendInbox` (which holds `inboxMu`) and all reads use `DrainInbox` (which atomically clears the inbox). No message is ever lost or duplicated.

**Consequence:** Messages sent to an agent's inbox after `DrainInbox` has been called (i.e., during an active turn) are queued for the next turn. This creates at-most-one-turn-latency for inbox delivery, which is acceptable for the inter-agent coordination use case.

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

**Rationale:** Different agents may require different handling: the main agent's tokens go to the TUI chat window; sub-agent tokens may be logged or discarded. A pool-level hook would need to multiplex by agent ID, which effectively recreates per-agent routing in the caller. Keeping callbacks on the `Agent` struct is simpler, allows different policies per agent, and avoids centralizing routing logic in the pool. The harness wires the main agent's callbacks in `SetProgram`; sub-agent callbacks are wired in the `OnAgentSpawn` closure in `cmd/main.go`.

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

**Rationale:** After the model.go refactor (Task #27), `harness.New` takes `(pool, mainAgentID, h)` — removing the `langModel` and `provName` parameters. The status bar still needs a provider name. Storing it on the pool is natural: the pool already owns the provider (via `SetProvider`), and the name logically accompanies it. This avoids adding a fourth parameter to `harness.New` solely for display purposes, and keeps the display name co-located with the provider.

**Consequence:** `cmd/main.go` must call `pool.SetProviderName(cfg.Provider)` before calling `harness.New`. The harness reads the name once at construction; runtime changes to the provider name are not reflected in the status bar without creating a new model.

---

## 6. Global token counter on AgentPool
*Added: 2026-05-06*
**Decision:** All agents share one atomic.Int64 token counter on the pool.
**Rationale:** The TUI status bar shows total tokens across all active agents. A per-agent counter would require aggregation on every status bar refresh.
**Consequence:** Counter reflects total tokens since pool creation, not per-session.
