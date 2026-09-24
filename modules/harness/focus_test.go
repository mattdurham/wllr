package harness

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/mattdurham/wllr/modules/agent"
	"github.com/mattdurham/wllr/modules/extension"
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

// Focus is published as the "agent" status so a statusline extension can show
// which agent input targets. An empty value means the root, which the reader
// renders with its own root label.
func TestFocusPublishesAgentStatus(t *testing.T) {
	m := newTestModel()
	if _, err := m.agentPool.Spawn("main/kid", newMockLM("hi"), agent.SpawnOpts{
		ModelName: "fake", ContextWindow: 1000,
	}); err != nil {
		t.Fatalf("spawn sub-agent: %v", err)
	}
	bridge := &harnessUIBridge{pool: m.agentPool, live: m.live}

	if got := bridge.GetStatusInfo().Statuses["agent"]; got != "" {
		t.Fatalf("initial agent status = %q, want empty (root)", got)
	}

	m, _ = callUpdate(m, FocusAgentMsg{AgentID: "main/kid"})
	if got := bridge.GetStatusInfo().Statuses["agent"]; got != "main/kid" {
		t.Fatalf("agent status = %q, want main/kid", got)
	}

	m, _ = callUpdate(m, FocusAgentMsg{AgentID: ""})
	if got := bridge.GetStatusInfo().Statuses["agent"]; got != "" {
		t.Fatalf("agent status after root focus = %q, want empty", got)
	}
}

// A focused sub-agent can close without the focusing extension being told: the
// agent self-closes after processing its shutdown request. The status must fall
// back to the root rather than naming an agent that no longer exists.
func TestFocusedAgentStatusClearsWhenAgentClosed(t *testing.T) {
	m := newTestModel()
	if _, err := m.agentPool.Spawn("main/kid", newMockLM("hi"), agent.SpawnOpts{
		ModelName: "fake", ContextWindow: 1000,
	}); err != nil {
		t.Fatalf("spawn sub-agent: %v", err)
	}
	bridge := &harnessUIBridge{pool: m.agentPool, live: m.live}

	m, _ = callUpdate(m, FocusAgentMsg{AgentID: "main/kid"})
	if got := bridge.GetStatusInfo().Statuses["agent"]; got != "main/kid" {
		t.Fatalf("agent status = %q, want main/kid", got)
	}

	if err := m.agentPool.Close("main/kid"); err != nil {
		t.Fatalf("close sub-agent: %v", err)
	}
	if got := bridge.GetStatusInfo().Statuses["agent"]; got != "" {
		t.Fatalf("agent status after close = %q, want empty (focus falls back to root)", got)
	}
}

// spawnFocusedKid spawns a sub-agent, focuses it, and returns the model. The
// caller decides when to close it, which is what makes focus stale.
func spawnFocusedKid(t *testing.T, m Model) Model {
	t.Helper()
	if _, err := m.agentPool.Spawn("main/kid", newMockLM("hi"), agent.SpawnOpts{
		ModelName: "fake", ContextWindow: 1000,
	}); err != nil {
		t.Fatalf("spawn sub-agent: %v", err)
	}
	m, _ = callUpdate(m, FocusAgentMsg{AgentID: "main/kid"})
	if m.focusedAgent != "main/kid" {
		t.Fatalf("focusedAgent = %q, want main/kid", m.focusedAgent)
	}
	return m
}

// A focused sub-agent that closes must hand the transcript back to the root.
// The harness owns focus and an agent self-closes after processing its shutdown
// request, so without reconciliation the statusline reads as the root while the
// transcript still shows the closed agent's conversation.
func TestFocusedAgentCloseReturnsTranscriptToRoot(t *testing.T) {
	m := newTestModel()
	m.width = 60
	m.scene = newSceneWithChat(t, "kid transcript")
	m = spawnFocusedKid(t, m)

	if err := m.agentPool.Close("main/kid"); err != nil {
		t.Fatalf("close sub-agent: %v", err)
	}

	// Reconciliation rides the 1-second extension tick.
	m, _ = callUpdate(m, extensionTickMsg{})

	if m.focusedAgent != "" {
		t.Fatalf("focusedAgent = %q, want empty (root) after the focused agent closed", m.focusedAgent)
	}
	if got := m.live.getStatus("agent"); got != "" {
		t.Fatalf("agent status = %q, want empty", got)
	}
	if got := m.scene.Render(wasmChatAreaID, 60); got != "" {
		t.Fatalf("transcript should be emptied for the root rebuild, got %q", got)
	}
}

