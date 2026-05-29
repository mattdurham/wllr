package agent_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mattdurham/wllr/modules/agent"
	"github.com/mattdurham/wllr/modules/sdk"
)

// ─── Helpers ─────────────────────────────────────────────────────────────────

// spawnWithCallbacks spawns an agent and wires token/done channels for easy
// inspection in tests.
func spawnWithCallbacks(t *testing.T, pool *agent.AgentPool, id, response string) (
	a *agent.Agent,
	tokens chan string,
	done chan error,
) {
	t.Helper()
	lm := &tokenStreamLM{tokens: []string{response}}
	var err error
	a, err = pool.Spawn(id, lm, agent.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn %q: %v", id, err)
	}
	tokens = make(chan string, 64)
	done = make(chan error, 1)
	a.SetOnToken(func(tok string) { tokens <- tok })
	a.SetOnDone(func(e error) { done <- e })
	return
}

// collectTokens drains a channel for up to d and returns the concatenated text.
func collectTokens(ch chan string, d time.Duration) string {
	var sb strings.Builder
	deadline := time.After(d)
	for {
		select {
		case tok, ok := <-ch:
			if !ok {
				return sb.String()
			}
			sb.WriteString(tok)
		case <-deadline:
			return sb.String()
		}
	}
}

// waitDone waits for a done channel with a timeout, failing the test on timeout.
func waitDone(t *testing.T, done chan error, d time.Duration, label string) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(d):
		t.Fatalf("timeout waiting for %s to complete", label)
		return nil
	}
}

// ─── Lifecycle ────────────────────────────────────────────────────────────────

// TestTeam_FullLifecycle creates a team, adds two agents, sends a message to
// each, verifies they respond, then tears the team down.
func TestTeam_FullLifecycle(t *testing.T) {
	pool := agent.NewPool()
	ctx := context.Background()

	// Spawn two agents with distinct responses.
	a1, tok1, done1 := spawnWithCallbacks(t, pool, "agent-1", "I am agent-1")
	a2, tok2, done2 := spawnWithCallbacks(t, pool, "agent-2", "I am agent-2")
	_ = a1
	_ = a2

	// Create team and add both agents.
	team, err := pool.CreateTeam("test-team")
	if err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}
	if err := team.AddMember("agent-1"); err != nil {
		t.Fatalf("AddMember agent-1: %v", err)
	}
	if err := team.AddMember("agent-2"); err != nil {
		t.Fatalf("AddMember agent-2: %v", err)
	}

	// Team should contain both members.
	members := team.Members()
	if len(members) != 2 {
		t.Errorf("expected 2 members, got %d", len(members))
	}

	// Send a message to each agent.
	if err := pool.Send("agent-1", "hello agent-1"); err != nil {
		t.Fatalf("Send to agent-1: %v", err)
	}
	if err := pool.Send("agent-2", "hello agent-2"); err != nil {
		t.Fatalf("Send to agent-2: %v", err)
	}

	// Both agents should complete their turns.
	if err := waitDone(t, done1, 5*time.Second, "agent-1"); err != nil {
		t.Errorf("agent-1 done with error: %v", err)
	}
	if err := waitDone(t, done2, 5*time.Second, "agent-2"); err != nil {
		t.Errorf("agent-2 done with error: %v", err)
	}

	// Verify each agent produced its expected response.
	got1 := collectTokens(tok1, 100*time.Millisecond)
	got2 := collectTokens(tok2, 100*time.Millisecond)
	if !strings.Contains(got1, "agent-1") {
		t.Errorf("agent-1 response: got %q, want to contain %q", got1, "agent-1")
	}
	if !strings.Contains(got2, "agent-2") {
		t.Errorf("agent-2 response: got %q, want to contain %q", got2, "agent-2")
	}

	// Tear down via CloseTeam — both agents should be removed.
	if err := pool.CloseTeam(ctx, "test-team"); err != nil {
		t.Fatalf("CloseTeam: %v", err)
	}
	if pool.Get("agent-1") != nil {
		t.Error("agent-1 still in pool after CloseTeam")
	}
	if pool.Get("agent-2") != nil {
		t.Error("agent-2 still in pool after CloseTeam")
	}
	if pool.GetTeam("test-team") != nil {
		t.Error("team still in pool after CloseTeam")
	}
}

// ─── Cross-agent messaging ────────────────────────────────────────────────────

