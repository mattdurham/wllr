package agent_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"charm.land/fantasy"
	anthropicprovider "charm.land/fantasy/providers/anthropic"
	"github.com/mattdurham/wllr/modules/agent"
)

// providerOptsSpyLM captures the ProviderOptions passed to Stream on each turn.
type providerOptsSpyLM struct {
	mu     sync.Mutex
	tokens []string
	last   fantasy.ProviderOptions
}

func (s *providerOptsSpyLM) Model() string    { return "opts-spy" }
func (s *providerOptsSpyLM) Provider() string { return "test" }

func (s *providerOptsSpyLM) Stream(_ context.Context, call fantasy.Call) (fantasy.StreamResponse, error) {
	s.mu.Lock()
	s.last = call.ProviderOptions
	toks := s.tokens
	s.mu.Unlock()
	return func(yield func(fantasy.StreamPart) bool) {
		for _, tok := range toks {
			if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextDelta, Delta: tok}) {
				return
			}
		}
		yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeFinish, FinishReason: fantasy.FinishReasonStop})
	}, nil
}

func (s *providerOptsSpyLM) Generate(_ context.Context, _ fantasy.Call) (*fantasy.Response, error) {
	return &fantasy.Response{}, nil
}

func (s *providerOptsSpyLM) GenerateObject(_ context.Context, _ fantasy.ObjectCall) (*fantasy.ObjectResponse, error) {
	return nil, nil
}

func (s *providerOptsSpyLM) StreamObject(_ context.Context, _ fantasy.ObjectCall) (fantasy.ObjectStreamResponse, error) {
	return nil, nil
}

func (s *providerOptsSpyLM) lastOptions() fantasy.ProviderOptions {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.last
}

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

// TestSetModel_SwapsModelForNextTurn verifies SetModel updates the reported
// model name and the LM used by the next turn (the /model picker path).
func TestSetModel_SwapsModelForNextTurn(t *testing.T) {
	pool := agent.NewPool()
	a, err := pool.Spawn("main", &tokenStreamLM{tokens: []string{"from-original"}}, agent.SpawnOpts{ModelName: "claude-sonnet-4-6"})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	a.SetModel(&tokenStreamLM{tokens: []string{"from-swapped"}}, "claude-opus-4-8")
	if got := a.ModelName(); got != "claude-opus-4-8" {
		t.Errorf("ModelName after SetModel = %q, want claude-opus-4-8", got)
	}

	// The next turn must stream from the swapped LM.
	got := collectResponse(t, a, "go")
	if !strings.Contains(got, "from-swapped") {
		t.Errorf("turn used old LM: got %q, want text from swapped LM", got)
	}
}

// TestSetProviderOptions_AppliedToNextTurn verifies SetProviderOptions is
// threaded into the fantasy.Call for the next turn (the /thinking picker path),
// and that clearing it (nil) removes the options.
func TestSetProviderOptions_AppliedToNextTurn(t *testing.T) {
	pool := agent.NewPool()
	spy := &providerOptsSpyLM{tokens: []string{"ok"}}
	a, err := pool.Spawn("main", spy, agent.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	// No options set yet: first turn carries none. (fantasy normalizes nil to an
	// empty map, so assert emptiness rather than nil.)
	_ = collectResponse(t, a, "one")
	if got := spy.lastOptions(); len(got) != 0 {
		t.Errorf("turn 1 ProviderOptions = %v, want empty", got)
	}

	// Set options: next turn must carry them.
	a.SetProviderOptions(fantasy.ProviderOptions{
		anthropicprovider.Name: &anthropicprovider.ProviderOptions{
			Thinking: &anthropicprovider.ThinkingProviderOption{BudgetTokens: 4096},
		},
	})
	_ = collectResponse(t, a, "two")
	if got := spy.lastOptions(); got == nil || got[anthropicprovider.Name] == nil {
		t.Errorf("turn 2 ProviderOptions = %v, want anthropic present", got)
	}

	// Clear options: subsequent turn carries none again.
	a.SetProviderOptions(nil)
	_ = collectResponse(t, a, "three")
	if got := spy.lastOptions(); len(got) != 0 {
		t.Errorf("turn 3 ProviderOptions = %v, want empty after clear", got)
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
