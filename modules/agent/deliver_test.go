package agent_test

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mattdurham/wllr/modules/agent"
	"github.com/mattdurham/wllr/modules/sdk"
)

// TestDeliver_WakesIdleAgent verifies that Deliver(wake=true) queues a message
// AND starts a turn that processes it — the atomic deliver-and-process primitive.
func TestDeliver_WakesIdleAgent(t *testing.T) {
	pool := agent.NewPool()
	lm := &tokenStreamLM{tokens: []string{"ack"}}
	a, err := pool.Spawn("worker", lm, agent.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	done := make(chan error, 1)
	a.SetOnDone(func(e error) { done <- e })

	if err := pool.Deliver("worker", sdk.Message{Role: sdk.RoleUser, Content: "do work"}, true); err != nil {
		t.Fatalf("Deliver: %v", err)
	}

	if err := waitDone(t, done, 5*time.Second, "worker"); err != nil {
		t.Errorf("worker done with error: %v", err)
	}

	// The delivered message must appear in history (it became the turn content).
	var found bool
	for _, m := range a.History() {
		if strings.Contains(m.Content, "do work") {
			found = true
		}
	}
	if !found {
		t.Error("delivered message not found in history — Deliver did not process the inbox")
	}
}

// TestDeliver_NoWakeQueuesOnly verifies that Deliver(wake=false) queues without
// starting a turn.
func TestDeliver_NoWakeQueuesOnly(t *testing.T) {
	pool := agent.NewPool()
	lm := &tokenStreamLM{tokens: []string{"ack"}}
	a, err := pool.Spawn("worker", lm, agent.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	a.SetOnDone(func(error) { t.Error("onDone fired — wake=false must not start a turn") })

	if err := pool.Deliver("worker", sdk.Message{Role: sdk.RoleUser, Content: "queued"}, false); err != nil {
		t.Fatalf("Deliver: %v", err)
	}

	// Give any erroneous goroutine a chance to fire.
	time.Sleep(100 * time.Millisecond)

	// Message must still be queued in the inbox.
	if n := a.InboxLen(); n != 1 {
		t.Errorf("inbox length = %d, want 1 (message queued, not processed)", n)
	}
}

// TestDeliver_EmptyContentRejected verifies the non-empty content guard.
func TestDeliver_EmptyContentRejected(t *testing.T) {
	pool := agent.NewPool()
	lm := &tokenStreamLM{tokens: []string{"ack"}}
	if _, err := pool.Spawn("worker", lm, agent.SpawnOpts{}); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if err := pool.Deliver("worker", sdk.Message{Role: sdk.RoleUser, Content: "   "}, true); err == nil {
		t.Error("expected error for empty content, got nil")
	}
}

// TestDeliver_UnknownAgent verifies ErrAgentNotFound for an unknown ID.
func TestDeliver_UnknownAgent(t *testing.T) {
	pool := agent.NewPool()
	err := pool.Deliver("ghost", sdk.Message{Role: sdk.RoleUser, Content: "x"}, true)
	if err != agent.ErrAgentNotFound {
		t.Errorf("Deliver(unknown) = %v, want ErrAgentNotFound", err)
	}
}

// TestDeliver_WakeNotifierFires verifies that the wake notifier callback is
// invoked with the agent ID when Deliver wakes an agent.
func TestDeliver_WakeNotifierFires(t *testing.T) {
	pool := agent.NewPool()
	lm := &tokenStreamLM{tokens: []string{"ack"}}
	a, err := pool.Spawn("worker", lm, agent.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	done := make(chan error, 1)
	a.SetOnDone(func(e error) { done <- e })

	var mu sync.Mutex
	var notified []string
	pool.SetWakeNotifier(func(id string) {
		mu.Lock()
		notified = append(notified, id)
		mu.Unlock()
	})

	if err := pool.Deliver("worker", sdk.Message{Role: sdk.RoleUser, Content: "go"}, true); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	_ = waitDone(t, done, 5*time.Second, "worker")

	mu.Lock()
	defer mu.Unlock()
	if len(notified) != 1 || notified[0] != "worker" {
		t.Errorf("wake notifier got %v, want [worker]", notified)
	}
}

// TestIdleNotification_WakesCreator verifies that a sub-agent with a creatorID,
// on completing a turn and going idle, notifies its creator via the inbox and
// wakes it.
func TestIdleNotification_WakesCreator(t *testing.T) {
	pool := agent.NewPool()

	// Creator: long response so we can observe it receiving the idle notification.
	creatorLM := &tokenStreamLM{tokens: []string{"creator-ack"}}
	creator, err := pool.Spawn("main", creatorLM, agent.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn creator: %v", err)
	}

	// Worker with creatorID="main" via the spawner convention. pool.Spawn does
	// not set creatorID, so we use the spawner path is unavailable here; instead
	// drive it through the documented creatorID field by spawning then setting
	// history. We rely on the spawner test for creatorID wiring; here we verify
	// the notification path fires when creatorID is set.
	workerLM := &tokenStreamLM{tokens: []string{"worker-done"}}
	worker, err := pool.Spawn("main/worker", workerLM, agent.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn worker: %v", err)
	}

	// Capture the creator being woken.
	creatorDone := make(chan error, 1)
	creator.SetOnDone(func(e error) { creatorDone <- e })

	// The worker must have a creatorID for the idle notification to fire. This is
	// normally set by Spawner.Spawn; we assert the behaviour via SetCreatorID.
	worker.SetCreatorID("main")

	workerDone := make(chan error, 1)
	worker.SetOnDone(func(e error) { workerDone <- e })

	// Run the worker's turn — on completion it should notify "main".
	if err := pool.Send("main/worker", "task"); err != nil {
		t.Fatalf("Send worker: %v", err)
	}
	if err := waitDone(t, workerDone, 5*time.Second, "worker"); err != nil {
		t.Errorf("worker error: %v", err)
	}

	// The creator should be woken by the idle notification and complete a turn.
	if err := waitDone(t, creatorDone, 5*time.Second, "creator (idle notification)"); err != nil {
		t.Errorf("creator was not woken by idle notification: %v", err)
	}

	// The creator's history should contain the idle notification text.
	var found bool
	for _, m := range creator.History() {
		if strings.Contains(m.Content, "is idle") && strings.Contains(m.Content, "main/worker") {
			found = true
		}
	}
	if !found {
		t.Error("creator history does not contain the idle notification")
	}
}

// TestIdleNotification_TopLevelAgentDoesNotSelfNotify verifies that an agent
// with no creatorID (e.g. main) never sends an idle notification.
func TestIdleNotification_TopLevelAgentDoesNotSelfNotify(t *testing.T) {
	pool := agent.NewPool()
	lm := &tokenStreamLM{tokens: []string{"done"}}
	a, err := pool.Spawn("main", lm, agent.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	done := make(chan error, 1)
	a.SetOnDone(func(e error) { done <- e })

	if err := pool.Send("main", "task"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if err := waitDone(t, done, 5*time.Second, "main"); err != nil {
		t.Errorf("main error: %v", err)
	}

	// main has no creator, so its inbox must remain empty (no self-notification).
	if n := a.InboxLen(); n != 0 {
		t.Errorf("top-level agent inbox = %d, want 0 (no self-notification)", n)
	}
}
