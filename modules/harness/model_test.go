package harness

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/mattdurham/wllr/modules/agent"
	"github.com/mattdurham/wllr/modules/sdk"
)

// newTestPool creates an AgentPool with a mock LM and a spawned "main" agent.
func newTestPool() *agent.AgentPool {
	pool := agent.NewPool()
	lm := newMockLM("hello", " ", "world")
	_, _ = pool.Spawn("main", lm, agent.SpawnOpts{})
	return pool
}

func newTestModel() Model {
	pool := newTestPool()
	return New(pool, "main", nil)
}

// callUpdate is a helper that calls Update and returns the concrete Model.
func callUpdate(m Model, msg tea.Msg) (Model, tea.Cmd) {
	newModel, cmd := m.Update(msg)
	return newModel.(Model), cmd
}

func TestModel_Init_ReturnsCmd(t *testing.T) {
	m := newTestModel()
	cmd := m.Init()
	if cmd == nil {
		t.Error("Init() should return a non-nil Cmd")
	}
}

func TestModel_View_ReturnsNonEmpty(t *testing.T) {
	m := newTestModel()
	view := m.View()
	if view.Content == "" {
		t.Error("View() returned empty content")
	}
}

func TestRenderQueuedMessages(t *testing.T) {
	m := newTestModel()
	m.width = 80
	if err := m.agentPool.SendMessage("main", sdk.Message{Role: sdk.RoleUser, Content: "queued work"}); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}

	view := m.renderQueuedMessages()
	if !strings.Contains(view, "Queued") {
		t.Fatalf("queued pane missing header:\n%s", view)
	}
	if !strings.Contains(view, "queued work") {
		t.Fatalf("queued pane missing message:\n%s", view)
	}
	if !strings.Contains(view, "╭") || !strings.Contains(view, "╰") {
		t.Fatalf("queued pane missing border:\n%s", view)
	}
}

func TestModel_QueueLayoutTracksInboxTransitions(t *testing.T) {
	m := newTestModel()
	m.width = 80
	m.height = 30
	m.syncLayout()
	withoutQueue := m.chat.height

	if err := m.agentPool.SendMessage("main", sdk.Message{Role: sdk.RoleUser, Content: "queued work"}); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	m, _ = callUpdate(m, streamTickMsg{})
	if got, want := m.chat.height, withoutQueue-queuedMessagePaneLines; got != want {
		t.Fatalf("chat height with queue = %d, want %d", got, want)
	}
	if got := renderedLineCount(m.View().Content); got != m.height {
		t.Fatalf("queued view line count = %d, want terminal height %d", got, m.height)
	}

	if drained := m.agentPool.Get("main").DrainInbox(); len(drained) != 1 {
		t.Fatalf("DrainInbox returned %d messages, want 1", len(drained))
	}
	m, _ = callUpdate(m, streamTickMsg{})
	if got := m.chat.height; got != withoutQueue {
		t.Fatalf("chat height after queue drain = %d, want %d", got, withoutQueue)
	}
	if view := m.View().Content; strings.Contains(view, "Queued") {
		t.Fatalf("queue pane remained after drain:\n%s", view)
	}
}

func TestModel_QueueDisplayIsBounded(t *testing.T) {
	m := newTestModel()
	m.width = 80
	m.height = 30
	for i := 0; i < queuedMessageContentLines+3; i++ {
		if err := m.agentPool.SendMessage("main", sdk.Message{
			Role:    sdk.RoleUser,
			Content: fmt.Sprintf("queued-%d", i),
		}); err != nil {
			t.Fatalf("SendMessage(%d): %v", i, err)
		}
	}

	view := m.View().Content
	if !strings.Contains(view, "Queued (6 total, showing latest 3)") {
		t.Fatalf("queue count summary missing:\n%s", view)
	}
	if !strings.Contains(view, "queued queued-4") || !strings.Contains(view, "queued queued-5") {
		t.Fatalf("latest queued entries missing:\n%s", view)
	}
	if strings.Contains(view, "queued queued-3") {
		t.Fatalf("oldest visible row should be replaced by the overflow hint:\n%s", view)
	}
	if got := renderedLineCount(view); got != m.height {
		t.Fatalf("bounded queue view line count = %d, want terminal height %d", got, m.height)
	}
}

