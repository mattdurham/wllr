package main

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/mattdurham/wllr/modules/agent"
)

// Prometheus metrics for model and agent usage. wllr is a local, single-user
// process, so these are registered on a dedicated registry rather than the
// global default one: the exposition then contains only wllr's own metrics.
//
// Turns, tokens, failures, and turn duration are labeled by agent_id and model
// because per-agent/per-model attribution is the point of these metrics; they
// are cumulative counters, so the series set grows with the number of distinct
// (agent, model) pairs seen rather than with turn count. Pool-level gauges
// (live agents, spawns) are labeled by kind only, since a long session can
// spawn unboundedly many distinctly-named sub-agents.
type wllrMetrics struct {
	registry *prometheus.Registry

	turns     *prometheus.CounterVec
	tokens    *prometheus.CounterVec
	agents    *prometheus.GaugeVec
	spawns    prometheus.Counter
	failures  *prometheus.CounterVec
	turnDur   *prometheus.HistogramVec
	activeTms *prometheus.GaugeVec
}

// newWllrMetrics builds and registers the metric set.
func newWllrMetrics() *wllrMetrics {
	reg := prometheus.NewRegistry()
	m := &wllrMetrics{
		registry: reg,
		turns: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "wllr_turns_total",
			Help: "Completed LLM turns, by agent and model.",
		}, []string{labelAgentID, labelAgentKind, labelModel}),
		tokens: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "wllr_tokens_total",
			Help: "Tokens processed, by agent, model, and direction.",
		}, []string{labelAgentID, labelAgentKind, labelModel, "kind"}),
		agents: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "wllr_agents",
			Help: "Live agents by kind (main or subagent).",
		}, []string{"agent_kind"}),
		spawns: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "wllr_subagent_spawns_total",
			Help: "Sub-agents spawned since startup.",
		}),
		failures: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "wllr_turn_failures_total",
			Help: "Failed LLM turns, by agent and model.",
		}, []string{labelAgentID, labelAgentKind, labelModel}),
		turnDur: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "wllr_turn_duration_seconds",
			Help:    "Wall-clock duration of completed LLM turns.",
			Buckets: []float64{1, 5, 15, 30, 60, 120, 300, 600},
		}, []string{labelAgentID, labelAgentKind, labelModel}),
		activeTms: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "wllr_active_turns",
			Help: "Turns in flight, by agent id.",
		}, []string{labelAgentID}),
	}
	reg.MustRegister(
		m.turns, m.tokens, m.agents, m.spawns, m.failures, m.turnDur, m.activeTms,
	)
	return m
}

// Metric label names. Hoisted because they repeat across every vec.
const (
	labelAgentID   = "agent_id"
	labelAgentKind = "agent_kind"
	labelModel     = "model"
)

// agentKind classifies an agent ID for the low-cardinality agent_kind label.
func agentKind(main bool) string {
	if main {
		return "main"
	}
	return "subagent"
}

// recordTurn folds one completed agent turn into the counters. It is called
// from agent goroutines, so it must be concurrency-safe; the Prometheus
// counter types are.
func (m *wllrMetrics) recordTurn(t agent.TurnUsage) {
	if m == nil {
		return
	}
	// A start signal only moves the in-flight gauge; a completion moves it back
	// and records the turn.
	if t.Started {
		m.activeTms.WithLabelValues(t.AgentID).Inc()
		return
	}
	m.activeTms.WithLabelValues(t.AgentID).Dec()
	if t.Model == "" {
		t.Model = "unknown"
	}
	kind := agentKind(t.Main)
	if t.DurationMS > 0 {
		m.turnDur.WithLabelValues(t.AgentID, kind, t.Model).
			Observe(float64(t.DurationMS) / 1000)
	}
	if t.Err {
		m.failures.WithLabelValues(t.AgentID, kind, t.Model).Inc()
		return
	}
	m.turns.WithLabelValues(t.AgentID, kind, t.Model).Inc()
	// Emit each direction separately so cached/reasoning tokens stay
	// distinguishable instead of collapsing into one total. Empty kinds are
	// skipped so a provider that does not report them adds no zero series.
	for kindLabel, v := range map[string]int64{
		"input":          t.InputTokens,
		"output":         t.OutputTokens,
		"reasoning":      t.ReasoningTokens,
		"cache_creation": t.CacheCreationTokens,
		"cache_read":     t.CacheReadTokens,
	} {
		if v > 0 {
			m.tokens.WithLabelValues(t.AgentID, kind, t.Model, kindLabel).Add(float64(v))
		}
	}
}

// recordLifecycle tracks pool membership: a gauge for live agents by id, and a
// spawn counter so sub-agent creation is visible even after the agent closes.
func (m *wllrMetrics) recordLifecycle(ev agent.AgentLifecycle) {
	if m == nil {
		return
	}
	// The gauge is labeled by kind, not agent id: a long session can spawn
	// arbitrarily many distinctly-named sub-agents, and a per-id gauge would
	// leak a series for each.
	if ev.Main {
		m.agents.WithLabelValues("main").Set(1)
		return
	}
	m.agents.WithLabelValues("subagent").Set(float64(ev.Live))
	if ev.Spawned {
		m.spawns.Inc()
	}
}

// handler returns the HTTP handler serving the exposition.
func (m *wllrMetrics) handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}
