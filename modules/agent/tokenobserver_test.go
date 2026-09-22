package agent_test

import (
	"context"
	"testing"
	"time"

	"github.com/mattdurham/wllr/modules/agent"
	"github.com/mattdurham/wllr/modules/extension"
	"github.com/mattdurham/wllr/modules/testutil"
)

// A sub-agent's streamed text must reach the token observer keyed by the agent
// that produced it. Without this, a focused view can only show text after the
// turn completes; sub-agent output is otherwise discarded entirely.
func TestSpawnerTokenObserverCarriesAgentID(t *testing.T) {
	prov := testutil.NewFakeProvider("hello from the sub-agent")
	pool := agent.NewPool()
	pool.SetProvider(prov)
	pool.SetDefaultModelName("fake-model")
	// The spawner resolves a sub-agent's window from the pool; a zero window
	// makes the turn refuse to run, which would produce no tokens.
	pool.SetModelContextWindow("fake-model", 100000)

	lm, err := pool.LanguageModelForModel(context.Background(), "fake-model")
	if err != nil {
		t.Fatalf("LanguageModelForModel: %v", err)
	}
	if _, err := pool.Spawn("main", lm, agent.SpawnOpts{ModelName: "fake-model", ContextWindow: 100000}); err != nil {
		t.Fatalf("Spawn main: %v", err)
	}

	spawner := agent.NewSpawner(pool, nil, nil)
	type ev struct{ id, text string }
	got := make(chan ev, 64)
	spawner.SetTokenObserver(func(id, text string) { got <- ev{id, text} })

	if err := spawner.Spawn(context.Background(), extension.SpawnRequest{
		ID:           "main/coder",
		Name:         "Coder",
		SystemPrompt: "you are a coder",
		ModelName:    "fake-model",
		// An initial prompt starts a turn, which is what produces tokens.
		InitialPrompt: "say hello",
	}); err != nil {
		t.Fatalf("Spawn sub-agent: %v", err)
	}

	deadline := time.After(5 * time.Second)
	seen := false
	for !seen {
		select {
		case e := <-got:
			if e.id == "" {
				t.Fatalf("token event has no agent id: %+v", e)
			}
			if e.id != "main/coder" {
				t.Fatalf("agent = %q, want main/coder", e.id)
			}
			seen = true
		case <-deadline:
			t.Fatal("no token event; sub-agent streamed text is not routed")
		}
	}
}