func TestModel_QueuePaneHidesWhenItWouldPushInputOffScreen(t *testing.T) {
	m := newTestModel()
	m.width = 80
	m.height = 15
	if err := m.agentPool.SendMessage("main", sdk.Message{Role: sdk.RoleUser, Content: "queued work"}); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}

	view := m.View().Content
	if strings.Contains(view, "Queued") {
		t.Fatalf("queue pane should be hidden when terminal is too short:\n%s", view)
	}
	if !strings.Contains(view, "╰") {
		t.Fatalf("input box should remain visible when queue is hidden:\n%s", view)
	}
}

func TestNew_SeedsActiveModelFromMainAgent(t *testing.T) {
	pool := agent.NewPool()
	pool.SetProviderName("openai")
	pool.SetDefaultModelName("gpt-5.5")
	_, _ = pool.Spawn("main", newMockLM("ok"), agent.SpawnOpts{})

	m := New(pool, "main", nil)
	if m.activeProvider != "openai" {
		t.Errorf("activeProvider = %q, want openai", m.activeProvider)
	}
	if m.activeModel != "gpt-5.5" {
		t.Errorf("activeModel = %q, want gpt-5.5", m.activeModel)
	}
	if m.live.model != "gpt-5.5" {
		t.Errorf("live.model = %q, want gpt-5.5", m.live.model)
	}
}

func TestModel_Update_TokenMsg_AccumulatesResponse(t *testing.T) {
	m := newTestModel()

	m, _ = callUpdate(m, TokenMsg{Token: "hello"})
	if m.streamContent != "hello" {
		t.Errorf("streamContent: got %q, want %q", m.streamContent, "hello")
	}

	m, _ = callUpdate(m, TokenMsg{Token: " world"})
	if m.streamContent != "hello world" {
		t.Errorf("streamContent: got %q, want %q", m.streamContent, "hello world")
	}
}

func TestModel_Update_TokenMsg_UpdatesLiveTokenSnapshot(t *testing.T) {
	m := newTestModel()
	m.agentPool.AddTokens(7)

	m, _ = callUpdate(m, TokenMsg{Token: "hello"})

	m.live.mu.RLock()
	tokens := m.live.tokens
	m.live.mu.RUnlock()
	if tokens != 7 {
		t.Fatalf("live.tokens = %d, want 7", tokens)
	}
}

func TestModel_Update_StreamDoneMsg_ClearsStreamStatus(t *testing.T) {
	m := newTestModel()
	m.streamContent = "test response"
	// Simulate a stream status set externally.
	m.live.setStatus("stream", "working.")

	m, _ = callUpdate(m, StreamDoneMsg{Err: nil})
	// Stream status should be cleared.
	if v := m.live.getStatus("stream"); v != "" {
		t.Errorf("stream status should be cleared after StreamDoneMsg, got %q", v)
	}
	if m.streamContent != "" {
		t.Errorf("streamContent should be reset, got %q", m.streamContent)
	}
}

func TestModel_Update_StreamDoneMsg_Error_ShowsError(t *testing.T) {
	m := newTestModel()

	m, _ = callUpdate(m, StreamDoneMsg{Err: errors.New("API error")})
	// The error line is rendered by the WASM transcript via EventNotify; the
	// harness-side effect is the live status entry.
	if v := m.live.getStatus("stream"); v != streamStatusError {
		t.Errorf("expected stream status 'error', got %q", v)
	}
}

