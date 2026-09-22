package harness

import (
	"testing"

	"github.com/mattdurham/wllr/modules/sdk"
)

func TestShowAgentTreeMsgOpensOverlay(t *testing.T) {
	m := newTestModel()
	m.width, m.height = 80, 24

	msg := ShowAgentTreeMsg{
		Callback: AgentTreeCallback,
		Nodes: []sdk.AgentTreeNode{
			{ID: "main", Label: "main"},
			{ID: "main/kid", ParentID: "main", Label: "kid"},
		},
	}
	m, _ = callUpdate(m, msg)
	if !m.agentTree.IsActive() {
		t.Fatal("tree overlay did not open from ShowAgentTreeMsg")
	}
	t.Logf("tree active with %d nodes", len(m.agentTree.Nodes))
}
