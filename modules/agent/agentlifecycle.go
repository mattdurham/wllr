package agent

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// AgentLifecycle describes an agent appearing or disappearing from the pool,
// for observability. Reported outside the pool mutex.
type AgentLifecycle struct {
	// AgentID is the agent that changed (e.g. "main", "main/coder").
	AgentID string
	// Live is the number of agents in the pool after the change.
	Live int64
	// Main reports whether the agent is the primary agent.
	Main bool
	// Spawned is true when the agent was added, false when removed.
	Spawned bool
}

// SetLifecycleObserver replaces all lifecycle observers with fn, invoked
// whenever an agent is added to or removed from the pool. It runs on the
// caller's goroutine and must be safe for concurrent use and non-blocking. A
// nil observer clears the list.
//
// Prefer AddLifecycleObserver when another subsystem already owns the slot
// (for example the metrics recorder in cmd): Set replaces, Add appends.
func (p *AgentPool) SetLifecycleObserver(fn func(AgentLifecycle)) {
	p.dispatchMu.Lock()
	if fn == nil {
		p.lifecycleObservers = nil
	} else {
		p.lifecycleObservers = []func(AgentLifecycle){fn}
	}
	p.dispatchMu.Unlock()
}

// AddLifecycleObserver appends a lifecycle observer without disturbing any
// already-installed observer. The API server uses it to attach per-agent
// stream observers as agents appear. A nil observer is ignored.
func (p *AgentPool) AddLifecycleObserver(fn func(AgentLifecycle)) {
	if fn == nil {
		return
	}
	p.dispatchMu.Lock()
	p.lifecycleObservers = append(p.lifecycleObservers, fn)
	p.dispatchMu.Unlock()
}

// observeLifecycle reports a pool membership change to every installed
// observer. Callers must not hold p.mu: the observers are host code. The slice
// is snapshotted under the lock and invoked outside it.
func (p *AgentPool) observeLifecycle(ev AgentLifecycle) {
	if p == nil {
		return
	}
	p.dispatchMu.RLock()
	observers := make([]func(AgentLifecycle), len(p.lifecycleObservers))
	copy(observers, p.lifecycleObservers)
	p.dispatchMu.RUnlock()
	for _, fn := range observers {
		fn(ev)
	}
}