// TestTeam_AgentsSendToEachOther tests one agent delivering a message into
// another agent's inbox, simulating the pattern used by the agents extension.
func TestTeam_AgentsSendToEachOther(t *testing.T) {
	pool := agent.NewPool()
	ctx := context.Background()

	lm := &tokenStreamLM{tokens: []string{"ack"}}
	_, err := pool.Spawn("sender", lm, agent.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn sender: %v", err)
	}

	receiver, err := pool.Spawn("receiver", lm, agent.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn receiver: %v", err)
	}

	done := make(chan error, 1)
	var received []sdk.Message
	var mu sync.Mutex
	receiver.SetOnDone(func(e error) { done <- e })

	// Deliver a message into the receiver's inbox (simulates cross-agent comms).
	msg := sdk.Message{Role: sdk.RoleUser, Content: "message from sender"}
	receiver.AppendInbox(msg)

	// Trigger a turn — the receiver drains its inbox as part of Submit.
	if err := pool.Send("receiver", "start"); err != nil {
		t.Fatalf("Send: %v", err)
	}

	if err := waitDone(t, done, 5*time.Second, "receiver"); err != nil {
		t.Errorf("receiver done with error: %v", err)
	}

	// The inbox message should have been drained into the turn history.
	mu.Lock()
	_ = received
	mu.Unlock()

	// Verify inbox is empty after the turn (DrainInbox was called).
	drained := receiver.DrainInbox()
	if len(drained) != 0 {
		t.Errorf("inbox should be empty after turn, got %d messages", len(drained))
	}

	_ = ctx
}

// ─── Parallel agents ─────────────────────────────────────────────────────────

// TestTeam_ParallelAgents verifies that multiple agents in a team can run
// concurrently without interfering with each other's history or callbacks.
func TestTeam_ParallelAgents(t *testing.T) {
	pool := agent.NewPool()
	ctx := context.Background()

	const n = 4
	dones := make([]chan error, n)
	responses := make([]string, n)
	var mu sync.Mutex

	for i := range n {
		id := strings.Repeat("x", i+1) // "x", "xx", "xxx", "xxxx"
		lm := &tokenStreamLM{tokens: []string{"response-" + id}}
		a, err := pool.Spawn(id, lm, agent.SpawnOpts{})
		if err != nil {
			t.Fatalf("Spawn %q: %v", id, err)
		}
		dones[i] = make(chan error, 1)
		idx := i
		a.SetOnToken(func(tok string) {
			mu.Lock()
			responses[idx] += tok
			mu.Unlock()
		})
		a.SetOnDone(func(e error) { dones[idx] <- e })
	}

	team, err := pool.CreateTeam("parallel")
	if err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}
	for i := range n {
		id := strings.Repeat("x", i+1)
		if err := team.AddMember(id); err != nil {
			t.Fatalf("AddMember %q: %v", id, err)
		}
	}

	// Submit to all agents simultaneously.
	for i := range n {
		id := strings.Repeat("x", i+1)
		if err := pool.Send(id, "go"); err != nil {
			t.Fatalf("Send to %q: %v", id, err)
		}
	}

	// Wait for all to complete.
	for i, done := range dones {
		id := strings.Repeat("x", i+1)
		if err := waitDone(t, done, 5*time.Second, id); err != nil {
			t.Errorf("%q done with error: %v", id, err)
		}
	}

	// Verify each agent got its own response without cross-contamination.
	mu.Lock()
	defer mu.Unlock()
	for i := range n {
		id := strings.Repeat("x", i+1)
		want := "response-" + id
		if !strings.Contains(responses[i], want) {
			t.Errorf("agent %q response %q does not contain %q", id, responses[i], want)
		}
	}

	if err := pool.CloseTeam(ctx, "parallel"); err != nil {
		t.Fatalf("CloseTeam: %v", err)
	}
}

// ─── Team isolation ───────────────────────────────────────────────────────────

// TestTeam_MembersIsolatedFromNonMembers verifies that closing a team only
// removes its members and leaves other agents untouched.
func TestTeam_MembersIsolatedFromNonMembers(t *testing.T) {
	pool := agent.NewPool()
	ctx := context.Background()
	lm := newMockLM()

	// Spawn three agents; only two join the team.
	for _, id := range []string{"in-1", "in-2", "out-1"} {
		if _, err := pool.Spawn(id, lm, agent.SpawnOpts{}); err != nil {
			t.Fatalf("Spawn %q: %v", id, err)
		}
	}

	team, err := pool.CreateTeam("isolated")
	if err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}
	if err := team.AddMember("in-1"); err != nil {
		t.Fatalf("AddMember: %v", err)
	}
	if err := team.AddMember("in-2"); err != nil {
		t.Fatalf("AddMember: %v", err)
	}

	if err := pool.CloseTeam(ctx, "isolated"); err != nil {
		t.Fatalf("CloseTeam: %v", err)
	}

	// Team members removed.
	if pool.Get("in-1") != nil {
		t.Error("in-1 still in pool after CloseTeam")
	}
	if pool.Get("in-2") != nil {
		t.Error("in-2 still in pool after CloseTeam")
	}

	// Non-member untouched.
	if pool.Get("out-1") == nil {
		t.Error("out-1 was removed but should not have been")
	}
}
