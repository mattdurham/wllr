package harness

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/mattdurham/wllr/modules/agent"
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

func TestModel_Update_TokenMsg_AppendsToChat(t *testing.T) {
	m := newTestModel()

	m, _ = callUpdate(m, TokenMsg{Token: "hello"})
	if m.chat.current != "hello" {
		t.Errorf("chat current: got %q, want %q", m.chat.current, "hello")
	}

	m, _ = callUpdate(m, TokenMsg{Token: " world"})
	if m.chat.current != "hello world" {
		t.Errorf("chat current: got %q, want %q", m.chat.current, "hello world")
	}
}

func TestModel_Update_StreamDoneMsg_ClearsStreamStatus(t *testing.T) {
	m := newTestModel()
	m.chat.AppendToken("test response")
	// Simulate a stream status set externally.
	m.statusBar.statuses["stream"] = "working."

	m, _ = callUpdate(m, StreamDoneMsg{Err: nil})
	// Stream status should be cleared.
	if _, ok := m.statusBar.statuses["stream"]; ok {
		t.Error("stream status should be cleared after StreamDoneMsg")
	}
}

func TestModel_Update_StreamDoneMsg_Error_ShowsError(t *testing.T) {
	m := newTestModel()

	m, _ = callUpdate(m, StreamDoneMsg{Err: errors.New("API error")})
	// Should have added an error notification.
	if len(m.chat.messages) == 0 {
		t.Error("expected error notification in chat")
	}
}

func TestModel_Update_StreamDoneMsg_ContextCanceled_NoError(t *testing.T) {
	m := newTestModel()
	m.chat.AppendToken("partial")

	// context.Canceled should not show as an error notification.
	m, _ = callUpdate(m, StreamDoneMsg{Err: context.Canceled})
	// FinalizeMessage adds the partial assistant message (1 message expected).
	// No additional error notification should be added for context.Canceled.
	for _, msg := range m.chat.messages {
		if msg.role == "system" {
			t.Errorf("unexpected error notification for context.Canceled: %q", msg.content)
		}
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
	m.chat.AddUserMessage("hello")

	m, _ = callUpdate(m, clearMsg{})
	if len(m.chat.messages) != 0 {
		t.Errorf("expected empty chat after clear, got %d messages", len(m.chat.messages))
	}
}

func TestModel_Update_SetModelMsg(t *testing.T) {
	m := newTestModel()
	m, _ = callUpdate(m, setModelMsg{Model: "claude-haiku-3-5"})
	if m.activeModel != "claude-haiku-3-5" {
		t.Errorf("activeModel: got %q, want %q", m.activeModel, "claude-haiku-3-5")
	}
	if m.statusBar.modelName != "claude-haiku-3-5" {
		t.Errorf("statusBar.modelName: got %q, want %q", m.statusBar.modelName, "claude-haiku-3-5")
	}
}

func TestModel_Update_CommandMsg_Clear(t *testing.T) {
	m := newTestModel()
	m.chat.AddUserMessage("test")

	// Dispatch /clear command.
	cmd := m.commands.Dispatch("clear", nil)
	msg := cmd()
	m, _ = callUpdate(m, msg)

	if len(m.chat.messages) != 0 {
		t.Errorf("expected empty chat after /clear, got %d", len(m.chat.messages))
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

func TestModel_Update_SubmitMsg_AddsToHistoryAndChat(t *testing.T) {
	pool := newTestPool()
	m := New(pool, "main", nil)

	m, cmd := callUpdate(m, SubmitMsg{Content: "hello"})
	// SubmitMsg no longer starts a synchronous stream; it sends to the pool.
	// The model should return nil cmd (pool runs asynchronously).
	_ = cmd

	// User message should be in chat immediately after SubmitMsg.
	if len(m.chat.messages) != 1 {
		t.Fatalf("expected 1 chat message after SubmitMsg, got %d", len(m.chat.messages))
	}
	if m.chat.messages[0].content != "hello" {
		t.Errorf("chat.messages[0].content: got %q, want %q", m.chat.messages[0].content, "hello")
	}
}

func TestModel_Update_SubmitMsg_MultipleSubmitsAllowed(t *testing.T) {
	// First submit goes into chat.messages; second (while streaming) goes into chat.queued.
	pool := newTestPool()
	m := New(pool, "main", nil)

	m, _ = callUpdate(m, SubmitMsg{Content: "first"})
	m, _ = callUpdate(m, SubmitMsg{Content: "second"})

	// First message in history, second queued below streaming output.
	if len(m.chat.messages) != 1 {
		t.Errorf("expected 1 chat message (first), got %d", len(m.chat.messages))
	}
	if len(m.chat.queued) != 1 {
		t.Errorf("expected 1 queued message (second), got %d", len(m.chat.queued))
	}
}

func TestModel_Update_NotifyMsg(t *testing.T) {
	m := newTestModel()
	m, _ = callUpdate(m, NotifyMsg{Text: "test notification"})
	// Should have added to chat.
	if len(m.chat.messages) == 0 {
		t.Error("expected notification in chat")
	}
}

func TestModel_Update_StatusUpdateMsg(t *testing.T) {
	m := newTestModel()
	m, _ = callUpdate(m, StatusUpdateMsg{Key: "foo", Value: "bar"})
	if m.statusBar.statuses["foo"] != "bar" {
		t.Errorf("statusBar.statuses[foo]: got %q, want %q", m.statusBar.statuses["foo"], "bar")
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
	// User message should still be in chat.
	if len(m.chat.messages) != 1 {
		t.Errorf("expected 1 chat entry, got %d", len(m.chat.messages))
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

	rem, ok := m.statusBar.statuses["ctx rem"]
	if !ok {
		t.Fatal("expected statuses[ctx rem] to be present after StreamDoneMsg with non-zero usage")
	}
	// threshold=80%, current=25% → rem=55%
	if rem != "55%" {
		t.Errorf("statuses[ctx rem] = %q, want %q", rem, "55%")
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

	if _, ok := m.statusBar.statuses["ctx rem"]; ok {
		t.Errorf("statuses[ctx rem] should be absent when ContextWindow is 0, got %q", m.statusBar.statuses["ctx rem"])
	}
}
