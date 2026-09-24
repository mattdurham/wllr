package agent_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/mattdurham/wllr/modules/agent"
)

// TestUsageObserverReportsTurn covers the per-turn observer contract: a start
// signal, then one completion carrying the model, agent, and token counts the
// turn actually produced.
func TestUsageObserverReportsTurn(t *testing.T) {
	pool := agent.NewPool()
	var mu sync.Mutex
	var got []agent.TurnUsage
	pool.SetUsageObserver(func(u agent.TurnUsage) {
		mu.Lock()
		got = append(got, u)
		mu.Unlock()
	})

	lm := &usageLM{tokens: []string{"hi"}, inputTokens: 100, outputTokens: 20}
	a, err := pool.Spawn("main", lm, agent.SpawnOpts{ModelName: "model-x", ContextWindow: 200000})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	done := make(chan error, 1)
	a.SetOnDone(func(e error) { done <- e })
	a.Submit(context.Background(), "hi")
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("submit: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(got) != 2 {
		t.Fatalf("observer calls = %d (%+v), want a start and a completion", len(got), got)
	}
	if !got[0].Started {
		t.Errorf("first call should be a start signal: %+v", got[0])
	}
	done0 := got[1]
	if done0.Started {
		t.Errorf("second call should be a completion: %+v", done0)
	}
	if done0.AgentID != "main" || !done0.Main {
		t.Errorf("agent attribution = %q main=%v, want main/true", done0.AgentID, done0.Main)
	}
	if done0.Model != "model-x" {
		t.Errorf("model = %q, want model-x", done0.Model)
	}
	if done0.InputTokens != 100 || done0.OutputTokens != 20 {
		t.Errorf("tokens = in %d out %d, want 100/20", done0.InputTokens, done0.OutputTokens)
	}
	if done0.Err {
		t.Error("successful turn reported as an error")
	}
}

// TestUsageObserverReportsFailure verifies a failed turn is still reported, so
// turn counts reflect what ran, but with no token usage attributed.
func TestUsageObserverReportsFailure(t *testing.T) {
	pool := agent.NewPool()
	var mu sync.Mutex
	var completions []agent.TurnUsage
	pool.SetUsageObserver(func(u agent.TurnUsage) {
		if u.Started {
			return
		}
		mu.Lock()
		completions = append(completions, u)
		mu.Unlock()
	})

	a, err := pool.Spawn("main", &errStreamLM{}, agent.SpawnOpts{ModelName: "model-y", ContextWindow: 200000})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	done := make(chan error, 1)
	a.SetOnDone(func(e error) { done <- e })
	a.Submit(context.Background(), "hi")
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(completions) != 1 {
		t.Fatalf("completions = %d, want 1", len(completions))
	}
	if !completions[0].Err {
		t.Error("failed turn not marked as an error")
	}
	if completions[0].InputTokens != 0 || completions[0].TotalTokens != 0 {
		t.Errorf("failed turn attributed tokens: %+v", completions[0])
	}
}

// TestLifecycleObserverReportsSpawnAndClose covers pool membership reporting.
func TestLifecycleObserverReportsSpawnAndClose(t *testing.T) {
	pool := agent.NewPool()
	var mu sync.Mutex
	var events []agent.AgentLifecycle
	pool.SetLifecycleObserver(func(ev agent.AgentLifecycle) {
		mu.Lock()
		events = append(events, ev)
		mu.Unlock()
	})

	a, err := pool.Spawn("main/kid", &usageLM{}, agent.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if err := pool.Close(a.ID()); err != nil {
		t.Fatalf("Close: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(events) != 2 {
		t.Fatalf("events = %+v, want spawn and close", events)
	}
	if !events[0].Spawned || events[0].AgentID != "main/kid" {
		t.Errorf("spawn event = %+v", events[0])
	}
	if events[1].Spawned {
		t.Errorf("close event should not be marked spawned: %+v", events[1])
	}
	if events[1].Live != 0 {
		t.Errorf("live after close = %d, want 0", events[1].Live)
	}
}

// TestLifecycleObserverCanBeAddedWithoutReplacingThePrimaryObserver covers
// the harness's need to forward lifecycle events while metrics remains wired.
func TestLifecycleObserverCanBeAddedWithoutReplacingThePrimaryObserver(t *testing.T) {
	p := agent.NewPool()
	var primary, additional int
	p.SetLifecycleObserver(func(agent.AgentLifecycle) { primary++ })
	p.AddLifecycleObserver(func(agent.AgentLifecycle) { additional++ })

	a, err := p.Spawn("main/worker", &usageLM{}, agent.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if err := p.Close(a.ID()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if primary != 2 || additional != 2 {
		t.Fatalf("observer calls = primary %d, additional %d; want 2 each", primary, additional)
	}
}

// TestUsageObserverCoversSubagents is the reason per-agent accounting is
// possible: the observer fires for every agent, not just the main one.
func TestUsageObserverCoversSubagents(t *testing.T) {
	pool := agent.NewPool()
	seen := map[string]bool{}
	pool.SetUsageObserver(func(u agent.TurnUsage) {
		if u.Started {
			return
		}
		seen[u.AgentID] = true
	})

	for _, id := range []string{"main", "main/kid"} {
		a, err := pool.Spawn(id, &usageLM{tokens: []string{"x"}, inputTokens: 5, outputTokens: 2},
			agent.SpawnOpts{ModelName: "m", ContextWindow: 1000})
		if err != nil {
			t.Fatalf("Spawn(%s): %v", id, err)
		}
		done := make(chan struct{})
		a.SetOnDone(func(error) { close(done) })
		a.Submit(context.Background(), "hi")
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatalf("timeout waiting for %s", id)
		}
	}
	if !seen["main"] || !seen["main/kid"] {
		t.Fatalf("observed agents = %v, want main and main/kid", seen)
	}
}