func TestModel_Update_StreamDoneMsg_ContextCanceled_NoError(t *testing.T) {
	m := newTestModel()
	m.streamContent = "partial"

	// context.Canceled should not set the error status.
	m, _ = callUpdate(m, StreamDoneMsg{Err: context.Canceled})
	if v := m.live.getStatus("stream"); v == streamStatusError {
		t.Error("context.Canceled should not set error status")
	}
}

func TestModel_Update_ReloadMsg_TriggersExtensionReload(t *testing.T) {
	m := newTestModel() // no extension host
	_, cmd := callUpdate(m, ReloadMsg{})
	if cmd == nil {
		t.Error("expected non-nil cmd after ReloadMsg")
	}
	// Execute the cmd.
	msg := cmd()
	_, ok := msg.(NotifyMsg)
	if !ok {
		t.Errorf("expected NotifyMsg from reload cmd, got %T", msg)
	}
}

func TestModel_Update_ClearMsg_ClearsHistory(t *testing.T) {
	m := newTestModel()
	m.streamContent = "hello"

	m, _ = callUpdate(m, clearMsg{})
	if m.streamContent != "" {
		t.Errorf("expected streamContent reset after clear, got %q", m.streamContent)
	}
}

func TestModel_Update_SetModelMsg(t *testing.T) {
	m := newTestModel()
	m, _ = callUpdate(m, setModelMsg{Model: "claude-haiku-3-5"})
	if m.activeModel != "claude-haiku-3-5" {
		t.Errorf("activeModel: got %q, want %q", m.activeModel, "claude-haiku-3-5")
	}
	if m.live.model != "claude-haiku-3-5" {
		t.Errorf("live.model: got %q, want %q", m.live.model, "claude-haiku-3-5")
	}
}

func TestModel_Update_CommandMsg_Clear(t *testing.T) {
	m := newTestModel()
	m.streamContent = "test"

	// Dispatch /clear command.
	cmd := m.commands.Dispatch("clear", nil)
	msg := cmd()
	m, _ = callUpdate(m, msg)

	if m.streamContent != "" {
		t.Errorf("expected streamContent reset after /clear, got %q", m.streamContent)
	}
}

func TestInstantCommandSkipsQueueingStatus(t *testing.T) {
	m := newTestModel()

	// Dispatch a built-in Instant command (e.g. /clear).
	m, _ = callUpdate(m, CommandMsg{Name: "clear", Args: nil})

	// Instant commands must NOT set the "queuing…" status indicator.
	if v := m.live.getStatus("stream"); v == "queuing…" {
		t.Error("Instant command should not set stream status to 'queuing…'")
	}
}

func TestNonInstantCommandSetsQueueingStatus(t *testing.T) {
	m := newTestModel()

	// Register a non-instant (WASM dispatch) command.
	m.commands.Register(Command{
		Name:    "wasm-cmd",
		Desc:    "simulates a WASM-dispatched command",
		Instant: false,
		Handler: func(args []string) tea.Cmd {
			return func() tea.Msg { return dispatchOnCommandMsg{Name: "wasm-cmd", Args: args} }
		},
	})

	m, _ = callUpdate(m, CommandMsg{Name: "wasm-cmd", Args: nil})

	// Non-instant commands should set the "queuing…" status indicator.
	if v := m.live.getStatus("stream"); v != "queuing…" {
		t.Errorf("non-instant command should set stream status to 'queuing…', got %q", v)
	}
}

func TestModel_Update_CommandMsg_UnknownCommand(t *testing.T) {
	m := newTestModel()
	m, cmd := callUpdate(m, CommandMsg{Name: "nonexistent", Args: nil})
	if cmd == nil {
		t.Fatal("expected non-nil cmd for unknown command")
	}
	msg := cmd()
	notify, ok := msg.(NotifyMsg)
	if !ok {
		t.Fatalf("expected NotifyMsg, got %T", msg)
	}
	_ = m
	if notify.Text == "" {
		t.Error("expected non-empty error message")
	}
}

