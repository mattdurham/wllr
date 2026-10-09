package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/mattdurham/wllr/modules/agent"
	"github.com/mattdurham/wllr/modules/extension"
	"github.com/mattdurham/wllr/modules/sdk"
)

// TestWireMainAgentCallbacksDoesNotClobberExistingObserver proves the root
// agent can be wired for a headless front end without displacing an observer
// already attached to it.
//
// This is the regression that left a browser front end with no streamed
// transcript and a tool-less agent: the server's per-agent Bridge attaches
// observers, and the harness wiring used single-slot Set* calls, so whichever
// ran second silently replaced the other.
func TestWireMainAgentCallbacksDoesNotClobberExistingObserver(t *testing.T) {
	pool := agent.NewPool()
	a, err := pool.Spawn(agent.MainAgentID, newMockLM("hello there"), agent.SpawnOpts{
		ModelName: "fake", ContextWindow: 1000,
	})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}

	// Stand in for the server's Bridge observer.
	var mu sync.Mutex
	var preExistingTokens int
	a.SetOnToken(func(string) {
		mu.Lock()
		preExistingTokens++
		mu.Unlock()
	})

	m := New(pool, agent.MainAgentID, nil)
	// A no-op sink: the front end's frame publisher is the pre-existing
	// observer, so this wiring must add only the extension-dispatch side.
	m.WireMainAgentCallbacks(&sdk.FuncSink{})

	done := make(chan struct{}, 1)
	a.SetOnDone(func(error) { done <- struct{}{} })

	a.Submit(context.Background(), "ping")
	for {
		select {
		case <-done:
			mu.Lock()
			n := preExistingTokens
			mu.Unlock()
			if n == 0 {
				t.Fatal("pre-existing token observer was clobbered by WireMainAgentCallbacks")
			}
			return
		case <-time.After(5 * time.Second):
			t.Fatal("timeout waiting for turn to finish")
		}
	}
}

// TestDispatchSessionStartReachesExtensions proves the headless session-start
// dispatch delivers EventSessionStart, which is what makes the bundled `agents`
// extension create the chat transcript area in serve mode. Without it a browser
// received no extension-driven transcript at all.
func TestDispatchSessionStartReachesExtensions(t *testing.T) {
	host := extension.NewHost(nil)
	ctx := context.Background()
	defer func() { _ = host.Close(ctx) }()

	got := make(chan sdk.SessionStartPayload, 1)
	host.Bus.Subscribe(sdk.EventSessionStart, func(_ context.Context, evt sdk.Event) error {
		var p sdk.SessionStartPayload
		if err := json.Unmarshal(evt.Payload, &p); err != nil {
			return err
		}
		got <- p
		return nil
	})

	m := New(nil, agent.MainAgentID, host)
	if err := m.DispatchSessionStart(ctx); err != nil {
		t.Fatalf("DispatchSessionStart: %v", err)
	}

	select {
	case p := <-got:
		if p.Reason != "new_session" {
			t.Fatalf("reason = %q, want new_session", p.Reason)
		}
		if p.CWD == "" {
			t.Fatal("session-start payload carried no working directory")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("EventSessionStart never reached the host")
	}
}

// TestDispatchSessionStartNilHostIsNoop proves a build with no extension host
// (serve still works without extensions) is a silent no-op, not a panic.
func TestDispatchSessionStartNilHostIsNoop(t *testing.T) {
	m := New(nil, agent.MainAgentID, nil)
	if err := m.DispatchSessionStart(context.Background()); err != nil {
		t.Fatalf("DispatchSessionStart with nil host: %v", err)
	}
	if err := m.DispatchModelChanged(context.Background()); err != nil {
		t.Fatalf("DispatchModelChanged with nil host: %v", err)
	}
	if err := m.DispatchMessageEnd(context.Background(), "assistant", "hi"); err != nil {
		t.Fatalf("DispatchMessageEnd with nil host: %v", err)
	}
}

// TestDispatchMessageEndReachesExtensions proves the headless message-end
// dispatch delivers EventMessageEnd with the finalized assistant text. The
// bundled `agents` extension renders the finalized (markdown) assistant bubble
// only on that event; a front end with no tea loop has no other trigger.
func TestDispatchMessageEndReachesExtensions(t *testing.T) {
	host := extension.NewHost(nil)
	ctx := context.Background()
	defer func() { _ = host.Close(ctx) }()

	got := make(chan sdk.MessageEndPayload, 1)
	host.Bus.Subscribe(sdk.EventMessageEnd, func(_ context.Context, evt sdk.Event) error {
		var p sdk.MessageEndPayload
		if err := json.Unmarshal(evt.Payload, &p); err != nil {
			return err
		}
		got <- p
		return nil
	})

	m := New(nil, agent.MainAgentID, host)
	if err := m.DispatchMessageEnd(ctx, string(sdk.RoleAssistant), "**done**"); err != nil {
		t.Fatalf("DispatchMessageEnd: %v", err)
	}

	select {
	case p := <-got:
		if p.Role != string(sdk.RoleAssistant) || p.Content != "**done**" {
			t.Fatalf("payload = %+v, want assistant/`**done**`", p)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("EventMessageEnd never reached the host")
	}
}
