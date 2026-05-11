package agent_test

import (
	"context"
	"testing"

	"charm.land/fantasy"
	"github.com/mattdurham/wllr/agent"
	"github.com/mattdurham/wllr/sdk"
)

// mockLM is a no-op fantasy.LanguageModel for tests that don't need real streaming.
type mockLM struct {
	modelID  string
	provider string
}

var _ fantasy.LanguageModel = (*mockLM)(nil)

func (m *mockLM) Model() string    { return m.modelID }
func (m *mockLM) Provider() string { return m.provider }
func (m *mockLM) Generate(_ context.Context, _ fantasy.Call) (*fantasy.Response, error) {
	return &fantasy.Response{}, nil
}

func (m *mockLM) Stream(_ context.Context, _ fantasy.Call) (fantasy.StreamResponse, error) {
	return func(yield func(fantasy.StreamPart) bool) {
		yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeFinish, FinishReason: fantasy.FinishReasonStop})
	}, nil
}

func (m *mockLM) GenerateObject(_ context.Context, _ fantasy.ObjectCall) (*fantasy.ObjectResponse, error) {
	return nil, nil
}

func (m *mockLM) StreamObject(_ context.Context, _ fantasy.ObjectCall) (fantasy.ObjectStreamResponse, error) {
	return nil, nil
}

func newMockLM() fantasy.LanguageModel {
	return &mockLM{modelID: "mock-model", provider: "mock"}
}

func TestAgentPool_SpawnAndGet(t *testing.T) {
	pool := agent.NewPool()
	lm := newMockLM()

	a, err := pool.Spawn("a", lm, agent.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if a == nil {
		t.Fatal("Spawn returned nil agent")
	}
	if a.ID() != "a" {
		t.Errorf("Agent ID: got %q, want %q", a.ID(), "a")
	}

	got := pool.Get("a")
	if got == nil {
		t.Fatal("Get returned nil for existing agent")
	}
	if got.ID() != "a" {
		t.Errorf("Get agent ID: got %q, want %q", got.ID(), "a")
	}
}

func TestAgentPool_Get_Unknown(t *testing.T) {
	pool := agent.NewPool()
	got := pool.Get("nonexistent")
	if got != nil {
		t.Errorf("Get unknown agent: expected nil, got %v", got)
	}
}

func TestAgentPool_Spawn_DuplicateID_ReturnsError(t *testing.T) {
	pool := agent.NewPool()
	lm := newMockLM()

	if _, err := pool.Spawn("a", lm, agent.SpawnOpts{}); err != nil {
		t.Fatalf("first Spawn: %v", err)
	}

	_, err := pool.Spawn("a", lm, agent.SpawnOpts{})
	if err == nil {
		t.Fatal("expected error on duplicate Spawn, got nil")
	}
	if err != agent.ErrAgentExists {
		t.Errorf("expected ErrAgentExists, got %v", err)
	}
}

func TestAgentPool_Close_RemovesAgent(t *testing.T) {
	pool := agent.NewPool()
	lm := newMockLM()

	if _, err := pool.Spawn("a", lm, agent.SpawnOpts{}); err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	if err := pool.Close("a"); err != nil {
		t.Fatalf("Close: %v", err)
	}

	got := pool.Get("a")
	if got != nil {
		t.Errorf("Get after Close: expected nil, got %v", got)
	}
}

func TestAgentPool_Close_UnknownID_ReturnsError(t *testing.T) {
	pool := agent.NewPool()
	err := pool.Close("nonexistent")
	if err == nil {
		t.Fatal("expected error for closing unknown agent, got nil")
	}
	if err != agent.ErrAgentNotFound {
		t.Errorf("expected ErrAgentNotFound, got %v", err)
	}
}

func TestAgentPool_TokenCount(t *testing.T) {
	pool := agent.NewPool()
	if pool.TokenCount() != 0 {
		t.Errorf("initial TokenCount: got %d, want 0", pool.TokenCount())
	}
	// Increment via the internal method.
	pool.AddTokens(5)
	if pool.TokenCount() != 5 {
		t.Errorf("TokenCount after Add(5): got %d, want 5", pool.TokenCount())
	}
	pool.AddTokens(3)
	if pool.TokenCount() != 8 {
		t.Errorf("TokenCount after Add(3): got %d, want 8", pool.TokenCount())
	}
}

func TestAgentPool_SendMessage_ToExisting(t *testing.T) {
	pool := agent.NewPool()
	lm := newMockLM()

	if _, err := pool.Spawn("a", lm, agent.SpawnOpts{}); err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	msg := sdk.Message{Role: sdk.RoleUser, Content: "hello"}
	if err := pool.SendMessage("a", msg); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}

	a := pool.Get("a")
	inbox := a.DrainInbox()
	if len(inbox) != 1 {
		t.Fatalf("DrainInbox: expected 1 message, got %d", len(inbox))
	}
	if inbox[0].Content != "hello" {
		t.Errorf("DrainInbox message: got %q, want %q", inbox[0].Content, "hello")
	}
}

