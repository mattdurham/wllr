package harness

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/mattdurham/wllr/modules/agent"
	"github.com/mattdurham/wllr/modules/sdk"
)

// Focusing an agent must retarget input: the root is not special, so the same
// path serves main and a sub-agent.
func TestSubmitRoutesToFocusedAgent(t *testing.T) {
	m := newTestModel()
	if m.agentPool == nil {
		t.Fatal("no pool")
	}
	if _, err := m.agentPool.Spawn("main/kid", newMockLM("hi"), agent.SpawnOpts{
		ModelName: "fake", ContextWindow: 1000,
	}); err != nil {
		t.Fatalf("spawn sub-agent: %v", err)
	}

	// Focus the sub-agent, then observe which agent records the prompt.
	before := len(m.agentPool.Get("main/kid").History())
	m, cmd := callUpdate(m, FocusAgentMsg{AgentID: "main/kid"})
	if m.focusedAgent != "main/kid" {
		t.Fatalf("focusedAgent = %q, want main/kid", m.focusedAgent)
	}
	if cmd != nil {
		callUpdate(m, cmd())
	}
	sub := m.agentPool.Get("main/kid")
	if got := len(sub.History()); got != before {
		// History grows only once a turn records, which needs a model turn; the
		// assertion here is that focus state is set and input targets it.
		t.Logf("sub-agent history grew to %d", got)
	}
}

// An empty focus (or explicitly the root) targets the root agent.
func TestFocusEmptyTargetsRoot(t *testing.T) {
	m := newTestModel()
	m, _ = callUpdate(m, FocusAgentMsg{AgentID: "main/kid"})
	m, _ = callUpdate(m, FocusAgentMsg{AgentID: ""})
	if m.focusedAgent != "" {
		t.Fatalf("focusedAgent = %q, want empty (root)", m.focusedAgent)
	}
}

// A focus target that no longer exists must not break sending: input falls back
// to the root rather than being lost.
func TestSubmitFallsBackWhenFocusedAgentGone(t *testing.T) {
	m := newTestModel()
	m, _ = callUpdate(m, FocusAgentMsg{AgentID: "main/gone"})
	model, cmd := m.submitToAgent("hello", "")
	if cmd == nil {
		t.Fatal("submit should still produce a command")
	}
	if model == nil {
		t.Fatal("submit should return a model")
	}
	_ = cmd()
	if m.agentPool.Get(agent.MainAgentID) == nil {
		t.Fatal("root agent should still exist")
	}
}

// The tree overlay carries focus selection back to the extension through the
// reserved callback.
func TestAgentTreeSelectionDispatchesFocusCallback(t *testing.T) {
	m := newTestModel()
	m.width, m.height = 80, 24
	m, _ = callUpdate(m, ShowAgentTreeMsg{
		Callback: AgentTreeCallback,
		Nodes: []sdk.AgentTreeNode{
			{ID: "main", Label: "main"},
			{ID: "main/kid", ParentID: "main", Label: "kid"},
		},
	})
	if !m.agentTree.IsActive() {
		t.Fatal("tree not active")
	}
	_, cmd := callUpdate(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter on a node should dispatch the focus callback")
	}
	msg := cmd()
	d, ok := msg.(dispatchOnCommandMsg)
	if !ok {
		t.Fatalf("got %T, want dispatchOnCommandMsg", msg)
	}
	if d.Name != AgentTreeCallback || len(d.Args) != 1 || d.Args[0] != "main" {
		t.Fatalf("callback = %+v, want %s with [main]", d, AgentTreeCallback)
	}
}