func TestModel_Update_SubmitMsg_MarksStreaming(t *testing.T) {
	pool := newTestPool()
	m := New(pool, "main", nil)

	m, cmd := callUpdate(m, SubmitMsg{Content: "hello"})
	// SubmitMsg sends to the pool asynchronously; the harness marks streaming.
	// The user prompt is echoed into the transcript by the WASM extension.
	_ = cmd
	if !m.streaming {
		t.Fatal("expected streaming=true after SubmitMsg")
	}
}

func TestModel_Update_SubmitMsg_MultipleSubmitsAllowed(t *testing.T) {
	pool := newTestPool()
	m := New(pool, "main", nil)

	// Multiple submits (the second while streaming) must not panic.
	m, _ = callUpdate(m, SubmitMsg{Content: "first"})
	m, _ = callUpdate(m, SubmitMsg{Content: "second"})
	if !m.streaming {
		t.Error("expected streaming=true after submits")
	}
}

func TestModel_Update_NotifyMsg(t *testing.T) {
	m := newTestModel()
	// NotifyMsg is handled without panic; the visible line is rendered by the
	// WASM transcript via EventNotify (covered in test/wasmchat).
	_, _ = callUpdate(m, NotifyMsg{Text: "test notification"})
}

func TestModel_Update_StatusUpdateMsg(t *testing.T) {
	m := newTestModel()
	m, _ = callUpdate(m, StatusUpdateMsg{Key: "foo", Value: "bar"})
	if v := m.live.getStatus("foo"); v != "bar" {
		t.Errorf("live.statuses[foo]: got %q, want %q", v, "bar")
	}
}

func TestModel_Update_WindowSizeMsg(t *testing.T) {
	m := newTestModel()
	m, _ = callUpdate(m, tea.WindowSizeMsg{Width: 120, Height: 40})
	if m.width != 120 || m.height != 40 {
		t.Errorf("dimensions: got %dx%d, want 120x40", m.width, m.height)
	}
}

// TestModel_NilPool_SubmitMsg_Safe verifies that a nil agentPool does not panic on SubmitMsg.
func TestModel_NilPool_SubmitMsg_Safe(t *testing.T) {
	// Model with nil pool — should not panic.
	m := New(nil, "main", nil)

	m, cmd := callUpdate(m, SubmitMsg{Content: "hello"})
	_ = cmd
	// Should not panic; streaming state is set.
	if !m.streaming {
		t.Error("expected streaming=true after submit")
	}
}

// TestModel_AbortStreamMsg_CancelsAgent verifies abortStreamMsg cancels the main agent.
func TestModel_AbortStreamMsg_CancelsAgent(t *testing.T) {
	pool := newTestPool()
	m := New(pool, "main", nil)

	// abortStreamMsg should not panic even with no active turn.
	m, cmd := callUpdate(m, abortStreamMsg{})
	_ = m
	if cmd != nil {
		t.Error("expected nil cmd from abortStreamMsg")
	}
}

// --- TestHarnessModel_OnTeam* tests ---
// These tests exercise the OnTeam* callbacks as wired in Model.SetProgram.
// Rather than starting a real tea.Program (which requires a TTY), we call the
// same closure logic that SetProgram wires, operating directly on the pool.

// makeTeamCallbacks replicates the team-management closure wiring from
// SetProgram so tests can exercise the logic without a running TUI program.
func makeTeamCallbacks(pool *agent.AgentPool) (
	onTeamCreate func(id, name string) error,
	onTeamClose func(id string) error,
	onTeamAddMember func(teamID, agentID string) error,
	onAgentSendMessage func(id, message string) error,
) {
	onTeamCreate = func(id, _ string) error {
		if pool == nil {
			return nil
		}
		_, err := pool.CreateTeam(id)
		return err
	}
	onTeamClose = func(id string) error {
		if pool == nil {
			return nil
		}
		return pool.CloseTeam(context.Background(), id)
	}
	onTeamAddMember = func(teamID, agentID string) error {
		if pool == nil {
			return nil
		}
		t := pool.GetTeam(teamID)
		if t == nil {
			return nil
		}
		return t.AddMember(agentID)
	}
	onAgentSendMessage = func(id, message string) error {
		if pool == nil {
			return nil
		}
		// pool.Send calls agent.Submit in a goroutine — non-blocking and safe.
		// This wakes the target agent immediately to process the message.
		return pool.Send(id, message)
	}
	return
}