func TestAgentPool_SendMessage_UnknownID_ReturnsError(t *testing.T) {
	pool := agent.NewPool()
	err := pool.SendMessage("nonexistent", sdk.Message{Role: sdk.RoleUser, Content: "hi"})
	if err == nil {
		t.Fatal("expected error for SendMessage to unknown agent, got nil")
	}
	if err != agent.ErrAgentNotFound {
		t.Errorf("expected ErrAgentNotFound, got %v", err)
	}
}

func TestAgentPool_ListAgents(t *testing.T) {
	pool := agent.NewPool()
	lm := newMockLM()

	ids := pool.ListAgents()
	if len(ids) != 0 {
		t.Errorf("ListAgents on empty pool: got %v, want []", ids)
	}

	if _, err := pool.Spawn("b", lm, agent.SpawnOpts{}); err != nil {
		t.Fatalf("Spawn b: %v", err)
	}
	if _, err := pool.Spawn("a", lm, agent.SpawnOpts{}); err != nil {
		t.Fatalf("Spawn a: %v", err)
	}

	ids = pool.ListAgents()
	if len(ids) != 2 {
		t.Errorf("ListAgents: expected 2 agents, got %d: %v", len(ids), ids)
	}
}

func TestAgent_Inbox_AppendAndDrain(t *testing.T) {
	pool := agent.NewPool()
	lm := newMockLM()

	a, err := pool.Spawn("a", lm, agent.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	msg1 := sdk.Message{Role: sdk.RoleUser, Content: "first"}
	msg2 := sdk.Message{Role: sdk.RoleUser, Content: "second"}

	a.AppendInbox(msg1)
	a.AppendInbox(msg2)

	drained := a.DrainInbox()
	if len(drained) != 2 {
		t.Fatalf("DrainInbox: expected 2 messages, got %d", len(drained))
	}
	if drained[0].Content != "first" {
		t.Errorf("drained[0]: got %q, want %q", drained[0].Content, "first")
	}
	if drained[1].Content != "second" {
		t.Errorf("drained[1]: got %q, want %q", drained[1].Content, "second")
	}

	// After drain, inbox should be empty.
	again := a.DrainInbox()
	if len(again) != 0 {
		t.Errorf("second DrainInbox: expected 0 messages, got %d", len(again))
	}
}

func TestTeam_Create_And_List(t *testing.T) {
	pool := agent.NewPool()

	team, err := pool.CreateTeam("t1")
	if err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}
	if team == nil {
		t.Fatal("CreateTeam returned nil")
	}
	if team.ID() != "t1" {
		t.Errorf("Team ID: got %q, want %q", team.ID(), "t1")
	}

	got := pool.GetTeam("t1")
	if got == nil {
		t.Fatal("GetTeam returned nil for existing team")
	}
	if got.ID() != "t1" {
		t.Errorf("GetTeam ID: got %q, want %q", got.ID(), "t1")
	}
}

