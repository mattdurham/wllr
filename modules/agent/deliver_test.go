package agent_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"charm.land/fantasy"
	"github.com/mattdurham/wllr/modules/agent"
	"github.com/mattdurham/wllr/modules/sdk"
)

// gatedLM blocks the first Stream call on a release channel, then streams its
// token. Subsequent calls stream immediately. Used to hold an agent mid-turn so
// a concurrent Deliver lands while isRunning==true (the drain-until-empty path).
type gatedLM struct {
	release chan struct{}
	started chan struct{}
	mu      sync.Mutex
	calls   int
}

func newGatedLM() *gatedLM {
	return &gatedLM{release: make(chan struct{}), started: make(chan struct{}, 1)}
}

func (g *gatedLM) Model() string    { return "gated" }
func (g *gatedLM) Provider() string { return "test" }

func (g *gatedLM) Stream(ctx context.Context, _ fantasy.Call) (fantasy.StreamResponse, error) {
	g.mu.Lock()
	g.calls++
	first := g.calls == 1
	g.mu.Unlock()
	return func(yield func(fantasy.StreamPart) bool) {
		if first {
			select {
			case g.started <- struct{}{}:
			default:
			}
			select {
			case <-g.release:
			case <-ctx.Done():
				return
			}
		}
		if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextDelta, Delta: "ok"}) {
			return
		}
		yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeFinish, FinishReason: fantasy.FinishReasonStop})
	}, nil
}

func (g *gatedLM) Generate(_ context.Context, _ fantasy.Call) (*fantasy.Response, error) {
	return &fantasy.Response{}, nil
}

func (g *gatedLM) GenerateObject(_ context.Context, _ fantasy.ObjectCall) (*fantasy.ObjectResponse, error) {
	return nil, nil
}

func (g *gatedLM) StreamObject(_ context.Context, _ fantasy.ObjectCall) (fantasy.ObjectStreamResponse, error) {
	return nil, nil
}

// TestDeliver_WhileRunning_DrainsAfterTurn verifies the concurrent branch: a
// Deliver that arrives while the agent is mid-turn is queued (Submit's CAS
// fails) and then processed by finishTurn's drain-until-empty after the running
// turn completes — no message is lost, and no second goroutine is started early.
func TestDeliver_WhileRunning_DrainsAfterTurn(t *testing.T) {
	pool := agent.NewPool()
	lm := newGatedLM()
	a, err := pool.Spawn("worker", lm, agent.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	// onDone fires exactly once: the initial turn defers it to the drain turn
	// (drain-until-empty), so a mid-turn delivery does NOT cause a second
	// onDone — the drain sub-turn fires the single completion.
	done := make(chan error, 4)
	a.SetOnDone(func(e error) { done <- e })

	// Start turn 1 — it blocks inside Stream until released.
	a.Submit(context.Background(), "first task")
	select {
	case <-lm.started:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for turn 1 to start")
	}
	if !a.IsRunning() {
		t.Fatal("agent should be running while turn 1 is gated")
	}

	// Deliver while running: must queue (not start a turn) and the message must
	// be picked up after the current turn finishes.
	if err := pool.Deliver("worker", sdk.Message{Role: sdk.RoleUser, Content: "delivered mid-turn"}, true); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if n := a.InboxLen(); n != 1 {
		t.Fatalf("inbox length while running = %d, want 1 (queued, not yet drained)", n)
	}

	// Release the gate; turn 1 completes, then finishTurn drains the mid-turn
	// message into a drain turn which fires the single onDone.
	close(lm.release)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("turn error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for drain turn to complete")
	}

	// Drain-until-empty guarantees the agent is idle with an empty inbox once the
	// final onDone fires. Both the initial and delivered messages must be in
	// history; the mid-turn delivery must not have been lost.
	if a.IsRunning() {
		t.Error("agent still running after final onDone")
	}
	var foundFirst, foundDelivered bool
	for _, m := range a.History() {
		if strings.Contains(m.Content, "first task") {
			foundFirst = true
		}
		if strings.Contains(m.Content, "delivered mid-turn") {
			foundDelivered = true
		}
	}
	if !foundFirst {
		t.Error("initial message missing from history")
	}
	if !foundDelivered {
		t.Error("mid-turn delivered message was lost — not found in history after drain")
	}
	if n := a.InboxLen(); n != 0 {
		t.Errorf("inbox length after drain = %d, want 0", n)
	}
}

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

	// Worker with creatorID="main". pool.Spawn does not set creatorID (that is the
	// spawner's job, covered in spawner_test.go); here we set it via SetCreatorID
	// to isolate the idle-notification path.
	workerLM := &tokenStreamLM{tokens: []string{"worker-done"}}
	worker, err := pool.Spawn("main/worker", workerLM, agent.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn worker: %v", err)
	}

	// Capture the creator being woken.
	creatorDone := make(chan error, 1)
	creator.SetOnDone(func(e error) { creatorDone <- e })

	// The worker must have a creatorID for the idle notification to fire. This is
	// normally set by Spawner.Spawn; we assert the behavior via SetCreatorID.
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

