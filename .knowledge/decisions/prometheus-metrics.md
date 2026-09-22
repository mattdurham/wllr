---
type: Decision
title: Prometheus metrics via plain-value observer seams
description: Usage and agent-lifecycle metrics are observed as plain values from the agent package and turned into Prometheus counters by cmd, keeping the agent package free of a metrics dependency.
tags: [metrics, prometheus, observability, agent, tokens]
timestamp: 2026-09-22T00:00:00Z
---

**Decision:** wllr exposes Prometheus metrics at `/metrics` on the debug
listener (`WLLR_PPROF_ADDR`, default `127.0.0.1:6060`), alongside pprof. The
`modules/agent` package reports per-turn usage and pool membership as plain
values (`TurnUsage`, `AgentLifecycle`) through observer callbacks; `cmd` owns the
Prometheus types and the registry.

**Rationale:** The questions these metrics answer are per-model and per-agent —
which models are in use, tokens per model per agent, turns per model, and
sub-agent counts. That data lives on the agent turn boundary, but the agent
package is deliberately dependency-light and must not import a metrics library.
The existing `SetContextUsageDispatcher` seam already established the pattern, so
usage reporting reuses it. Reporting plain values also keeps the instrumentation
unit-testable without a registry.

**Consequence:**

- A turn reports a start signal and then a completion, so in-flight turns are
  visible; a failed turn is reported with no usage rather than dropped, so turn
  counts match what actually ran.
- Interceptor reroutes update the turn's recorded model, so usage is attributed
  to the model that actually served the request.
- Series labeled by `agent_id`/`model` are cumulative counters, so the series
  set grows with distinct (agent, model) pairs rather than with turn count;
  pool-level gauges are labeled by kind only because a long session can spawn
  unboundedly many distinctly-named sub-agents.
- Metrics are opt-in with the debug listener: both are disabled when
  `WLLR_PPROF_ADDR=""`, and the listener binds to loopback by default.

# Applies To

- [agent package](../packages/agent.md) (observer seams)
- [harness package](../packages/harness.md) (status-bar context usage, unchanged)
- [metrics doc](../../docs/metrics.md)

# Origin

Requested 2026-09-22: visibility into models in use, tokens per model per agent,
turns per model, and sub-agent counts. See also the optional `otel-traces`
extension for per-request traces; the two are independent.