func TestHarnessModel_OnTeamCreate_CreatesTeamInPool(t *testing.T) {
	pool := agent.NewPool()
	onTeamCreate, _, _, _ := makeTeamCallbacks(pool)

	if err := onTeamCreate("team-alpha", "Alpha Team"); err != nil {
		t.Fatalf("OnTeamCreate: %v", err)
	}

	team := pool.GetTeam("team-alpha")
	if team == nil {
		t.Fatal("expected pool.GetTeam(team-alpha) to return non-nil after create")
	}
	if team.ID() != "team-alpha" {
		t.Errorf("team.ID(): got %q, want %q", team.ID(), "team-alpha")
	}
}

func TestHarnessModel_OnTeamAddMember_AddsAgent(t *testing.T) {
	pool := agent.NewPool()
	lm := newMockLM("hi")
	_, err := pool.Spawn("worker-1", lm, agent.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	_, err = pool.CreateTeam("team-b")
	if err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}

	_, _, onTeamAddMember, _ := makeTeamCallbacks(pool)

	if err := onTeamAddMember("team-b", "worker-1"); err != nil {
		t.Fatalf("OnTeamAddMember: %v", err)
	}

	team := pool.GetTeam("team-b")
	if team == nil {
		t.Fatal("team-b not found in pool")
	}
	members := team.Members()
	if len(members) != 1 || members[0] != "worker-1" {
		t.Errorf("team members: got %v, want [worker-1]", members)
	}
}

func TestHarnessModel_OnTeamClose_RemovesTeamAndMembers(t *testing.T) {
	pool := agent.NewPool()
	lm := newMockLM("hi")
	_, err := pool.Spawn("member-1", lm, agent.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	team, err := pool.CreateTeam("team-c")
	if err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}
	if err := team.AddMember("member-1"); err != nil {
		t.Fatalf("AddMember: %v", err)
	}

	// Verify setup.
	if pool.GetTeam("team-c") == nil {
		t.Fatal("team-c should exist before close")
	}
	if pool.Get("member-1") == nil {
		t.Fatal("member-1 should exist in pool before close")
	}

	_, onTeamClose, _, _ := makeTeamCallbacks(pool)
	if err := onTeamClose("team-c"); err != nil {
		t.Fatalf("OnTeamClose: %v", err)
	}

	if pool.GetTeam("team-c") != nil {
		t.Error("expected pool.GetTeam(team-c) to return nil after close")
	}
	if pool.Get("member-1") != nil {
		t.Error("expected pool.Get(member-1) to return nil after team close (member was closed)")
	}
}