func TestTeam_CreateDuplicate_ReturnsError(t *testing.T) {
	pool := agent.NewPool()

	if _, err := pool.CreateTeam("t1"); err != nil {
		t.Fatalf("first CreateTeam: %v", err)
	}

	_, err := pool.CreateTeam("t1")
	if err == nil {
		t.Fatal("expected error on duplicate CreateTeam, got nil")
	}
	if err != agent.ErrTeamExists {
		t.Errorf("expected ErrTeamExists, got %v", err)
	}
}

func TestTeam_GetUnknown_ReturnsNil(t *testing.T) {
	pool := agent.NewPool()
	got := pool.GetTeam("nonexistent")
	if got != nil {
		t.Errorf("GetTeam unknown: expected nil, got %v", got)
	}
}

func TestTeam_AddMember_And_RemoveMember(t *testing.T) {
	pool := agent.NewPool()
	lm := newMockLM()

	if _, err := pool.Spawn("agent1", lm, agent.SpawnOpts{}); err != nil {
		t.Fatalf("Spawn agent1: %v", err)
	}
	if _, err := pool.Spawn("agent2", lm, agent.SpawnOpts{}); err != nil {
		t.Fatalf("Spawn agent2: %v", err)
	}

	team, err := pool.CreateTeam("t1")
	if err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}

	if err := team.AddMember("agent1"); err != nil {
		t.Fatalf("AddMember agent1: %v", err)
	}
	if err := team.AddMember("agent2"); err != nil {
		t.Fatalf("AddMember agent2: %v", err)
	}

	members := team.Members()
	if len(members) != 2 {
		t.Errorf("Members: expected 2, got %d: %v", len(members), members)
	}

	team.RemoveMember("agent1")
	members = team.Members()
	if len(members) != 1 {
		t.Errorf("Members after RemoveMember: expected 1, got %d: %v", len(members), members)
	}
	if members[0] != "agent2" {
		t.Errorf("remaining member: got %q, want %q", members[0], "agent2")
	}
}

func TestTeam_AddMember_UnknownAgent_ReturnsError(t *testing.T) {
	pool := agent.NewPool()

	team, err := pool.CreateTeam("t1")
	if err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}

	err = team.AddMember("nonexistent")
	if err == nil {
		t.Fatal("expected error for adding unknown agent to team, got nil")
	}
	if err != agent.ErrAgentNotFound {
		t.Errorf("expected ErrAgentNotFound, got %v", err)
	}
}

func TestTeam_Close_ClosesAllMembers(t *testing.T) {
	pool := agent.NewPool()
	lm := newMockLM()

	if _, err := pool.Spawn("a1", lm, agent.SpawnOpts{}); err != nil {
		t.Fatalf("Spawn a1: %v", err)
	}
	if _, err := pool.Spawn("a2", lm, agent.SpawnOpts{}); err != nil {
		t.Fatalf("Spawn a2: %v", err)
	}

	team, err := pool.CreateTeam("t1")
	if err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}

	if err := team.AddMember("a1"); err != nil {
		t.Fatalf("AddMember a1: %v", err)
	}
	if err := team.AddMember("a2"); err != nil {
		t.Fatalf("AddMember a2: %v", err)
	}

	ctx := context.Background()
	if err := team.Close(ctx); err != nil {
		t.Fatalf("team.Close: %v", err)
	}

	// After Close, agents should be removed from the pool.
	if pool.Get("a1") != nil {
		t.Error("a1 still in pool after team.Close")
	}
	if pool.Get("a2") != nil {
		t.Error("a2 still in pool after team.Close")
	}
}

