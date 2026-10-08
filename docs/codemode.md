# Code Mode — JS execution for agents (goja)

pi.dev-style "code mode" for wllr: instead of N discrete tool calls (each burning
context and a round trip), the model gets **one** tool — `run_js` — whose code can
compose wllr's capabilities (loop, branch, batch, aggregate) and return a single
structured result. Primary author: **agents**. Secondary (same runtime, later): user scripts.

## Status

Brainstorm-validated 2026-10-06. Not implemented. Decisions below are settled;
open questions are listed at the end.

## Decisions (settled in brainstorm)

| # | Question | Decision | Rationale |
|---|---|---|---|
| 1 | Primary author | Agents; user scripts later, same runtime | The round-trip/context win is agent-side |
| 2 | Where the runtime lives | **Extension-owned tool, host-executed evaluator** | goja is reflection-heavy pure Go — TinyGo/WASM cannot host it. Extension owns the tool (hot-reload, per-project enable, tool-description iteration); a `codemode_eval` host method executes |
| 3 | API surface | **Full**: `tasks`, `files`, `agents`, `pool`, `msg` + `state` | Orchestration power is the point; gated per namespace |
| 4 | Execution path | **Everything through the wllr host bridge** | Bindings reuse the same host-method surface extensions get — one choke point for capability checks, permissions, audit. No direct pool/agent reach-in from bindings |
| 5 | Sync/async | **All bridge calls sync** | pi.dev insight: models write better sync code (no forgotten `await` — which fails silently in goja). Long ops exist but block |
| 6 | Time budget | **Caller-supplied timespan**; no script-declared global budget | `run_js` args carry `timeoutMs` (default 30s). Per-call `{timeoutMs}` narrows individual long ops (default: remaining allowance) |
| 7 | VM lifetime | **Fresh VM per call** + `wllr.state` JSON stash | Bounded bursts; continuity is explicit (return state to model as args, or `wllr.state`). Persistent VM deferred to v2 |
| 8 | Output | `{result, logs}`, head+tail truncated to a byte cap (default ~16KB) | A script that greps 40 files returns a digest, not a firehose |

## Architecture

```
extensions/codemode/          TinyGo WASM extension
  main.go                     registers run_js tool; validates args; calls host method
modules/codemode/             host side (core binary)
  eval.go                     goja VM lifecycle, watchdog (Interrupt), fresh-per-call
  bridge.go                   bridgeCall choke point: capability check + permission + audit + timeout
  bind_tasks.go               wllr.tasks.* → host methods (list_tasks, task_get, …)
  bind_files.go               wllr.files.* (read-only in v1)
  bind_agents.go              wllr.agents.* (capability `agents`)
  bind_pool.go                wllr.pool.* / wllr.msg.*
  bind_state.go               wllr.state.* (host-side JSON store per agent session)
  output.go                   serialization + head/tail truncation
```

- **No new bridge surface beyond `codemode_eval`.** Bindings call the *existing*
  host-method surface (`list_tasks`, `spawn_agent`, `file_read`, …) — the same
  methods the WASM extensions use. The bridge method registry is the single
  audited boundary for both extension tools and codemode scripts.
- The extension's tool description is the model-facing contract (like every
  wllr extension) — iterable without core releases.
- goja VM constructed per call: no globals, no `require`, no `process`,
  no `fetch`/net, no `fs`, no timers. Only injected roots: `wllr`, `console`.

## Capability gating

```yaml
# ~/.config/wllr/config.yaml (per-project override: .wllr/config.yaml)
codemode:
  capabilities: [tasks, files, agents]   # namespaces code may see
  timeoutMs: 30000        # default when the tool call omits it
  maxOutputBytes: 16384
```

- A namespace not in `capabilities` is **absent** from `wllr` (property throws
  a clear "capability disabled" error).
