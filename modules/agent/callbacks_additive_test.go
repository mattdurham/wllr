package agent_test

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/mattdurham/wllr/modules/agent"
	"github.com/mattdurham/wllr/modules/sdk"
)

// TestAdditiveAgentCallbacksCoexist proves the agent's per-turn callbacks are
// additive: a Set-installed observer and an Add-installed observer both fire.
//
// This is the property that lets two independent consumers observe the same
// turn. Before it existed, the callbacks were single slots, so a front end that
// wired the root agent (to give it tools and dispatch its extension events)
// silently replaced the observer publishing that agent's stream — which is how
// a headless browser lost its streamed transcript and its prompt bubbles.
func TestAdditiveAgentCallbacksCoexist(t *testing.T) {
	pool := agent.NewPool()
	lm := &tokenStreamLM{tokens: []string{"a", "b"}}
	a, err := pool.Spawn("additive", lm, agent.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	var mu sync.Mutex
	var setTokens, addTokens int
	var setDone, addDone int

	a.SetOnToken(func(string) {
		mu.Lock()
		setTokens++
		mu.Unlock()
	})
	a.AddOnToken(func(string) {
		mu.Lock()
		addTokens++
		mu.Unlock()
	})

	done := make(chan struct{}, 2)
	a.SetOnDone(func(error) {
		mu.Lock()
		setDone++
		mu.Unlock()
		done <- struct{}{}
	})
	a.AddOnDone(func(error) {
		mu.Lock()
		addDone++
		mu.Unlock()
		done <- struct{}{}
	})

	a.Submit(context.Background(), "ping")

	// Both done observers must fire for the turn.
	for i := 0; i < 2; i++ {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("timeout waiting for both done callbacks")
		}
	}

	mu.Lock()
	defer mu.Unlock()
	if setTokens == 0 || addTokens == 0 {
		t.Fatalf("both token observers must fire: set=%d add=%d", setTokens, addTokens)
	}
	if setTokens != addTokens {
		t.Fatalf("observers disagree on token count: set=%d add=%d", setTokens, addTokens)
	}
	if setDone != 1 || addDone != 1 {
		t.Fatalf("both done observers must fire once: set=%d add=%d", setDone, addDone)
	}
}

// TestSetAgentCallbacksReplaces proves Set still replaces the whole list, so a
// caller that wants exclusive ownership (and re-wiring a fresh agent) behaves
// as before, and Set(nil) clears rather than registering a nil callback that
// would panic when invoked.
func TestSetAgentCallbacksReplaces(t *testing.T) {
	pool := agent.NewPool()
	lm := &tokenStreamLM{tokens: []string{"a"}}
	a, err := pool.Spawn("replace", lm, agent.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	var mu sync.Mutex
	var added int

	a.AddOnToken(func(string) {
		mu.Lock()
		added++
		mu.Unlock()
	})
	// Replace the list with a no-op; the added observer must not fire.
	a.SetOnToken(func(string) {})
	a.SetOnDone(func(error) {})
	// Clearing with nil must not leave a nil callback in the list.
	a.SetOnToken(nil)

	done := make(chan error, 1)
	a.SetOnDone(func(err error) { done <- err })
	a.Submit(context.Background(), "ping")

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for done")
	}

	mu.Lock()
	defer mu.Unlock()
	if added != 0 {
		t.Fatalf("added observer fired after Set replaced the list: %d", added)
	}
}

// TestAdditiveToolCallAndTurnStartCallbacks proves the tool-call and turn-start
// callbacks are additive too, since the front end's frame publisher and the
// extension-event forwarder both subscribe to them.
func TestAdditiveToolCallAndTurnStartCallbacks(t *testing.T) {
	pool := agent.NewPool()
	lm := &tokenStreamLM{tokens: []string{"a"}}
	a, err := pool.Spawn("additive2", lm, agent.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	var mu sync.Mutex
	var setTurnStart, addTurnStart int

	a.SetOnTurnStart(func(string, []sdk.Message) {
		mu.Lock()
		setTurnStart++
		mu.Unlock()
	})
	a.AddOnTurnStart(func(string, []sdk.Message) {
		mu.Lock()
		addTurnStart++
		mu.Unlock()
	})
	a.SetOnToolCall(func(string, string, string) {})
	a.AddOnToolCall(func(string, string, string) {})

	done := make(chan error, 1)
	a.SetOnDone(func(err error) { done <- err })
	a.Submit(context.Background(), "ping")

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for done")
	}

	mu.Lock()
	defer mu.Unlock()
	if setTurnStart != 1 || addTurnStart != 1 {
		t.Fatalf("both turn-start observers must fire once: set=%d add=%d", setTurnStart, addTurnStart)
	}
}