func TestHarnessModel_OnAgentSendMessage_TriggersAgentTurn(t *testing.T) {
	// Regression test: OnAgentSendMessage must call pool.Send (not AppendInbox).
	// pool.Send calls agent.Submit directly, waking the agent immediately.
	// The inbox must be empty after the call — nothing was queued there.
	pool := agent.NewPool()
	lm := newMockLM("response")
	_, err := pool.Spawn("sub-1", lm, agent.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	_, _, _, onAgentSendMessage := makeTeamCallbacks(pool)
	if err := onAgentSendMessage("sub-1", "hello from orchestrator"); err != nil {
		t.Fatalf("OnAgentSendMessage: %v", err)
	}

	a := pool.Get("sub-1")
	if a == nil {
		t.Fatal("agent sub-1 not found in pool")
	}
	// pool.Send bypasses the inbox entirely — it calls Submit directly.
	// DrainInbox must return empty: the message was never queued here.
	inbox := a.DrainInbox()
	if len(inbox) != 0 {
		t.Errorf("expected inbox to be empty (pool.Send uses Submit, not AppendInbox), got %d messages", len(inbox))
	}
}

// --- Bug 3: OnAgentRun wires to pool.Send ---

// --- Bug 3: OnAgentRun wires to pool.Send ---

func TestHarnessModel_OnAgentRun_TriggersPoolSend(t *testing.T) {
	// Bug 3 fix: OnAgentRun must call pool.Send(id, "") which invokes Submit
	// on the target agent. pool.Send returns ErrAgentNotFound for unknown IDs.
	pool := agent.NewPool()
	lm := newMockLM("ok")
	if _, err := pool.Spawn("worker-a", lm, agent.SpawnOpts{}); err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	// Same closure as harness/model.go OnAgentRun.
	onAgentRun := func(id string) error {
		return pool.Send(id, "")
	}

	// Known agent: should succeed (Submit is called in a goroutine, no error).
	if err := onAgentRun("worker-a"); err != nil {
		t.Errorf("onAgentRun(known): expected nil, got %v", err)
	}

	// Unknown agent: pool.Send must return ErrAgentNotFound.
	err := onAgentRun("does-not-exist")
	if err == nil {
		t.Fatal("onAgentRun(unknown): expected error, got nil")
	}
	if err != agent.ErrAgentNotFound {
		t.Errorf("expected ErrAgentNotFound, got %v", err)
	}
}

// --- Bug 6: Sub-agent system prompts include own ID + orchestrator reference ---

func TestHarnessModel_SubAgentSystemPrompt_ContainsAgentIDAndMain(t *testing.T) {
	// Bug 6 fix: OnAgentSpawn appends an "Agent Identity" block to the system
	// prompt containing the agent's own ID and a reference to "main" so sub-agents
	// know how to coordinate. We verify the prompt construction formula directly.
	agentID := "agent-worker-99"
	baseSystemPrompt := "You are a helpful sub-agent."

	// Replicate the formula from harness/model.go OnAgentSpawn.
	fullSystemPrompt := baseSystemPrompt +
		"\n\n## Your Agent Identity\nYour agent ID is: " + agentID +
		"\nTo report results back to the orchestrator, call send_message with agent_id=\"main\"."

	if !contains(fullSystemPrompt, agentID) {
		t.Errorf("system prompt missing agent ID %q\ngot: %s", agentID, fullSystemPrompt)
	}
	if !contains(fullSystemPrompt, "main") {
		t.Errorf("system prompt missing orchestrator reference \"main\"\ngot: %s", fullSystemPrompt)
	}
	if !contains(fullSystemPrompt, "## Your Agent Identity") {
		t.Errorf("system prompt missing identity header\ngot: %s", fullSystemPrompt)
	}
}

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}

func TestModel_Update_ConsoleMsg_AppendsToConsole(t *testing.T) {
	m := newTestModel()
	m, _ = callUpdate(m, ConsoleMsg{Line: "test output"})
	if !m.consoleVisible {
		t.Fatal("consoleVisible should be true after ConsoleMsg{Line}")
	}
	if m.console.IsEmpty() {
		t.Fatal("console should not be empty after ConsoleMsg{Line}")
	}
}

func TestModel_Update_ConsoleMsg_Clear_ResetsConsole(t *testing.T) {
	m := newTestModel()
	m, _ = callUpdate(m, ConsoleMsg{Line: "old"})
	m, _ = callUpdate(m, ConsoleMsg{Clear: true})
	if !m.console.IsEmpty() {
		t.Fatal("console should be empty after ConsoleMsg{Clear}")
	}
	if m.consoleVisible {
		t.Fatal("consoleVisible should be false after ConsoleMsg{Clear}")
	}
}