// TestIdleNotification_SuppressedDuringShutdown verifies that a sub-agent told
// to shut down does NOT also send its creator an AGENT_IDLE notification — the
// shutdown path takes precedence. The creator should receive exactly one
// message (AGENT_SHUTDOWN), not an idle notice as well.
func TestIdleNotification_SuppressedDuringShutdown(t *testing.T) {
	pool := agent.NewPool()

	creatorLM := &tokenStreamLM{tokens: []string{"ack"}}
	if _, err := pool.Spawn("main", creatorLM, agent.SpawnOpts{}); err != nil {
		t.Fatalf("Spawn creator: %v", err)
	}
	// Stop the creator from auto-running on delivery so we can inspect its raw
	// inbox deterministically.
	creator := pool.Get("main")

	workerLM := &tokenStreamLM{tokens: []string{"worker-done"}}
	worker, err := pool.Spawn("main/worker", workerLM, agent.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn worker: %v", err)
	}
	worker.SetCreatorID("main")

	workerDone := make(chan error, 2)
	worker.SetOnDone(func(e error) { workerDone <- e })

	// Deliver a shutdown_request, then trigger the worker's turn. finishTurn
	// should send AGENT_SHUTDOWN to the creator and self-close — without an
	// AGENT_IDLE.
	shutdownPayload := `{"event":"shutdown_request","from":"main"}`
	if err := pool.SendMessage("main/worker", sdk.Message{
		Role:    sdk.RoleUser,
		Content: shutdownPayload,
		Type:    sdk.MessageTypeSystem,
	}); err != nil {
		t.Fatalf("SendMessage shutdown: %v", err)
	}
	if err := pool.Send("main/worker", "work"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if err := waitDone(t, workerDone, 5*time.Second, "worker"); err != nil {
		t.Errorf("worker error: %v", err)
	}

	// Allow the AGENT_SHUTDOWN delivery (and any erroneous idle delivery) to land.
	// The worker self-closes, so poll the creator inbox briefly.
	deadline := time.After(2 * time.Second)
	for creator.InboxLen() < 1 {
		select {
		case <-deadline:
			t.Fatal("timeout waiting for AGENT_SHUTDOWN to reach creator")
		case <-time.After(10 * time.Millisecond):
		}
	}
	// Give any (erroneous) second delivery a chance to arrive before asserting.
	time.Sleep(100 * time.Millisecond)

	msgs := creator.DrainInbox()
	var shutdownCount, idleCount int
	for _, m := range msgs {
		if strings.Contains(m.Content, "AGENT_SHUTDOWN") {
			shutdownCount++
		}
		if strings.Contains(m.Content, "is idle") {
			idleCount++
		}
	}
	if shutdownCount != 1 {
		t.Errorf("creator received %d AGENT_SHUTDOWN, want 1", shutdownCount)
	}
	if idleCount != 0 {
		t.Errorf("creator received %d idle notifications during shutdown, want 0", idleCount)
	}
}

// TestDeliver_ShutdownRequestToIdleAgent is a regression test: delivering a
// shutdown_request to an IDLE agent (wake=true) must self-close it. Before the
// control-only-wake short-circuit in executeTurn, Submit drained the
// shutdown_request as turn content, the system message was filtered from LLM
// context, the LLM call errored with "prompt can't be empty", and the erroring
// turn skipped finishTurn's shutdown handling — stranding the agent forever.
func TestDeliver_ShutdownRequestToIdleAgent(t *testing.T) {
	pool := agent.NewPool()
	if _, err := pool.Spawn("main", &tokenStreamLM{tokens: []string{"ack"}}, agent.SpawnOpts{}); err != nil {
		t.Fatalf("Spawn main: %v", err)
	}
	worker, err := pool.Spawn("main/worker", &tokenStreamLM{tokens: []string{"done"}}, agent.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn worker: %v", err)
	}
	worker.SetCreatorID("main")
	done := make(chan error, 4)
	worker.SetOnDone(func(e error) { done <- e })

	// Worker is idle. shutdown_agent delivers a system shutdown_request with wake.
	payload := `{"event":"shutdown_request","from":"main"}`
	if err := pool.Deliver("main/worker", sdk.Message{
		Role:    sdk.RoleUser,
		Content: payload,
		Type:    sdk.MessageTypeSystem,
	}, true); err != nil {
		t.Fatalf("Deliver: %v", err)
	}

	if err := waitDone(t, done, 5*time.Second, "worker"); err != nil {
		t.Errorf("worker turn errored (must be a clean control-only wake): %v", err)
	}

	// The worker must have self-closed.
	deadline := time.After(2 * time.Second)
	for pool.Get("main/worker") != nil {
		select {
		case <-deadline:
			t.Fatal("worker still in pool — shutdown_request to idle agent was lost")
		case <-time.After(10 * time.Millisecond):
		}
	}

	// The creator must have received AGENT_SHUTDOWN, not an idle notice.
	creator := pool.Get("main")
	msgs := creator.DrainInbox()
	var gotShutdown bool
	for _, m := range msgs {
		if strings.Contains(m.Content, "AGENT_SHUTDOWN") {
			gotShutdown = true
		}
		if strings.Contains(m.Content, "is idle") {
			t.Error("creator got an idle notice for a shutdown — shutdown path must suppress idle")
		}
	}
	if !gotShutdown {
		t.Errorf("creator did not receive AGENT_SHUTDOWN; inbox: %+v", msgs)
	}
}

// TestIdleNotification_MultipleWorkersCoalesce verifies that several sub-agent
// idle notices delivered to a busy creator are not lost: drain-until-empty
// batches them so all reach the creator's history.
//
// Determinism: the creator is gated mid-turn (gatedLM) so every idle delivery
// provably lands while the creator is running and is therefore queued (Submit's
// CAS fails) rather than racing separate turns. Releasing the gate lets
// finishTurn drain all queued notices. This removes the timing nondeterminism
// of polling for a "settled" creator.
func TestIdleNotification_MultipleWorkersCoalesce(t *testing.T) {
	pool := agent.NewPool()

	creatorLM := newGatedLM()
	creator, err := pool.Spawn("main", creatorLM, agent.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn creator: %v", err)
	}

	// Start and gate the creator's turn so it is provably running while the
	// workers deliver their idle notices.
	creatorDone := make(chan error, 8)
	creator.SetOnDone(func(e error) { creatorDone <- e })
	creator.Submit(context.Background(), "orchestrate")
	select {
	case <-creatorLM.started:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for creator turn to start")
	}

	const nWorkers = 5
	workerIDs := make([]string, nWorkers)
	var workerWG sync.WaitGroup
	for i := 0; i < nWorkers; i++ {
		id := "main/w" + string(rune('a'+i))
		workerIDs[i] = id
		lm := &tokenStreamLM{tokens: []string{"done"}}
		w, err := pool.Spawn(id, lm, agent.SpawnOpts{})
		if err != nil {
			t.Fatalf("Spawn %s: %v", id, err)
		}
		w.SetCreatorID("main")
		done := make(chan error, 1)
		w.SetOnDone(func(error) { done <- nil })
		workerWG.Add(1)
		go func() {
			defer workerWG.Done()
			<-done
		}()
		if err := pool.Send(id, "task"); err != nil {
			t.Fatalf("Send %s: %v", id, err)
		}
	}
	// All workers finish and deliver their idle notices to the (gated) creator.
	workerWG.Wait()

	// The creator is still gated; all 5 notices must be queued in its inbox.
	// Poll briefly to let the final Deliver's AppendInbox land before release.
	deadline := time.After(2 * time.Second)
	for creator.InboxLen() < nWorkers {
		select {
		case <-deadline:
			t.Fatalf("creator inbox has %d notices while gated, want %d (lost wakeup)", creator.InboxLen(), nWorkers)
		case <-time.After(10 * time.Millisecond):
		}
	}

	// Release the gate: turn 1 completes, then drain-until-empty processes all
	// queued idle notices. Wait for the agent to go idle (final onDone with empty inbox).
	close(creatorLM.release)
	drainDeadline := time.After(5 * time.Second)
	for {
		select {
		case <-creatorDone:
		case <-drainDeadline:
			t.Fatal("timeout waiting for creator to drain queued notices")
		}
		if !creator.IsRunning() && creator.InboxLen() == 0 {
			break
		}
	}

	// Every worker's idle notice must appear in the creator's history exactly
	// once — none lost, none duplicated.
	seen := make(map[string]int)
	for _, m := range creator.History() {
		if !strings.Contains(m.Content, "is idle") {
			continue
		}
		for _, id := range workerIDs {
			if strings.Contains(m.Content, id) {
				seen[id]++
			}
		}
	}
	for _, id := range workerIDs {
		if seen[id] != 1 {
			t.Errorf("worker %s idle notices in creator history = %d, want 1", id, seen[id])
		}
	}
}
