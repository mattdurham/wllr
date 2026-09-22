# Metrics — Prometheus usage and agent observability

wllr exposes Prometheus metrics over HTTP so a local scraper can answer
questions like *which models am I actually using*, *how many tokens does each
model and agent consume*, and *how many turns and sub-agents am I running*.

Metrics are off unless the debug listener is enabled (see below). They are
per-process and in-memory: restarting wllr resets the counters, and nothing is
sent anywhere unless something scrapes the endpoint.

## Enabling

Metrics are served on the same listener as the Go profiler, configured with
`WLLR_PPROF_ADDR` (default `127.0.0.1:6060`):

```sh
WLLR_PPROF_ADDR=127.0.0.1:6060 wllr
curl -s http://127.0.0.1:6060/metrics | grep '^wllr_'
```

The listener binds to loopback by default. Setting `WLLR_PPROF_ADDR=""` disables
it, which also disables metrics. Because the endpoint exposes operational detail,
keep it on loopback or behind something that authenticates.

## Metrics

| Metric | Type | Labels | Meaning |
|--------|------|--------|---------|
| `wllr_turns_total` | counter | `agent_id`, `agent_kind`, `model` | Completed turns. |
| `wllr_tokens_total` | counter | `agent_id`, `agent_kind`, `model`, `kind` | Tokens by direction: `input`, `output`, `reasoning`, `cache_creation`, `cache_read`. |
| `wllr_turn_failures_total` | counter | `agent_id`, `agent_kind`, `model` | Turns that ended in error. |
| `wllr_turn_duration_seconds` | histogram | `agent_id`, `agent_kind`, `model` | Wall-clock turn duration. |
| `wllr_active_turns` | gauge | `agent_id` | Turns currently in flight. |
| `wllr_agents` | gauge | `agent_kind` | Live agents of each kind. |
| `wllr_subagent_spawns_total` | counter | — | Sub-agents spawned since startup. |

`agent_kind` is `main` for the primary agent and `subagent` for everything else.
`agent_id` is the agent's identity (`main`, `main/coder`, …), so per-agent and
per-model attribution comes from grouping on those labels:

```promql
# Tokens per model, split by direction.
sum by (model, kind) (wllr_tokens_total)

# Tokens and turns for sub-agents only.
sum by (model) (wllr_tokens_total{agent_kind="subagent"})
sum by (agent_id, model) (wllr_turns_total{agent_kind="subagent"})
```

## Cardinality

`wllr_turns_total`, `wllr_tokens_total`, `wllr_turn_failures_total`, and
`wllr_turn_duration_seconds` carry `agent_id` and `model`, because per-agent and
per-model attribution is their purpose. They are cumulative counters, so the
series set grows with the number of distinct (agent, model) pairs seen in a
process — not with turn count. A session that spawns many uniquely named
sub-agents will accumulate one series per name.

The pool-level gauges (`wllr_agents`, `wllr_subagent_spawns_total`) are
deliberately *not* labeled by agent id for that reason; they aggregate by kind.

## Instrumentation points

- `modules/agent` reports a plain `TurnUsage` / `AgentLifecycle` value through
  the observer seams (`AgentPool.SetUsageObserver`, `SetLifecycleObserver`). The
  agent package has no metrics dependency; `cmd` owns the Prometheus types.
- A turn reports a *start* signal (in-flight gauge) and then a completion with
  its usage, so finished and in-flight turns are distinguishable.
- Failed turns are reported as turns with no token usage rather than dropped, so
  turn counts match what actually ran.
- When an interceptor reroutes a turn to a different model, usage is attributed
  to the rerouted model — the one that actually served the request.

## Relationship to traces

Metrics are counters and gauges for aggregate questions. For per-request detail
(spans, timings, payload metadata) use the optional `otel-traces` extension,
which exports OpenTelemetry traces to an OTLP endpoint. The two are independent:
enabling one does not enable the other.