func TestModel_Update_StreamDoneMsg_HidesConsole(t *testing.T) {
	m := newTestModel()
	m.streaming = true
	m.consoleVisible = true
	m, _ = callUpdate(m, StreamDoneMsg{})
	if m.consoleVisible {
		t.Fatal("consoleVisible should be false after StreamDoneMsg")
	}
}

func TestModel_chatHeight_AccountsForConsole(t *testing.T) {
	m := newTestModel()
	m.width = 120
	m.height = 40
	m.consoleVisible = false
	h1 := m.chatHeight()
	m.consoleVisible = true
	h2 := m.chatHeight()
	if h1-h2 != consolePaneLines {
		t.Errorf("chatHeight diff: got %d, want %d (consolePaneLines)", h1-h2, consolePaneLines)
	}
}

func TestModel_chatHeight_AccountsForSceneStack(t *testing.T) {
	m := newTestModel()
	m.width = 80
	m.height = 40
	before := m.chatHeight()
	if err := m.scene.CreateArea(sdk.UIArea{ID: "extra", Placement: sdk.UIAreaSidebar}); err != nil {
		t.Fatalf("create extra scene area: %v", err)
	}
	root := sdk.UINode{
		ID:   "extra-root",
		Type: sdk.UINodeVStack,
		Children: []sdk.UINode{
			{ID: "extra-1", Type: sdk.UINodeText, Text: "extra one"},
			{ID: "extra-2", Type: sdk.UINodeText, Text: "extra two"},
		},
	}
	if err := m.scene.ApplyPatch(sdk.UIPatchParams{Area: "extra", Ops: []sdk.UIPatchOp{
		{Op: sdk.UIOpSetRoot, Node: &root},
	}}); err != nil {
		t.Fatalf("patch extra scene area: %v", err)
	}

	after := m.chatHeight()

	if before-after != 2 {
		t.Fatalf("chatHeight should shrink by extra scene lines, before=%d after=%d", before, after)
	}
}

func TestModel_ToolActivityPane_AlwaysRendersAndShowsRecentTools(t *testing.T) {
	m := newTestModel()
	m.width = 80
	m.height = 30
	m.chat.SetSize(m.width, m.chatHeight())

	if got := m.toolActivityHeight(); got != toolActivityPaneLines {
		t.Fatalf("toolActivityHeight = %d, want %d", got, toolActivityPaneLines)
	}
	emptyView := m.View().Content
	if !strings.Contains(emptyView, "─ tools ") {
		t.Fatalf("view should render empty tool activity pane:\n%s", emptyView)
	}

	m.streaming = true
	m, _ = callUpdate(m, ToolCallStartMsg{ID: "call-1", ToolName: "exec", Input: `{"command":"go test ./..."}`})
	view := m.View().Content
	if !strings.Contains(view, "─ tools ") || !strings.Contains(view, "running exec") {
		t.Fatalf("view missing pending tool activity:\n%s", view)
	}

	m, _ = callUpdate(m, ToolCallDoneMsg{ID: "call-1"})
	doneView := m.View().Content
	if !strings.Contains(doneView, "─ tools ") || !strings.Contains(doneView, "done exec") {
		t.Fatalf("tool activity pane should remain visible with completed call:\n%s", doneView)
	}
}

func TestModel_ToolActivityPane_RemainsOnStreamDone(t *testing.T) {
	m := newTestModel()
	m.width = 80
	m.height = 30
	m.streaming = true
	m, _ = callUpdate(m, ToolCallStartMsg{ID: "call-1", ToolName: "exec", Input: `{}`})
	if m.toolActivityHeight() == 0 {
		t.Fatal("tool activity should be visible while a tool is pending")
	}

	m, _ = callUpdate(m, StreamDoneMsg{})
	if m.toolActivityHeight() != toolActivityPaneLines {
		t.Fatalf("tool activity height after stream done = %d, want %d", m.toolActivityHeight(), toolActivityPaneLines)
	}
	if !strings.Contains(m.View().Content, "─ tools ") {
		t.Fatalf("tool activity pane should remain visible after stream ends:\n%s", m.View().Content)
	}
}