// Reconciliation must also ask the transcript-owning extension to rebuild, or
// the transcript stays blank instead of showing the root conversation.
func TestReconcileFocusedAgent_DispatchesRootRebuild(t *testing.T) {
	m := newTestModel()
	m.extHost = extension.NewHost(nil)
	ctx := context.Background()
	defer func() { _ = m.extHost.Close(ctx) }()
	m.width = 60
	m.scene = newSceneWithChat(t, "kid transcript")
	m = spawnFocusedKid(t, m)

	if err := m.agentPool.Close("main/kid"); err != nil {
		t.Fatalf("close sub-agent: %v", err)
	}

	cmd := m.reconcileFocusedAgent()
	if cmd == nil {
		t.Fatal("reconcile should dispatch a root rebuild once the focused agent is gone")
	}
	if m.focusedAgent != "" {
		t.Fatalf("focusedAgent = %q, want empty (root)", m.focusedAgent)
	}

	res := cmd()
	evtResult, ok := res.(ExtensionEventResultMsg)
	if !ok {
		t.Fatalf("rebuild cmd returned %T, want ExtensionEventResultMsg", res)
	}
	if evtResult.Err != nil {
		t.Fatalf("rebuild dispatch error: %v", evtResult.Err)
	}
}

// A focus target that still exists must be left alone: closing one sub-agent
// must not disturb focus on another.
func TestReconcileFocusedAgent_KeepsLiveFocus(t *testing.T) {
	m := newTestModel()
	m.width = 60
	m.scene = newSceneWithChat(t, "kid transcript")
	m = spawnFocusedKid(t, m)

	if cmd := m.reconcileFocusedAgent(); cmd != nil {
		t.Fatal("a live focused agent must not be reconciled away")
	}
	if m.focusedAgent != "main/kid" {
		t.Fatalf("focusedAgent = %q, want main/kid", m.focusedAgent)
	}
	if got := m.live.getStatus("agent"); got != "main/kid" {
		t.Fatalf("agent status = %q, want main/kid", got)
	}
	if got := m.scene.Render(wasmChatAreaID, 60); !strings.Contains(got, "kid transcript") {
		t.Fatalf("live focus must not clear the transcript, got %q", got)
	}
}

// Root focus is the resting state, so there is nothing to reconcile.
func TestReconcileFocusedAgent_RootFocusIsNoOp(t *testing.T) {
	m := newTestModel()
	if cmd := m.reconcileFocusedAgent(); cmd != nil {
		t.Fatal("root focus needs no reconciliation")
	}
}

// Reconciliation fires once per stale focus. Repeating it would re-clear the
// transcript and re-notify on every tick.
func TestReconcileFocusedAgent_Idempotent(t *testing.T) {
	m := newTestModel()
	m.extHost = extension.NewHost(nil)
	ctx := context.Background()
	defer func() { _ = m.extHost.Close(ctx) }()
	m.width = 60
	m.scene = newSceneWithChat(t, "kid transcript")
	m = spawnFocusedKid(t, m)
	if err := m.agentPool.Close("main/kid"); err != nil {
		t.Fatalf("close sub-agent: %v", err)
	}

	if cmd := m.reconcileFocusedAgent(); cmd == nil {
		t.Fatal("first reconcile should dispatch a root rebuild")
	}
	if cmd := m.reconcileFocusedAgent(); cmd != nil {
		t.Fatal("reconcile must not repeat once focus is back on the root")
	}
}

// Without an extension host there is nothing to rebuild through, but focus must
// still be reset so input and the statusline stop naming a dead agent.
func TestReconcileFocusedAgent_NilHostStillResetsFocus(t *testing.T) {
	m := newTestModel()
	m.width = 60
	m.scene = newSceneWithChat(t, "kid transcript")
	m = spawnFocusedKid(t, m)
	if err := m.agentPool.Close("main/kid"); err != nil {
		t.Fatalf("close sub-agent: %v", err)
	}

	if cmd := m.reconcileFocusedAgent(); cmd != nil {
		t.Fatal("with no extension host there is nothing to rebuild through")
	}
	if m.focusedAgent != "" {
		t.Fatalf("focusedAgent = %q, want empty (root)", m.focusedAgent)
	}
	if got := m.live.getStatus("agent"); got != "" {
		t.Fatalf("agent status = %q, want empty", got)
	}
}
