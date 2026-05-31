package harness

import (
	"context"
	"testing"
	"time"

	"github.com/mattdurham/wllr/modules/agent"
	"github.com/mattdurham/wllr/modules/sdk"
	"github.com/mattdurham/wllr/modules/testutil"
)

// newBridgeTestPool returns a pool with a spawned main agent.
func newBridgeTestPool(t *testing.T) *agent.AgentPool {
	t.Helper()
	prov := testutil.NewFakeProvider()
	pool := agent.NewPool()
	pool.SetProvider(prov)
	pool.SetDefaultModelName("fake-model")
	lm, err := pool.LanguageModelForModel(context.Background(), "fake-model")
	if err != nil {
		t.Fatalf("LanguageModelForModel: %v", err)
	}
	if _, err := pool.Spawn(agent.MainAgentID, lm, agent.SpawnOpts{}); err != nil {
		t.Fatalf("spawn main: %v", err)
	}
	return pool
}

// TestWaitForAll_NilPool returns an error immediately.
func TestWaitForAll_NilPool(t *testing.T) {
	b := &harnessAgentBridge{pool: nil}
	_, err := b.WaitForAll("main", []string{"main/worker"}, 1000)
	if err == nil {
		t.Fatal("expected error for nil pool, got nil")
	}
}

// TestWaitForAll_Complete fires a completion notification into the caller's inbox
// and verifies WaitForAll returns status="complete".
func TestWaitForAll_Complete(t *testing.T) {
	pool := newBridgeTestPool(t)

	// Inject a completion notification for "main/worker" directly into main's inbox.
	notification := "[from agent 'worker' (main/worker)]: turn complete — call get_agent_status"
	pool.SendMessage(agent.MainAgentID, sdk.Message{Role: sdk.RoleUser, Content: notification}) //nolint:errcheck

	b := &harnessAgentBridge{pool: pool}
	result, err := b.WaitForAll(agent.MainAgentID, []string{"main/worker"}, 5000)
	if err != nil {
		t.Fatalf("WaitForAll: %v", err)
	}
	if result.Status != "complete" {
		t.Errorf("expected status=complete, got %q", result.Status)
	}
	if _, ok := result.Results["main/worker"]; !ok {
		t.Error("expected results to contain main/worker")
	}
}

// TestWaitForAll_Interrupted returns status="interrupted" when a non-notification
// message arrives in the caller's inbox, and puts that message back.
func TestWaitForAll_Interrupted(t *testing.T) {
	pool := newBridgeTestPool(t)

	// Inject a user message (not a completion notification) into main's inbox.
	userMsg := sdk.Message{Role: sdk.RoleUser, Content: "hey, what's the status?"}
	pool.SendMessage(agent.MainAgentID, userMsg) //nolint:errcheck

	b := &harnessAgentBridge{pool: pool}
	result, err := b.WaitForAll(agent.MainAgentID, []string{"main/worker"}, 5000)
	if err != nil {
		t.Fatalf("WaitForAll: %v", err)
	}
	if result.Status != "interrupted" {
		t.Errorf("expected status=interrupted, got %q", result.Status)
	}
	if len(result.Pending) == 0 {
		t.Error("expected pending list to be non-empty")
	}

	// The user message must be back in main's inbox for the next turn to process.
	msgs := pool.Get(agent.MainAgentID).DrainInbox()
	found := false
	for _, m := range msgs {
		if m.Content == userMsg.Content {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected user message to be put back in caller's inbox")
	}
}

// TestWaitForAll_Timeout returns status="timeout" when no agents complete within the window.
func TestWaitForAll_Timeout(t *testing.T) {
	pool := newBridgeTestPool(t)

	b := &harnessAgentBridge{pool: pool}
	start := time.Now()
	result, err := b.WaitForAll(agent.MainAgentID, []string{"main/never"}, 300) // 300ms timeout
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("WaitForAll: %v", err)
	}
	if result.Status != "timeout" {
		t.Errorf("expected status=timeout, got %q", result.Status)
	}
	if len(result.Pending) == 0 {
		t.Error("expected pending to be non-empty on timeout")
	}
	if elapsed < 250*time.Millisecond {
		t.Errorf("expected to wait ~300ms, but returned after %v", elapsed)
	}
}

// TestWaitForAll_MultipleAgents waits for two agents simultaneously.
func TestWaitForAll_MultipleAgents(t *testing.T) {
	pool := newBridgeTestPool(t)

	// Inject completion notifications for both agents.
	for _, id := range []string{"main/worker-1", "main/worker-2"} {
		content := "[from agent 'worker' (" + id + ")]: turn complete — call get_agent_status"
		pool.SendMessage(agent.MainAgentID, sdk.Message{Role: sdk.RoleUser, Content: content}) //nolint:errcheck
	}

	b := &harnessAgentBridge{pool: pool}
	result, err := b.WaitForAll(agent.MainAgentID, []string{"main/worker-1", "main/worker-2"}, 5000)
	if err != nil {
		t.Fatalf("WaitForAll: %v", err)
	}
	if result.Status != "complete" {
		t.Errorf("expected complete, got %q (pending: %v)", result.Status, result.Pending)
	}
	if len(result.Results) != 2 {
		t.Errorf("expected 2 results, got %d", len(result.Results))
	}
}
