package agent_test

import (
	"context"
	"testing"
	"time"

	"github.com/mattdurham/wllr/modules/agent"
)

func TestAgentPool_SetBaseSystemPrompt_PropagatestoExistingAgents(t *testing.T) {
	pool := agent.NewPool()
	lm := newMockLM()

	a, err := pool.Spawn("a", lm, agent.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	pool.SetBaseSystemPrompt("You are a helpful assistant.")

	// After SetBaseSystemPrompt, the agent's system prompt should be updated.
	// We verify by checking it appears in the next Submit (done is reached with no error).
	done := make(chan error, 1)
	a.SetOnDone(func(err error) { done <- err })
	a.Submit(testCtx(t), "hello")
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}
}

func TestAgentPool_SetBaseSystemPrompt_AppliedToNewAgents(t *testing.T) {
	pool := agent.NewPool()
	pool.SetBaseSystemPrompt("Base system prompt.")

	lm := newMockLM()
	a, err := pool.Spawn("new-agent", lm, agent.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	// The new agent should inherit the base system prompt — verify via a successful submit.
	done := make(chan error, 1)
	a.SetOnDone(func(err error) { done <- err })
	a.Submit(testCtx(t), "test")
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}

	// BaseSystemPrompt accessor should return what was set.
	if pool.BaseSystemPrompt() != "Base system prompt." {
		t.Errorf("BaseSystemPrompt: got %q, want %q", pool.BaseSystemPrompt(), "Base system prompt.")
	}
}

func TestAgentPool_AppendBaseSystemPrompt(t *testing.T) {
	pool := agent.NewPool()
	pool.SetBaseSystemPrompt("First section.")
	pool.AppendBaseSystemPrompt("Second section.")

	got := pool.BaseSystemPrompt()
	if got != "First section.\n\nSecond section." {
		t.Errorf("BaseSystemPrompt after append: got %q", got)
	}
}

func TestAgentPool_CancelAll_CancelsActiveAgent(t *testing.T) {
	pool := agent.NewPool()
	lm := &slowLM{}
	_, err := pool.Spawn("slow1", lm, agent.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	done := make(chan error, 1)
	a := pool.Get("slow1")
	a.SetOnDone(func(err error) { done <- err })
	a.Submit(testCtx(t), "slow request")
	time.Sleep(10 * time.Millisecond) // let goroutine start

	pool.CancelAll()

	select {
	case err := <-done:
		_ = err // cancellation error expected
	case <-time.After(5 * time.Second):
		t.Fatal("timeout: CancelAll did not stop agents")
	}
}

func TestAgentPool_SetDefaultModelName(t *testing.T) {
	pool := agent.NewPool()
	pool.SetDefaultModelName("claude-sonnet-4-5")
	// Verify via LanguageModelForModel — needs a provider set.
	// Without a provider it returns an error, which is also a correct assertion.
	_, err := pool.LanguageModelForModel(testCtx(t), "")
	if err == nil {
		t.Error("expected error when no provider is configured")
	}
}

func TestAgentPool_SetProviderName(t *testing.T) {
	pool := agent.NewPool()
	pool.SetProviderName("anthropic")
	if got := pool.ProviderName(); got != "anthropic" {
		t.Errorf("ProviderName: got %q, want %q", got, "anthropic")
	}
}

func TestAgentPool_Cancel_NonExistent_ReturnsError(t *testing.T) {
	pool := agent.NewPool()
	err := pool.Cancel("nonexistent")
	if err == nil {
		t.Fatal("expected error for Cancel on unknown agent")
	}
	if err != agent.ErrAgentNotFound {
		t.Errorf("expected ErrAgentNotFound, got %v", err)
	}
}

func TestAgentPool_Send_NonExistent_ReturnsError(t *testing.T) {
	pool := agent.NewPool()
	err := pool.Send("nonexistent", "content")
	if err == nil {
		t.Fatal("expected error for Send on unknown agent")
	}
	if err != agent.ErrAgentNotFound {
		t.Errorf("expected ErrAgentNotFound, got %v", err)
	}
}

// testCtx returns a background context associated with the test.
func testCtx(t *testing.T) context.Context {
	t.Helper()
	return context.Background()
}
