package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/mattdurham/wllr/modules/agent"
	"github.com/mattdurham/wllr/modules/extension"
	"github.com/mattdurham/wllr/modules/sdk"
)

// TestUIBridgeEngineMutationsRoutedThroughFuncSink proves the UI bridge is
// front-end agnostic: driven by a programmatic sdk.FuncSink instead of the TUI,
// it still performs the engine-local mutations (base prompt, history reset,
// tool bookkeeping) and forwards the user-visible effect to the sink.
func TestUIBridgeEngineMutationsRoutedThroughFuncSink(t *testing.T) {
	pool := agent.NewPool()
	if _, err := pool.Spawn(agent.MainAgentID, newMockLM("hi"), agent.SpawnOpts{
		ModelName: "fake", ContextWindow: 1000,
	}); err != nil {
		t.Fatalf("spawn: %v", err)
	}

	var gotReset []sdk.Message
	var gotToolDone bool
	var gotModel string
	sink := &sdk.FuncSink{
		OnResetHistory: func(m []sdk.Message) { gotReset = m },
		OnToolCallDone: func(agentID, id, toolName, output string, isError bool) {
			gotToolDone = agentID == agent.MainAgentID && toolName == "read_file" && output == "contents"
		},
		OnSetModel: func(value, thinking string) error { gotModel = value; return nil },
	}
	b := &harnessUIBridge{pool: pool, sink: sink, mainID: agent.MainAgentID}

	b.SetSystemPrompt("base")
	if got := pool.BaseSystemPrompt(); got != "base" {
		t.Fatalf("BaseSystemPrompt = %q, want base", got)
	}

	msgs := []sdk.Message{{Role: sdk.RoleUser, Content: "a"}}
	if err := b.ResetHistory(msgs); err != nil {
		t.Fatalf("ResetHistory: %v", err)
	}
	if got := pool.Get(agent.MainAgentID).History(); len(got) != 1 {
		t.Fatalf("pool history len = %d, want 1", len(got))
	}
	if len(gotReset) != 1 {
		t.Fatalf("sink ResetHistory not called: %+v", gotReset)
	}

	b.AfterToolCall(agent.MainAgentID, "c1", "read_file", "contents", false)
	if !gotToolDone {
		t.Fatal("sink ToolCallDone not called with expected fields")
	}

	if err := b.SetModel("m", ""); err != nil {
		t.Fatalf("SetModel: %v", err)
	}
	if gotModel != "m" {
		t.Fatalf("sink SetModel got %q, want m", gotModel)
	}
}

// TestAgentBridgeWakeupRoutedThroughSink proves the agent bridge reports a
// wakeup through the sink rather than a tea.Program, and only for the root
// agent (sub-agents have no stream state to indicate).
func TestAgentBridgeWakeupRoutedThroughSink(t *testing.T) {
	pool := agent.NewPool()
	if _, err := pool.Spawn(agent.MainAgentID, newMockLM("hi"), agent.SpawnOpts{
		ModelName: "fake", ContextWindow: 1000,
	}); err != nil {
		t.Fatalf("spawn: %v", err)
	}

	var woke string
	sink := &sdk.FuncSink{OnAgentWakeup: func(id string) { woke = id }}
	b := &harnessAgentBridge{pool: pool, sink: sink, mainID: agent.MainAgentID}

	// Run reports the wake before starting the drain turn.
	if err := b.Run(agent.MainAgentID); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if woke != agent.MainAgentID {
		t.Fatalf("AgentWakeup got %q, want %q", woke, agent.MainAgentID)
	}

	// A non-root wake must not fire (sub-agents have no stream state).
	woke = ""
	_ = b.Run("main/kid")
	if woke != "" {
		t.Fatalf("AgentWakeup fired for sub-agent: %q", woke)
	}
}

// TestInstallHeadlessBridgesEnablesSpawner is the regression for the serve-mode
// gap: before the sink refactor, serve installed only the no-op earlyAgentBridge
// stub, so create_agent failed with "session not yet started". After
// InstallHeadlessBridges the host carries a real, spawner-backed AgentBridge, so
// a spawn is attempted for real (and fails only for want of a configured model,
// not because the host is a stub).
func TestInstallHeadlessBridgesEnablesSpawner(t *testing.T) {
	pool := agent.NewPool()
	host := extension.NewHost(slog.New(slog.NewTextHandler(io.Discard, nil)))

	InstallHeadlessBridges(host, pool, agent.MainAgentID, nil, &sdk.FuncSink{})

	if !host.AgentBridgeSet() {
		t.Fatal("host.AgentBridgeSet() = false, want true")
	}
	ab := host.AgentBridge()
	if ab == nil {
		t.Fatal("host.AgentBridge() = nil")
	}
	err := ab.Spawn(context.Background(), extension.SpawnRequest{ID: "main/kid", Name: "kid"})
	if err == nil {
		t.Fatal("Spawn with no provider: want error")
	}
	if strings.Contains(err.Error(), "not yet started") {
		t.Fatalf("stub bridge still installed: %v", err)
	}
	if strings.Contains(err.Error(), "spawner not initialized") {
		t.Fatalf("real bridge installed without a spawner: %v", err)
	}
}

func TestUIBridgeNilSinkIsSafe(t *testing.T) {
	b := &harnessUIBridge{pool: nil, sink: nil, mainID: agent.MainAgentID}
	b.Notify("x")
	b.ShowModal("x")
	b.ConsoleOutput("x")
	b.ConsoleClear()
	b.SetFocusedAgent("")
	if err := b.SetModel("m", ""); err != nil {
		t.Fatalf("SetModel with nil sink: %v", err)
	}
	if err := b.CreateArea(sdk.UIArea{ID: "chat"}); err != nil {
		t.Fatalf("CreateArea with nil sink: %v", err)
	}
	if got := b.GetStatusInfo(); got.Statuses == nil {
		t.Fatal("GetStatusInfo with nil sink returned nil map")
	}
}