func TestAgentPool_CloseTeam(t *testing.T) {
	pool := agent.NewPool()
	lm := newMockLM()

	if _, err := pool.Spawn("a1", lm, agent.SpawnOpts{}); err != nil {
		t.Fatalf("Spawn a1: %v", err)
	}

	team, err := pool.CreateTeam("myteam")
	if err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}
	if err := team.AddMember("a1"); err != nil {
		t.Fatalf("AddMember a1: %v", err)
	}

	ctx := context.Background()
	if err := pool.CloseTeam(ctx, "myteam"); err != nil {
		t.Fatalf("CloseTeam: %v", err)
	}

	// Team should be gone.
	if pool.GetTeam("myteam") != nil {
		t.Error("team still in pool after CloseTeam")
	}
}

func TestAgentPool_CloseTeam_UnknownID_ReturnsError(t *testing.T) {
	pool := agent.NewPool()
	err := pool.CloseTeam(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for CloseTeam with unknown ID, got nil")
	}
	if err != agent.ErrTeamNotFound {
		t.Errorf("expected ErrTeamNotFound, got %v", err)
	}
}

func TestAgentPool_ListTeams(t *testing.T) {
	pool := agent.NewPool()

	ids := pool.ListTeams()
	if len(ids) != 0 {
		t.Errorf("ListTeams on empty pool: got %v, want []", ids)
	}

	if _, err := pool.CreateTeam("team-a"); err != nil {
		t.Fatalf("CreateTeam team-a: %v", err)
	}
	if _, err := pool.CreateTeam("team-b"); err != nil {
		t.Fatalf("CreateTeam team-b: %v", err)
	}

	ids = pool.ListTeams()
	if len(ids) != 2 {
		t.Errorf("ListTeams: expected 2 teams, got %d: %v", len(ids), ids)
	}
}

func TestAgentPool_GetTeamMembers(t *testing.T) {
	pool := agent.NewPool()
	lm := newMockLM()

	// Spawn two agents and create a team with both.
	if _, err := pool.Spawn("m1", lm, agent.SpawnOpts{}); err != nil {
		t.Fatalf("Spawn m1: %v", err)
	}
	if _, err := pool.Spawn("m2", lm, agent.SpawnOpts{}); err != nil {
		t.Fatalf("Spawn m2: %v", err)
	}

	team, err := pool.CreateTeam("myteam")
	if err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}
	if err := team.AddMember("m1"); err != nil {
		t.Fatalf("AddMember m1: %v", err)
	}
	if err := team.AddMember("m2"); err != nil {
		t.Fatalf("AddMember m2: %v", err)
	}

	members, err := pool.GetTeamMembers("myteam")
	if err != nil {
		t.Fatalf("GetTeamMembers: %v", err)
	}
	if len(members) != 2 {
		t.Errorf("GetTeamMembers: expected 2, got %d: %v", len(members), members)
	}
}

func TestAgentPool_GetTeamMembers_UnknownTeam_ReturnsError(t *testing.T) {
	pool := agent.NewPool()
	_, err := pool.GetTeamMembers("nonexistent")
	if err == nil {
		t.Fatal("expected error for GetTeamMembers with unknown team, got nil")
	}
	if err != agent.ErrTeamNotFound {
		t.Errorf("expected ErrTeamNotFound, got %v", err)
	}
}

func TestAgentPool_ListTeams_AfterCloseTeam(t *testing.T) {
	pool := agent.NewPool()

	if _, err := pool.CreateTeam("gone"); err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}
	if _, err := pool.CreateTeam("stay"); err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}

	if err := pool.CloseTeam(context.Background(), "gone"); err != nil {
		t.Fatalf("CloseTeam: %v", err)
	}

	ids := pool.ListTeams()
	if len(ids) != 1 {
		t.Errorf("ListTeams after CloseTeam: expected 1, got %d: %v", len(ids), ids)
	}
	if ids[0] != "stay" {
		t.Errorf("ListTeams: got %q, want %q", ids[0], "stay")
	}
}
