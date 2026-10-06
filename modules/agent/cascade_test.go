package agent_test

import (
	"errors"
	"testing"

	"github.com/mattdurham/wllr/modules/agent"
)

func spawnTree(t *testing.T, ids ...string) *agent.AgentPool {
	t.Helper()
	pool := agent.NewPool()
	pool.SetModelContextWindow("m", 100000)
	for _, id := range ids {
		if _, err := pool.Spawn(id, newMockLM(), agent.SpawnOpts{
			ModelName: "m", ContextWindow: 100000,
		}); err != nil {
			t.Fatalf("spawn %s: %v", id, err)
		}
	}
	return pool
}

// Closing an agent must remove its whole subtree: descendants exist only to
// serve their parent, so leaving them running orphans their work.
func TestCloseCascadesToDescendants(t *testing.T) {
	pool := spawnTree(t, "main", "main/planner", "main/planner/helper", "main/other")

	if err := pool.Close("main/planner"); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if a := pool.Get("main/planner"); a != nil {
		t.Error("closed agent still present")
	}
	if a := pool.Get("main/planner/helper"); a != nil {
		t.Error("grandchild survived its parent's close")
	}
	if pool.Get("main") == nil || pool.Get("main/other") == nil {
		t.Error("siblings of the closed agent must be untouched")
	}
}

// The root agent cannot be closed: it owns the session lifecycle and exits
// with the harness, so a close request on it is rejected outright.
func TestCloseRootIsRejected(t *testing.T) {
	pool := spawnTree(t, "main", "main/a")
	if err := pool.Close(agent.MainAgentID); !errors.Is(err, agent.ErrRootAgentClose) {
		t.Fatalf("Close root err = %v, want ErrRootAgentClose", err)
	}
	if pool.Get(agent.MainAgentID) == nil || pool.Get("main/a") == nil {
		t.Error("rejected close must not remove any agent")
	}
}

// "main/x2" must not be treated as a descendant of "main/x"; the separator is
// what makes the prefix test correct.
func TestCloseDoesNotMatchSiblingPrefix(t *testing.T) {
	pool := spawnTree(t, "main", "main/x", "main/x2")
	if err := pool.Close("main/x"); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if pool.Get("main/x2") == nil {
		t.Fatal("main/x2 is not a descendant of main/x and must survive")
	}
}

// Cancel stops descendants' turns but keeps them in the pool for inspection.
func TestCancelCascadesButKeepsAgents(t *testing.T) {
	pool := spawnTree(t, "main", "main/worker", "main/worker/kid")
	if err := pool.Cancel("main/worker"); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if pool.Get("main/worker") == nil || pool.Get("main/worker/kid") == nil {
		t.Error("Cancel must not remove agents from the pool")
	}
}

func TestDescendantsListsSubtree(t *testing.T) {
	pool := spawnTree(t, "main", "main/a", "main/a/b", "main/z")
	got := pool.Descendants("main/a")
	if len(got) != 1 || got[0] != "main/a/b" {
		t.Fatalf("Descendants(main/a) = %v, want [main/a/b]", got)
	}
	if all := pool.Descendants("main"); len(all) != 3 {
		t.Fatalf("Descendants(main) = %v, want 3 entries", all)
	}
}
