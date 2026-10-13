package server

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"sync"
	"testing"
	"time"

	"github.com/mattdurham/wllr/modules/agent"
)

// TestBridgeAttachesAdditively proves the Bridge subscribes to an agent's
// stream without displacing observers already attached to it, and still
// publishes its own frames.
//
// The Bridge is one of several possible consumers of an agent's stream (the
// harness wires the root agent's callbacks too). With single-slot Set* calls
// the later subscriber silently replaced the earlier one, which is how a
// headless browser lost first its streamed transcript and then, once the root
// agent was wired, its prompt bubbles.
func TestBridgeAttachesAdditively(t *testing.T) {
	pool, _ := newTestPool(t)

	a := pool.Get(agent.MainAgentID)
	if a == nil {
		t.Fatal("main agent missing")
	}

	// Stand in for the other consumer (the harness's root-agent wiring).
	var mu sync.Mutex
	var otherTokens int
	a.SetOnToken(func(string) {
		mu.Lock()
		otherTokens++
		mu.Unlock()
	})

	srv := New(pool, Options{})
	srv.Start()

	ch, _, cancel := srv.Hub().Subscribe(0)
	defer cancel()

	if err := pool.Send(agent.MainAgentID, "hi"); err != nil {
		t.Fatalf("send: %v", err)
	}
	events := collectUntilTurnDone(t, ch)

	var bridgeTokens int
	for _, ev := range events {
		if ev.Kind == KindToken {
			bridgeTokens++
		}
	}
	if bridgeTokens == 0 {
		t.Fatal("Bridge published no token frames after Start")
	}

	mu.Lock()
	defer mu.Unlock()
	if otherTokens == 0 {
		t.Fatal("pre-existing token observer was clobbered by Bridge.Start")
	}
}

// TestBridgeMessageEndNotifierDeliversFinalizedText proves the Bridge reports a
// turn's finalized assistant text to the installed notifier, which is how a
// front end with no tea loop triggers EventMessageEnd (the event the bundled
// agents extension renders the finalized markdown bubble on).
func TestBridgeMessageEndNotifierDeliversFinalizedText(t *testing.T) {
	pool, _ := newTestPool(t)
	srv := New(pool, Options{})

	type endCall struct{ role, content string }
	got := make(chan endCall, 1)
	srv.SetMessageEndNotifier(func(role, content string) { got <- endCall{role, content} })
	srv.Start()

	ch, _, cancel := srv.Hub().Subscribe(0)
	defer cancel()

	if err := pool.Send(agent.MainAgentID, "hi"); err != nil {
		t.Fatalf("send: %v", err)
	}
	collectUntilTurnDone(t, ch)

	select {
	case ec := <-got:
		if ec.role != "assistant" {
			t.Fatalf("role = %q, want assistant", ec.role)
		}
		if ec.content == "" {
			t.Fatal("finalized content is empty")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("message-end notifier never fired")
	}
}
