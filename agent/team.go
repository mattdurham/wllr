package agent

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"context"
	"sync"
)

// Team is a named collection of agent IDs managed by an AgentPool.
// The team itself does not own goroutines or resources — it is a lightweight
// grouping for coordinated lifecycle management. All member agents are owned
// by the pool.
type Team struct {
	id   string
	pool *AgentPool

	mu      sync.RWMutex
	members map[string]bool // set of agent IDs
}

// ID returns the team's unique identifier.
func (t *Team) ID() string { return t.id }

// AddMember registers agentID as a member of this team.
// Returns ErrAgentNotFound if agentID is not registered in the pool.
func (t *Team) AddMember(agentID string) error {
	if t.pool.Get(agentID) == nil {
		return ErrAgentNotFound
	}
	t.mu.Lock()
	t.members[agentID] = true
	t.mu.Unlock()
	return nil
}

// RemoveMember removes agentID from the team's membership set.
// Does NOT close or cancel the agent. No-op if agentID is not a member.
func (t *Team) RemoveMember(agentID string) {
	t.mu.Lock()
	delete(t.members, agentID)
	t.mu.Unlock()
}

// Members returns a snapshot of all member agent IDs.
func (t *Team) Members() []string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	ids := make([]string, 0, len(t.members))
	for id := range t.members {
		ids = append(ids, id)
	}
	return ids
}

// Close cancels all member agents via pool.Close and removes them from the pool.
// The team itself is removed from the pool by pool.CloseTeam, not here.
// Close is idempotent: calling it on already-closed agents is a no-op.
func (t *Team) Close(_ context.Context) error {
	t.mu.Lock()
	members := make([]string, 0, len(t.members))
	for id := range t.members {
		members = append(members, id)
	}
	t.members = make(map[string]bool)
	t.mu.Unlock()

	var firstErr error
	for _, id := range members {
		if err := t.pool.Close(id); err != nil && err != ErrAgentNotFound {
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}
