# Interceptor Contract — Transform-Capable Interactions

*Date: 2026-06-30*

## Vision

Treat every major region, component, and system in the harness as an **actor**
whose interactions can be observed, **transformed**, **rerouted**, or **blocked**
by registered interceptors. Not every actor needs a mailbox — the unifying
primitive is a single, consistent *interceptor contract* applied at interaction
points (events), with the host as the ordered, trusted router.

This unlocks a class of features that all share one shape — "inspect an
interaction, then optionally change it before it proceeds":

- **Bash / tool security** — inspect a tool call, **rewrite** the command
  (e.g. inject `--dry-run`) or **block** it.
- **PII / API-key detection** — inspect messages going to the LLM, **redact**
  them, or **block** the request.
- **Model routing** — inspect the request, **reroute** it to a cheap local
  model or a frontier model.

All three live on interaction seams that already exist; the only thing missing
is the ability to *transform* rather than merely observe/veto.

## Core insight

Today `sdk.EventResponse` is **observe + veto**:

```go
type EventResponse struct {
    Error  string `json:"error,omitempty"`
    Cancel bool   `json:"cancel,omitempty"`
    Block  bool   `json:"block,omitempty"`
}
```

The whole feature is one capability gap: an interceptor cannot **return a
modified payload**. Close that gap uniformly and the actor/interception vision
becomes tractable — it's a generalization of the existing event/dispatch infra,
not a new actor runtime.

## The contract

`EventResponse` gains an optional transformed payload:

```go
type EventResponse struct {
    Error   string          `json:"error,omitempty"`
    Cancel  bool            `json:"cancel,omitempty"`
    Block   bool            `json:"block,omitempty"`
    Payload json.RawMessage `json:"payload,omitempty"` // transformed event payload
}
```

An interceptor's `_on_event` may return:

| Intent     | Returns                                  |
|------------|------------------------------------------|
| observe    | `nil` / `{}`                             |
| transform  | `{payload: <modified event payload>}`    |
| reroute    | transform a routing field in the payload (e.g. `model`) |
| block      | `{block: true, error: "<reason>"}`       |

### Chaining (host-mediated)

The host runs subscribed interceptors in **priority order** (existing extension
`priority`, lower runs first; alphabetical tiebreak — same order as
`DispatchEvent` today). The payload is **threaded**: each interceptor receives
the payload as transformed by the previous one. The first `Block`/`Cancel`
stops the chain. The host applies the final payload to the underlying operation.

New host method:

```go
// DispatchEventChain threads evt's payload through subscribed interceptors in
// priority order. Each interceptor may transform the payload (EventResponse.Payload),
// or block the chain (Block/Cancel). Returns the final (possibly transformed)
// event, whether it was blocked, the block reason, and any dispatch error.
func (h *Host) DispatchEventChain(ctx context.Context, evt sdk.Event) (final sdk.Event, blocked bool, reason string, err error)
```

`DispatchEvent` (observe-only, collects all responses) stays for existing
fire-and-forget callers; `DispatchEventChain` is the transform path.

### Invariants

- **Backward compatible.** An interceptor that returns no `Payload` leaves the
  payload unchanged — existing observe/veto handlers behave identically.
- **Ordering is deterministic.** Priority asc, then name asc — identical to
  `DispatchEvent`.
- **First block wins.** The chain stops at the first `Block`/`Cancel`; later
  interceptors do not run. `Error` carries the surfaced reason.
- **Malformed transforms are ignored.** If an interceptor returns a `Payload`
  that fails to unmarshal into the event's payload type at the seam, the host
  keeps the prior payload and logs a warning (never crashes the turn).
- **Bounded crossings.** Chains run **once per interaction** (per tool call,
  per provider request) — never per render frame. WASM-crossing cost is
  proportional to interceptor count × interaction count, not frame rate.
- **Trust.** Interception requires the same subscription + permission model as
  events today. An interceptor only sees interactions it subscribes to.

## Seams

### Phase 1 (this change): `before_tool_call`

`Host.ExecuteTool` already dispatches `EventBeforeToolCall` and honors `Cancel`.
Upgrade it to `DispatchEventChain`:

- An interceptor may return a transformed `BeforeToolCallPayload` with a
  rewritten `Input` (e.g. a security layer rewriting a bash command) or set
  `Block` with a reason.
- **Native tools:** the host calls `nativeFn(ctx, finalInput)` with the
  transformed input.
- **WASM tools:** the chain delivers the transformed payload to the implementing
  extension (later in priority order), which calls `tool_result` against the
  rewritten input.
- A blocked call returns `toolResult{Result: reason, IsError: true}`.

This fully covers the **bash security** use case (rewrite or block) and tightens
the existing `permissions` block path (now carries a reason).

### Phase 2 (next): `before_provider_request`

Upgrade the provider-request dispatch to `DispatchEventChain`:

- **PII redaction:** interceptor returns a transformed `BeforeProviderRequestPayload`
  with redacted `Messages`, or `Block` on a hard hit.
- **Routing:** interceptor returns a transformed payload with a different `Model`.

Wrinkle: model is fixed at spawn today, and `before_provider_request` is
dispatched in the harness *decoupled* from the turn. Phase 2 must thread the
transformed request (messages + model) into where the turn's `LanguageModel`
and message slice are built (`agent.executeTurn` / the submit path). Designed
here, implemented in the follow-up.

### Future seams (reuse the same contract)

- `before_agent_start` — transform/expand the prompt.
- Region render transform — interceptor edits a region's `UINode` tree before
  render (off-loop, on-change; see the statusline scene design). This is the UI
  half of the actor vision; it reuses `DispatchEventChain` with a `node` payload.
- Input routing — interceptor claims/transforms input events.

These are added **only as concrete use cases arrive** — the contract is the
architecture; seams are incremental.

## SDK (`wllrsdk.go`)

Extensions need an ergonomic way to transform. Add:

```go
// OnInterceptToolCall registers a transform interceptor for tool calls. The
// handler returns (rewrittenInput, block, reason): a non-nil rewrittenInput
// replaces the tool input; block=true vetoes the call with reason.
func OnInterceptToolCall(fn func(agentID, toolName string, input json.RawMessage) (newInput json.RawMessage, block bool, reason string))
```

This wraps the `_on_event` → `EventResponse{Payload|Block|Error}` plumbing so
extension authors never construct the envelope by hand.

## Implementation order

1. **`sdk`** — add `EventResponse.Payload`; round-trip test.
2. **`extension`** — `DispatchEventChain`; unit tests (transform threads,
   ordering, first-block-wins, malformed-payload-ignored).
3. **`extension` ExecuteTool** — use the chain for `before_tool_call`; apply
   transformed input (native + WASM); block with reason. Tests.
4. **`wllrsdk.go`** — `OnInterceptToolCall` helper (base + per-extension copies
   that need it).
5. **Spec/NOTES/docs** — `sdk` SPECS/NOTES, `extension` SPECS/NOTES/TESTS,
   `docs/extensions.md` (EventResponse transform contract + tool-call rewrite).
6. **Phase 2 (follow-up)** — `before_provider_request` transform/reroute +
   per-turn model override plumbing.
