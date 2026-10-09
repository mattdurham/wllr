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

// SetUsageObserver replaces all turn-usage observers with fn, invoked once per
// completed agent turn with that turn's usage. Observers run on the agent's
// turn goroutine, so they must be safe for concurrent use and must not block.
// A nil observer clears the list.
//
// Prefer AddUsageObserver when another subsystem already owns the slot (for
// example the metrics recorder in cmd): Set replaces, Add appends.
func (p *AgentPool) SetUsageObserver(fn func(TurnUsage)) {
	p.dispatchMu.Lock()
	if fn == nil {
		p.usageObservers = nil
	} else {
		p.usageObservers = []func(TurnUsage){fn}
	}
	p.dispatchMu.Unlock()
}

// AddUsageObserver appends a turn-usage observer without disturbing any
// already-installed observer. This is the multi-subscriber form: the API
// server and the metrics recorder both observe turns independently. A nil
// observer is ignored.
func (p *AgentPool) AddUsageObserver(fn func(TurnUsage)) {
	if fn == nil {
		return
	}
	p.dispatchMu.Lock()
	p.usageObservers = append(p.usageObservers, fn)
	p.dispatchMu.Unlock()
}

// observeTurn reports a completed turn to every installed observer. The
// observer slice is snapshotted under the lock and invoked outside it, so an
// observer that itself calls back into the pool cannot deadlock.
func (p *AgentPool) observeTurn(u TurnUsage) {
	if p == nil {
		return
	}
	p.dispatchMu.RLock()
	observers := make([]func(TurnUsage), len(p.usageObservers))
	copy(observers, p.usageObservers)
	p.dispatchMu.RUnlock()
	for _, fn := range observers {
		fn(u)
	}
}
