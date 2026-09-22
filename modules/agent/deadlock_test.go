package agent_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/mattdurham/wllr/modules/agent"
	"github.com/mattdurham/wllr/modules/extension"
	"github.com/mattdurham/wllr/modules/testutil"
)

// A sub-agent's first turn starts inside Spawn, which callers may reach from
// inside an extension's WASM call. If the turn-start callback dispatches back
// into that extension synchronously, it blocks on the extension's non-reentrant
// call mutex and never returns — the create_agent deadlock.
//
// This reproduces the hazard without WASM: the callback takes a lock that the
// spawning frame already holds, and Spawn must not block on it.
func TestSpawnDoesNotBlockOnTurnStartCallback(t *testing.T) {
	prov := testutil.NewFakeProvider("hello")
	pool := agent.NewPool()
	pool.SetProvider(prov)
	pool.SetDefaultModelName("fake-model")
	pool.SetModelContextWindow("fake-model", 100000)

	lm, err := pool.LanguageModelForModel(context.Background(), "fake-model")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Spawn(agent.MainAgentID, lm, agent.SpawnOpts{
		ModelName: "fake-model", ContextWindow: 100000,
	}); err != nil {
		t.Fatal(err)
	}

	// Stand in for the extension host's per-extension call mutex: the spawning
	// frame holds it, and the turn-start callback tries to take it again.
	var callMu sync.Mutex
	callMu.Lock() // held by the "outer WASM call"

	spawner := agent.NewSpawner(pool, nil, nil)
	spawner.SetPromptObserver(func(agentID, content string, queued bool) {
		// A synchronous re-entrant dispatch would block here forever.
		callMu.Lock()
		defer callMu.Unlock()
	})

	done := make(chan error, 1)
	go func() {
		done <- spawner.Spawn(context.Background(), extension.SpawnRequest{
			ID:            "main/kid",
			Name:          "kid",
			SystemPrompt:  "child",
			ModelName:     "fake-model",
			InitialPrompt: "start",
		})
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Spawn: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Spawn blocked: the turn-start callback was invoked on the spawning stack")
	}

	// Release the outer "call" and confirm the deferred callback then runs.
	callMu.Unlock()
	time.Sleep(200 * time.Millisecond)
}
