package agent_test

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mattdurham/wllr/modules/agent"
)

// ─── helpers ──────────────────────────────────────────────────────────────────

func subWaitDone(t *testing.T, ch chan error, label string) error {
	t.Helper()
	select {
	case err := <-ch:
		return err
	case <-time.After(5 * time.Second):
		t.Fatalf("timeout waiting for %s", label)
		return nil
	}
}

func collectResponse(t *testing.T, a *agent.Agent, prompt string) string {
	t.Helper()
	var mu sync.Mutex
	var sb strings.Builder
	done := make(chan error, 1)
	a.SetOnToken(func(tok string) { mu.Lock(); sb.WriteString(tok); mu.Unlock() })
	a.SetOnDone(func(e error) { done <- e })
	a.Submit(testCtx(t), prompt)
	if err := subWaitDone(t, done, prompt); err != nil {
		t.Fatalf("agent error: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	return sb.String()
}

// ─── ModelName in SpawnOpts ───────────────────────────────────────────────────

func TestSpawnOpts_ModelName_OverridesPoolDefault(t *testing.T) {
	pool := agent.NewPool()
	pool.SetDefaultModelName("claude-sonnet-4-6")

	a, err := pool.Spawn("agent-1", newMockLM(), agent.SpawnOpts{
		ModelName: "claude-haiku-4-5-20251001",
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	// The agent should report the overridden model name.
	if got := a.ModelName(); got != "claude-haiku-4-5-20251001" {
		t.Errorf("ModelName = %q, want %q", got, "claude-haiku-4-5-20251001")
	}
}

func TestSpawnOpts_ModelName_Empty_UsesPoolDefault(t *testing.T) {
	pool := agent.NewPool()
	pool.SetDefaultModelName("claude-sonnet-4-6")

	a, err := pool.Spawn("agent-1", newMockLM(), agent.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	if got := a.ModelName(); got != "claude-sonnet-4-6" {
		t.Errorf("ModelName = %q, want %q", got, "claude-sonnet-4-6")
	}
}

// ─── InheritBasePrompt ────────────────────────────────────────────────────────

func TestSpawnOpts_InheritBasePrompt_DefaultTrue_InheritsPrompt(t *testing.T) {
	pool := agent.NewPool()
	pool.SetBaseSystemPrompt("You are a helpful assistant.")

	// InheritBasePrompt nil (default) → should inherit.
	a, err := pool.Spawn("a", newMockLM(), agent.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	if !strings.Contains(a.SystemPrompt(), "You are a helpful assistant.") {
		t.Errorf("expected inherited base prompt, got %q", a.SystemPrompt())
	}
}

func TestSpawnOpts_InheritBasePrompt_False_DoesNotInherit(t *testing.T) {
	pool := agent.NewPool()
	pool.SetBaseSystemPrompt("You are a helpful assistant.")

	inherit := false
	a, err := pool.Spawn("b", newMockLM(), agent.SpawnOpts{
		SystemPrompt:      "You are a focused code reviewer.",
		InheritBasePrompt: &inherit,
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	sp := a.SystemPrompt()
	if strings.Contains(sp, "You are a helpful assistant.") {
		t.Errorf("should not inherit base prompt, but got %q", sp)
	}
	if !strings.Contains(sp, "You are a focused code reviewer.") {
		t.Errorf("own system prompt missing, got %q", sp)
	}
}

func TestSpawnOpts_InheritBasePrompt_True_Explicit_InheritsPrompt(t *testing.T) {
	pool := agent.NewPool()
	pool.SetBaseSystemPrompt("base context")

	inherit := true
	a, err := pool.Spawn("c", newMockLM(), agent.SpawnOpts{
		InheritBasePrompt: &inherit,
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	if !strings.Contains(a.SystemPrompt(), "base context") {
		t.Errorf("expected base context, got %q", a.SystemPrompt())
	}
}

// ─── Sub-agent own history ────────────────────────────────────────────────────

func TestSubAgent_OwnHistory_IsolatedFromParent(t *testing.T) {
	pool := agent.NewPool()
	lm := &tokenStreamLM{tokens: []string{"response"}}

	parent, err := pool.Spawn("main", lm, agent.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn main: %v", err)
	}
	child, err := pool.Spawn("main/worker", lm, agent.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn child: %v", err)
	}

	// Run parent turn.
	collectResponse(t, parent, "parent message")

	// Child should have no history from parent turn.
	if h := child.History(); len(h) != 0 {
		t.Errorf("child history should be empty, got %d messages", len(h))
	}

	// Run child turn.
	collectResponse(t, child, "child message")

	// Parent's history unchanged (still just its own turn).
	parentH := parent.History()
	for _, m := range parentH {
		if strings.Contains(m.Content, "child message") {
			t.Error("parent history contains child message — histories not isolated")
		}
	}
}

// ─── Scoped agent ID ──────────────────────────────────────────────────────────

func TestScopedAgentID_ColisionPrevented(t *testing.T) {
	pool := agent.NewPool()
	lm := newMockLM()

	// Two different orchestrators create an agent with the same base name.
	// Scoped IDs: "main/researcher" and "planner/researcher" — no collision.
	_, err := pool.Spawn("main/researcher", lm, agent.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn main/researcher: %v", err)
	}
	_, err = pool.Spawn("planner/researcher", lm, agent.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn planner/researcher (should not collide): %v", err)
	}

	// Unscoped duplicate should still fail.
	_, err = pool.Spawn("main/researcher", lm, agent.SpawnOpts{})
	if err == nil {
		t.Error("expected ErrAgentExists for duplicate scoped ID, got nil")
	}
}

// ─── Sub-agent compaction uses own lastSummary ────────────────────────────────

func TestSubAgent_OwnLastSummary_IndependentOfParent(t *testing.T) {
	pool := agent.NewPool()
	lm := newMockLM()

	parent, err := pool.Spawn("main", lm, agent.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn main: %v", err)
	}
	child, err := pool.Spawn("main/worker", lm, agent.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn child: %v", err)
	}

	// Set a last summary on parent directly.
	parent.SetLastSummary("parent summary content")

	// Child should have no last summary.
	if s := child.LastSummary(); s != "" {
		t.Errorf("child lastSummary should be empty, got %q", s)
	}
}
