package harness

import (
	"testing"

	"github.com/mattdurham/wllr/modules/agent"
)

// At startup the pool should contain the root agent, so /agents has a node to
// show even before any sub-agent is spawned.
func TestPoolHasRootAtStartup(t *testing.T) {
	m := newTestModel()
	if m.agentPool == nil {
		t.Fatal("test model has no pool")
	}
	ids := m.agentPool.ListAgents()
	t.Logf("pool agents: %v", ids)
	if m.agentPool.Get(agent.MainAgentID) == nil {
		t.Fatalf("root agent %q missing from pool: %v", agent.MainAgentID, ids)
	}
}
