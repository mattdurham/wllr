package agent

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// TurnUsage describes one completed agent turn for observability. It is a
// plain value so the agent package stays free of any metrics dependency: the
// host installs an observer via SetUsageObserver and decides what to record.
type TurnUsage struct {
	// AgentID is the agent that ran the turn (e.g. "main", "main/coder").
	AgentID string
	// Model is the model the turn actually ran on.
	Model string
	// Token counts for the turn. Zero on failure.
	InputTokens         int64
	OutputTokens        int64
	TotalTokens         int64
	ReasoningTokens     int64
	CacheCreationTokens int64
	CacheReadTokens     int64
	// DurationMS is the turn's wall-clock duration in milliseconds.
	DurationMS int64
	// Main reports whether this was the primary agent rather than a sub-agent.
	Main bool
	// Err reports whether the turn failed, so a failed turn is counted without
	// attributing token usage it never produced.
	Err bool
	// Started reports that the turn began, so observers can track in-flight
	// turns. When true the remaining fields are zero and no completion is
	// implied; a completion is reported by a subsequent call with Started false.
	Started bool
}

// TurnStarted reports a turn beginning. Observers use it to track in-flight
// work; no usage fields are populated.
func TurnStarted(agentID string, main bool) TurnUsage {
	return TurnUsage{AgentID: agentID, Main: main, Started: true}
}

// SetUsageObserver installs a callback invoked once per completed agent turn
// with that turn's usage. The observer runs on the agent's turn goroutine, so
// it must be safe for concurrent use and must not block. A nil observer
// disables reporting.
func (p *AgentPool) SetUsageObserver(fn func(TurnUsage)) {
	p.dispatchMu.Lock()
	p.usageObserver = fn
	p.dispatchMu.Unlock()
}

// observeTurn reports a completed turn to the installed observer, if any.
func (p *AgentPool) observeTurn(u TurnUsage) {
	if p == nil {
		return
	}
	p.dispatchMu.RLock()
	fn := p.usageObserver
	p.dispatchMu.RUnlock()
	if fn != nil {
		fn(u)
	}
}
