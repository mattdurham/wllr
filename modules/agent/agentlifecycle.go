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

// SetLifecycleObserver installs a callback invoked whenever an agent is added
// to or removed from the pool. It runs on the caller's goroutine and must be
// safe for concurrent use and non-blocking. A nil observer disables reporting.
func (p *AgentPool) SetLifecycleObserver(fn func(AgentLifecycle)) {
	p.dispatchMu.Lock()
	p.lifecycleObserver = fn
	p.dispatchMu.Unlock()
}

// observeLifecycle reports a pool membership change to the installed observer.
// Callers must not hold p.mu: the observer is host code.
func (p *AgentPool) observeLifecycle(ev AgentLifecycle) {
	if p == nil {
		return
	}
	p.dispatchMu.RLock()
	fn := p.lifecycleObserver
	p.dispatchMu.RUnlock()
	if fn != nil {
		fn(ev)
	}
}