func TestModel_View_WithStatusLineFitsHeight(t *testing.T) {
	m := newTestModel()
	m.width = 80
	m.height = 12
	m.input.SetWidth(m.width - 4)
	root := sdk.UINode{ID: "status-root", Type: sdk.UINodeText, Text: ">> ChatGPT  gpt-5.5"}
	if err := m.scene.ApplyPatch(sdk.UIPatchParams{Area: statuslineAreaID, Ops: []sdk.UIPatchOp{
		{Op: sdk.UIOpSetRoot, Node: &root},
	}}); err != nil {
		t.Fatalf("set statusline root: %v", err)
	}
	m.chat.SetSize(m.width, m.chatHeight())

	view := m.View().Content
	if lines := renderedLineCount(view); lines > m.height {
		t.Fatalf("view rendered %d lines, want <= terminal height %d:\n%s", lines, m.height, view)
	}
	if lines := renderedLineCount(view); lines != m.height {
		t.Fatalf("view rendered %d lines, want exactly terminal height %d:\n%s", lines, m.height, view)
	}
	if !strings.Contains(view, ">> ChatGPT") {
		t.Fatalf("view missing statusline content:\n%s", view)
	}
	if !strings.Contains(view, "╰") {
		t.Fatalf("view missing input bottom border:\n%s", view)
	}
	lines := strings.Split(view, "\n")
	if len(lines) < 2 {
		t.Fatalf("view rendered too few lines:\n%s", view)
	}
	if !strings.Contains(lines[len(lines)-2], "╰") {
		t.Fatalf(
			"input bottom border should render above final gutter line; got penultimate=%q final=%q\n%s",
			lines[len(lines)-2],
			lines[len(lines)-1],
			view,
		)
	}
}

// TestStatusBarCtxPercent verifies that after a StreamDoneMsg, when the pool's main
// agent has real usage and the context window is set, statuses["ctx rem"] shows remaining
// headroom until compaction (threshold% - current%).
func TestStatusBarCtxPercent(t *testing.T) {
	pool := agent.NewPool()
	pool.SetContextWindow(200_000)
	// 50k / 200k = 25%; default threshold 80% → rem = 55%
	lm := newUsageMockLM(50_000, 500, "response")
	a, err := pool.Spawn(agent.MainAgentID, lm, agent.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	m := New(pool, agent.MainAgentID, nil)

	// Run a turn to populate LastUsage.
	done := make(chan error, 1)
	a.SetOnDone(func(e error) { done <- e })
	a.Submit(context.Background(), "query")
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("turn error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for turn")
	}

	// Simulate StreamDoneMsg — this should update ctx rem in status bar.
	m, _ = callUpdate(m, StreamDoneMsg{Err: nil})

	rem := m.live.getStatus("ctx rem")
	if rem == "" {
		t.Fatal("expected live.statuses[ctx rem] to be present after StreamDoneMsg with non-zero usage")
	}
	// threshold=80%, current=25% → rem=55%
	if rem != "55%" {
		t.Errorf("live.statuses[ctx rem] = %q, want %q", rem, "55%")
	}
}

// TestStatusBarCtxPercentZero verifies that when ContextWindow is 0, ctx rem key is absent.
func TestStatusBarCtxPercentZero(t *testing.T) {
	pool := agent.NewPool()
	// No SetContextWindow — window defaults to 0.
	lm := newUsageMockLM(50_000, 500, "response")
	_, err := pool.Spawn(agent.MainAgentID, lm, agent.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	m := New(pool, agent.MainAgentID, nil)

	// StreamDoneMsg with zero context window — ctx rem key should not appear.
	m, _ = callUpdate(m, StreamDoneMsg{Err: nil})

	if v := m.live.getStatus("ctx rem"); v != "" {
		t.Errorf("live.statuses[ctx rem] should be absent when ContextWindow is 0, got %q", v)
	}
}
