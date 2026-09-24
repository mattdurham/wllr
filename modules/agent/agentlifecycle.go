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

// SetLifecycleObserver installs the primary callback invoked whenever an agent
// is added to or removed from the pool. It runs on the caller's goroutine and
// must be safe for concurrent use and non-blocking. A nil observer disables
// the primary callback. Additional callbacks registered with
// AddLifecycleObserver are not affected.
func (p *AgentPool) SetLifecycleObserver(fn func(AgentLifecycle)) {
	p.dispatchMu.Lock()
	p.lifecycleObserver = fn
	p.dispatchMu.Unlock()
}

// AddLifecycleObserver registers an additional callback for agent pool
// membership changes. It complements SetLifecycleObserver, which is used by
// the host's primary metrics observer. The callback runs on the caller's
// goroutine and must be safe for concurrent use and non-blocking. A nil
// callback is ignored.
func (p *AgentPool) AddLifecycleObserver(fn func(AgentLifecycle)) {
	if p == nil || fn == nil {
		return
	}
	p.dispatchMu.Lock()
	p.lifecycleObservers = append(p.lifecycleObservers, fn)
	p.dispatchMu.Unlock()
}

// observeLifecycle reports a pool membership change to the installed observer.
// Callers must not hold p.mu: the observer is host code.
func (p *AgentPool) observeLifecycle(ev AgentLifecycle) {
	if p == nil {
		return
	}
	p.dispatchMu.RLock()
	primary := p.lifecycleObserver
	observers := append([]func(AgentLifecycle){}, p.lifecycleObservers...)
	p.dispatchMu.RUnlock()
	if primary != nil {
		primary(ev)
	}
	for _, fn := range observers {
		fn(ev)
	}
}