- `files` is **read-only in v1** (`read/list/glob/grep`); `files.write` becomes
  its own capability in v2 — model-written code must not mutate the tree outside
  the normal edit/audit trail on day one.
- Per-call permission checks ride the bridge choke point using the existing
  permission module. Interactive approval prompts (first-use-per-session) are a
  fast-follow reusing the existing prompt path — deliberately not in the first PR.

## Execution & timeout semantics

- `run_js` tool args: `{ code: string, timeoutMs?: number, input?: any }`.
  `timeoutMs` omitted → config default (30s).
- External watchdog goroutine: `select` on tool-call `ctx.Done()` or the
  timespan elapsing → `vm.Interrupt(err)`. This is the only reliable kill for a
  spinning VM (goja is cooperatively scheduled; a script cannot self-interrupt
  while blocked). Turn cancel (Esc) and agent hard-kill (`x`) cancel the tool
  ctx and therefore kill the script instantly — same primitives as any tool.
- Long ops take `{timeoutMs}` (default: the script's remaining allowance):
  `wllr.agents.waitFor(id, {timeoutMs})`, plus `wllr.sleep(ms)` (interruptible —
  sleeps on a channel the watchdog can fire) for poll loops.
- All timeouts enforced at `bridgeCall`, which already threads ctx.

## Output contract

- Script `return` value → JSON-serialized into `result`.
- `console.log/warn/error` captured into `logs`.
- Both fields head+tail truncated to `maxOutputBytes` with an elision marker.
- Errors surface as the tool's normal error with the JS stack + line numbers so
  the model self-corrects in one retry.

## v1 namespace surface

| Namespace | Methods | Host methods | Capability |
|---|---|---|---|
| `wllr.tasks` | list/get/create/update/complete | existing task_* methods | `tasks` |
| `wllr.files` | read/list/glob/grep | file_read + search host surface | `files` (read-only) |
| `wllr.agents` | spawn/status/list/output/waitFor | spawn_agent, agent_status, … | `agents` |
| `wllr.pool` | send / message agents | send_message bridge method | `pool` |
| `wllr.msg` | inbox peek/cancel (own queue) | queue_peek/queue_cancel | `msg` |
| `wllr.state` | get/set/del (JSON, per agent session, evicted on agent death) | host store | always |
| `wllr.sleep` | ms | — (in-process, interruptible) | always |

Out of the VM entirely: `require`, `fetch`/net, `fs`, `process`, timers, eval of
new host capabilities.

## Explicitly deferred (v2)

- Persistent VM per agent-session (helpers surviving across `run_js` calls)
- Real async scheduler (promises + event loop on goja) for true
  spawn-many-and-await fan-out — v1 pattern is: orchestrate via native tools,
  crunch via scripts
- `files.write` capability
- Per-execution interactive approval flow
- User-facing scripting surface (e.g. `wllr run script.js`)

## Test plan (TDD, per repo conventions)

- `modules/codemode`: watchdog kills a `while(true)` at the timespan; tool-ctx
  cancel kills mid-call; capability namespace absent + error text; per-call
  `timeoutMs` narrowing; truncation shape (head/tail + marker); state stash
  lifecycle incl. eviction; serialization of return values (objects, arrays,
  errors); sleep interruptibility.
- `extensions/codemode`: arg validation, `codemode_eval` contract, tool
  description stability.
- Harness: registration, config gating end-to-end, audit entries at the bridge.
- Docs per file-header contracts: SPECS/NOTES/TESTS in both modules + extension.

## Open questions (resolve during implementation)

1. Default capability set when `codemode:` is absent from config — suggest
   **none** (tool present but errors with "enable codemode.capabilities"), so
   opt-in is explicit.
2. Does `wllr.files.grep` call the search extension's host surface or bind rg
   directly? (Prefer reusing the search extension's host method — one rg policy.)
3. Whether `input` args are passed as `wllr.input` global or as a function param.
   (Slight lean: param — `function run(input)` — keeps the entry point explicit.)
