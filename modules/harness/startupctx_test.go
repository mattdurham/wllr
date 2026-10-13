package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"testing"

	"github.com/mattdurham/wllr/modules/agent"
	"github.com/mattdurham/wllr/modules/sdk"
	"github.com/mattdurham/wllr/modules/testutil"
)

// newWindowedModel builds a harness model whose main agent has a known context
// window, so refreshContextStatus can paint the ctx segment.
func newWindowedModel(t *testing.T) Model {
	t.Helper()
	pool := agent.NewPool()
	if _, err := pool.Spawn(
		agent.MainAgentID,
		testutil.NewFakeLMWithResponses("ok"),
		agent.SpawnOpts{ModelName: "fake-model", ContextWindow: 128_000},
	); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	return New(pool, agent.MainAgentID, nil)
}

// TestResetHistoryMsg_PaintsContextStatus verifies that restoring a conversation
// rewrites the live ctx status in the same update. The bridge has already
// reseeded the agent's usage baseline by this point; without the refresh the
// statusline would keep showing the previous value (or nothing).
func TestResetHistoryMsg_PaintsContextStatus(t *testing.T) {
	m := newWindowedModel(t)
	if got := m.live.getStatus("ctx"); got != "" {
		t.Fatalf("precondition: ctx should be empty before reset, got %q", got)
	}

	next, _ := m.Update(ResetHistoryMsg{Messages: []sdk.Message{
		{Role: sdk.RoleUser, Content: "restored question"},
	}})

	if got := next.(Model).live.getStatus("ctx"); got == "" {
		t.Fatal("ResetHistoryMsg did not paint ctx; statusline would show the old/absent value")
	}
}

// TestExtensionTick_PaintsContextStatusWhileIdle verifies the idle-tick refresh:
// the stream tick only runs during a turn, so the 1-second extension tick is
// what keeps (or first shows) the ctx segment while the agent is idle.
func TestExtensionTick_PaintsContextStatusWhileIdle(t *testing.T) {
	m := newWindowedModel(t)

	next, _, handled := m.updateExtension(extensionTickMsg{})
	if !handled {
		t.Fatal("extensionTickMsg not handled")
	}
	if got := next.live.getStatus("ctx"); got == "" {
		t.Fatal("idle tick did not paint ctx; segment would vanish between turns")
	}
}
