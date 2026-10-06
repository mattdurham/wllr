package agent_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mattdurham/wllr/modules/agent"
	"github.com/mattdurham/wllr/modules/extension"
	"github.com/mattdurham/wllr/modules/testutil"
)

// TestSpawnerTokenFlushDeliversTailAtTurnEnd pins the segment-boundary flush
// contract for sub-agents: the per-agent token batcher holds tokens that
// arrive within 75ms of the previous send, so the last words of a fast
// response are still buffered when the turn ends. Without a flush at turn
// end, the tail of a sub-agent's final text segment was never dispatched at
// all — focused transcripts rendered the response permanently one fragment
// short ("Waiting for the" with the rest missing).
func TestSpawnerTokenFlushDeliversTailAtTurnEnd(t *testing.T) {
	const response = " Waiting for the brainstormer"
	prov := testutil.NewFakeProvider(response)
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

	var mu sync.Mutex
	var text strings.Builder
	flushed := make(chan string, 8)
	spawner.SetTokenObserver(func(id, tok string) {
		mu.Lock()
		text.WriteString(tok)
		mu.Unlock()
	})
	spawner.SetTokenFlushObserver(func(id string) {
		flushed <- id
	})

	if err := spawner.Spawn(context.Background(), extension.SpawnRequest{
		ID:            "main/coder",
		Name:          "Coder",
		SystemPrompt:  "you are a coder",
		ModelName:     "fake-model",
		InitialPrompt: "say hello",
	}); err != nil {
		t.Fatalf("Spawn sub-agent: %v", err)
	}

	// Wait for the turn-end flush.
	deadline := time.After(10 * time.Second)
	var flushAgent string
	select {
	case flushAgent = <-flushed:
	case <-deadline:
		t.Fatal("token flush observer never fired at turn end")
	}
	if flushAgent != "main/coder" {
		t.Fatalf("flush agent = %q, want main/coder", flushAgent)
	}

	mu.Lock()
	got := text.String()
	mu.Unlock()
	if got != response {
		t.Fatalf("streamed text = %q, want complete response %q (tail lost)", got, response)
	}
}
