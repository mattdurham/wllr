package agent_test

import (
	"context"
	"encoding/json"
	"errors"
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
	// failFirst makes the first Stream call yield a stream error after the gate
	// releases, modeling a turn that fails mid-stream while later turns succeed.
	failFirst bool
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
			g.mu.Lock()
			fail := g.failFirst
			g.mu.Unlock()
			if fail {
				yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeError, Error: errors.New("stream error")})
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

// gatedAlwaysErrLM gates every Stream call on release, then yields a stream
// error — every turn fails, modeling a persistently broken provider. Used to
// prove the error-path drain chain terminates instead of looping.
type gatedAlwaysErrLM struct {
	started chan struct{}
	release chan struct{}
	mu      sync.Mutex
	calls   int
}

func (g *gatedAlwaysErrLM) Model() string    { return "gated-err" }
func (g *gatedAlwaysErrLM) Provider() string { return "test" }

func (g *gatedAlwaysErrLM) Stream(ctx context.Context, _ fantasy.Call) (fantasy.StreamResponse, error) {
	g.mu.Lock()
	g.calls++
	g.mu.Unlock()
	return func(yield func(fantasy.StreamPart) bool) {
		select {
		case g.started <- struct{}{}:
		default:
		}
		select {
		case <-g.release:
		case <-ctx.Done():
			return
		}
		yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeError, Error: errors.New("provider down")})
	}, nil
}

func (g *gatedAlwaysErrLM) Generate(_ context.Context, _ fantasy.Call) (*fantasy.Response, error) {
	return &fantasy.Response{}, nil
}

func (g *gatedAlwaysErrLM) GenerateObject(_ context.Context, _ fantasy.ObjectCall) (*fantasy.ObjectResponse, error) {
	return nil, nil
}

func (g *gatedAlwaysErrLM) StreamObject(_ context.Context, _ fantasy.ObjectCall) (fantasy.ObjectStreamResponse, error) {
	return nil, nil
}

// TestDeliver_ErrorTurn_StillDrainsQueued verifies that a message delivered
// while a turn is running is processed even when that turn FAILS. Before the
// error-path drain fix, finishTurn only drained after successful turns, so a
// provider error stranded the queued message in the inbox until the next
// explicit Submit — for sub-agents, potentially forever.
func TestDeliver_ErrorTurn_StillDrainsQueued(t *testing.T) {
	pool := agent.NewPool()
	lm := newGatedLM()
	lm.failFirst = true
	a, err := pool.Spawn("worker", lm, agent.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	done := make(chan error, 4)
	a.SetOnDone(func(e error) { done <- e })

	// Turn 1 gates mid-stream, then fails.
	a.Submit(context.Background(), "first task")
	select {
	case <-lm.started:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for turn 1 to start")
	}

	// Deliver while running: must queue.
	if err := pool.Deliver("worker", sdk.Message{Role: sdk.RoleUser, Content: "delivered mid-turn"}, true); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if n := a.InboxLen(); n != 1 {
		t.Fatalf("inbox length while running = %d, want 1", n)
	}

	// Release: turn 1 errors, but finishTurn must still drain the queued
	// message into a drain turn — which succeeds (failFirst only fails call 1).
	close(lm.release)

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for error-path drain turn to complete")
	}

	if calls := lm.calls; calls != 2 {
		t.Errorf("stream calls = %d, want 2 (failed turn + drain turn)", calls)
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
		t.Error("mid-turn delivered message stranded by the failed turn — not in history after drain")
	}
	if n := a.InboxLen(); n != 0 {
		t.Errorf("inbox length after error-path drain = %d, want 0", n)
	}
	if a.IsRunning() {
		t.Error("agent still running after error-path drain completed")
	}
}

// TestDeliver_ErrorTurn_DrainFailureTerminatesChain verifies loop safety of the
// error-path drain: when every turn fails (persistently broken provider), each
// queued batch gets exactly one attempt and the chain stops — the drain turn's
// own failure does NOT trigger another drain. This pins the non-looping
// guarantee that justifies draining after errors at all.
func TestDeliver_ErrorTurn_DrainFailureTerminatesChain(t *testing.T) {
	pool := agent.NewPool()
	lm := &gatedAlwaysErrLM{started: make(chan struct{}, 1), release: make(chan struct{})}
	a, err := pool.Spawn("worker", lm, agent.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	done := make(chan error, 4)
	a.SetOnDone(func(e error) { done <- e })

	// Turn 1 gates, then fails.
	a.Submit(context.Background(), "first task")
	select {
	case <-lm.started:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for turn 1 to start")
	}

	// Queue a message mid-turn, then release turn 1 into its error.
	if err := pool.Deliver("worker", sdk.Message{Role: sdk.RoleUser, Content: "delivered mid-turn"}, true); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	lm.release <- struct{}{}

	// The error-path drain turn must start (call 2 gates), consuming the queued
	// message as its turn content.
	select {
	case <-lm.started:
	case <-time.After(5 * time.Second):
		t.Fatal("drain turn did not start after failed turn")
	}

	// Release the drain turn into its error. Its finishTurn must find an empty
	// inbox (the batch was consumed) and terminate the chain with onDone(err).
	lm.release <- struct{}{}

	var turnErr error
	select {
	case turnErr = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for chain to terminate after failed drain turn")
	}
	if turnErr == nil {
		t.Error("onDone error = nil, want the drain turn's error")
	}

	// Exactly two turns must have run: the failed turn and the single failed
	// drain turn. A third call would mean the drain retried itself into a loop.
	deadline := time.After(500 * time.Millisecond)
	for {
		lm.mu.Lock()
		calls := lm.calls
		lm.mu.Unlock()
		if calls > 2 {
			t.Fatalf("stream calls = %d, want 2 — error-path drain is looping", calls)
		}
		select {
		case <-deadline:
			if calls != 2 {
				t.Fatalf("stream calls = %d, want 2", calls)
			}
			if a.IsRunning() {
				t.Error("agent still running after chain terminated")
			}
			if n := a.InboxLen(); n != 0 {
				t.Errorf("inbox length after chain terminated = %d, want 0", n)
			}
			return
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// panicGateLM panics inside the first Stream call after its release gate opens;
// later calls stream successfully. Models a turn that panics mid-stream (e.g. a
// provider bug) so the Submit goroutine's recover path runs while another
// message is already queued in the inbox. fantasy only recovers panics from
// tool implementations (runToolSafely) — provider Stream panics propagate to
// the agent goroutine's recover.
type panicGateLM struct {
	started chan struct{}
	release chan struct{}
	mu      sync.Mutex
	calls   int
}

func (p *panicGateLM) Model() string    { return "panic-gate" }
func (p *panicGateLM) Provider() string { return "test" }

func (p *panicGateLM) Stream(ctx context.Context, _ fantasy.Call) (fantasy.StreamResponse, error) {
	p.mu.Lock()
	p.calls++
	first := p.calls == 1
	p.mu.Unlock()
	return func(yield func(fantasy.StreamPart) bool) {
		if first {
			select {
			case p.started <- struct{}{}:
			default:
			}
			select {
			case <-p.release:
			case <-ctx.Done():
				return
			}
			panic("provider exploded mid-stream")
		}
		if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextDelta, Delta: "ok"}) {
			return
		}
		yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeFinish, FinishReason: fantasy.FinishReasonStop})
	}, nil
}

func (p *panicGateLM) Generate(_ context.Context, _ fantasy.Call) (*fantasy.Response, error) {
	return &fantasy.Response{}, nil
}

func (p *panicGateLM) GenerateObject(_ context.Context, _ fantasy.ObjectCall) (*fantasy.ObjectResponse, error) {
	return nil, nil
}

func (p *panicGateLM) StreamObject(_ context.Context, _ fantasy.ObjectCall) (fantasy.ObjectStreamResponse, error) {
	return nil, nil
}

// TestSubmit_PanicInTurn_ReleasesRunningAndDrains is a regression test: a turn
// that panics used to fire onDone directly from the goroutine's recover,
// skipping finishTurn — isRunning stayed set forever (the agent wedged as
// permanently "running") and mid-turn inbox messages were stranded. The
// recover path must route through finishTurn: the flag is released, the queued
// message is drained into a follow-up turn, and onDone fires exactly once.
func TestSubmit_PanicInTurn_ReleasesRunningAndDrains(t *testing.T) {
	pool := agent.NewPool()
	lm := &panicGateLM{started: make(chan struct{}, 1), release: make(chan struct{})}
	a, err := pool.Spawn("worker", lm, agent.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	done := make(chan error, 4)
	a.SetOnDone(func(e error) { done <- e })

	// Turn 1 gates mid-stream, then panics on release.
	a.Submit(context.Background(), "first task")
	select {
	case <-lm.started:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for turn 1 to start")
	}
	if !a.IsRunning() {
		t.Fatal("agent should be running while turn 1 is gated")
	}

	// Queue a message mid-turn so the panicking turn's finishTurn has something
	// to drain.
	if err := pool.Deliver("worker", sdk.Message{Role: sdk.RoleUser, Content: "delivered mid-turn"}, true); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if n := a.InboxLen(); n != 1 {
		t.Fatalf("inbox length while running = %d, want 1", n)
	}

	// Release the gate: Stream panics, the recover routes through finishTurn,
	// the queued message drains into a follow-up turn (call 2, succeeds), and
	// the chain settles with a single onDone(nil).
	close(lm.release)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("chain ended with error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for chain to settle after panic")
	}

	if a.IsRunning() {
		t.Error("agent still running after panic-recovered chain settled — isRunning was never released")
	}
	if calls := lm.calls; calls != 2 {
		t.Errorf("stream calls = %d, want 2 (panicked turn + drain turn)", calls)
	}
	var foundDelivered bool
	for _, m := range a.History() {
		if strings.Contains(m.Content, "delivered mid-turn") {
			foundDelivered = true
		}
	}
	// Note: the panicking turn's own prompt ("first task") is deliberately NOT
	// asserted here — the panic aborts executeTurn before its history-recording
	// section, so an abnormally terminated turn leaves no record of its prompt
	// (and no assistant reply). The stranding fix under test is that the QUEUED
	// message survives via the drain.
	if !foundDelivered {
		t.Error("mid-turn delivered message stranded by the panic — not in history after drain")
	}
	if n := a.InboxLen(); n != 0 {
		t.Errorf("inbox length after panic-recovered drain = %d, want 0", n)
	}

	// onDone must have fired exactly once for the whole chain.
	time.Sleep(100 * time.Millisecond)
	select {
	case extra := <-done:
		t.Errorf("onDone fired more than once: %v", extra)
	default:
	}
}

// TestSubmit_NilModel_ReleasesRunning is a regression test: an agent whose
// language model is nil used to fire onDone directly from executeTurn's early
// return, skipping finishTurn — isRunning stayed set forever, so every later
// Submit silently re-queued instead of running and the agent was unrecoverable
// without a restart. The nil-LM path must route through finishTurn so the flag
// releases and the agent can be reconfigured (SetModel) and driven again.
func TestSubmit_NilModel_ReleasesRunning(t *testing.T) {
	pool := agent.NewPool()
	// pool.Spawn does not validate the LM — exactly how a nil-model agent can
	// come to exist.
	a, err := pool.Spawn("worker", nil, agent.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	done := make(chan error, 4)
	a.SetOnDone(func(e error) { done <- e })

	firstErr := make(chan error, 1)
	go func() { firstErr <- <-done }()
	a.Submit(context.Background(), "go")
	select {
	case err := <-firstErr:
		if err == nil {
			t.Fatal("nil-model turn completed without error")
		}
		if !strings.Contains(err.Error(), "no language model configured") {
			t.Errorf("error = %v, want it to mention the missing model", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for nil-model turn to fail")
	}

	// The fix's whole point: the turn must have released isRunning, so the agent
	// is idle with an empty inbox and a follow-up turn actually RUNS (before the
	// fix it wedged as running and the second Submit just re-queued).
	if a.IsRunning() {
		t.Fatal("agent still running after nil-model turn — isRunning was never released")
	}
	if n := a.InboxLen(); n != 0 {
		t.Errorf("inbox length after nil-model turn = %d, want 0", n)
	}

	// Reconfigure and drive again — proves the agent is recoverable. The
	// explicit context window satisfies the window guard that every real model
	// resolves at spawn time.
	lm := &tokenStreamLM{tokens: []string{"ok"}}
	a.SetModel(lm, "token-stream", 100_000)
	if err := pool.Send("worker", "after fix"); err != nil {
		t.Fatalf("Send after SetModel: %v", err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("post-fix turn errored: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for post-fix turn — agent likely wedged")
	}
	var found bool
	for _, m := range a.History() {
		if strings.Contains(m.Content, "after fix") {
			found = true
		}
	}
	if !found {
		t.Error("post-fix message missing from history — the second turn never ran")
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
		var event struct {
			Event     string `json:"event"`
			AgentID   string `json:"agent_id"`
			CreatorID string `json:"creator_id"`
		}
		if json.Unmarshal([]byte(m.Content), &event) == nil &&
			event.Event == "agent_idle" && event.AgentID == "main/worker" && event.CreatorID == "main" {
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
	creator, err := pool.Spawn("main", creatorLM, agent.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn creator: %v", err)
	}
	creatorTurn := make(chan []sdk.Message, 1)
	creator.SetOnTurnStart(func(_ string, inbox []sdk.Message) { creatorTurn <- inbox })

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

	// The wake-enabled delivery is consumed by the creator's turn. Observe the
	// claimed inbox rather than expecting it to remain queued.
	var msgs []sdk.Message
	select {
	case msgs = <-creatorTurn:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for AGENT_SHUTDOWN to wake creator")
	}
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
	creator, err := pool.Spawn("main", &tokenStreamLM{tokens: []string{"ack"}}, agent.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn main: %v", err)
	}
	creatorTurn := make(chan []sdk.Message, 1)
	creator.SetOnTurnStart(func(_ string, inbox []sdk.Message) { creatorTurn <- inbox })
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

	// The creator must have received AGENT_SHUTDOWN, not an idle notice. The
	// notification is consumed by the creator's wake turn.
	var msgs []sdk.Message
	select {
	case msgs = <-creatorTurn:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for AGENT_SHUTDOWN to wake creator")
	}
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

// Lifecycle notifications must stay model-visible — the orchestrator reads them
// to learn a child finished — while being typed so the transcript does not
// render them as user bubbles. The existing idle test asserts delivery; this
// pins the type that keeps them out of the chat.
func TestIdleNotification_IsProtocolTyped(t *testing.T) {
	pool := agent.NewPool()
	creator, err := pool.Spawn("main", &tokenStreamLM{tokens: []string{"ack"}}, agent.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn creator: %v", err)
	}
	worker, err := pool.Spawn("main/worker", &tokenStreamLM{tokens: []string{"done"}}, agent.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn worker: %v", err)
	}
	worker.SetCreatorID("main")

	creatorDone := make(chan error, 1)
	creator.SetOnDone(func(e error) { creatorDone <- e })
	workerDone := make(chan error, 1)
	worker.SetOnDone(func(e error) { workerDone <- e })

	if err := pool.Send("main/worker", "task"); err != nil {
		t.Fatalf("Send worker: %v", err)
	}
	if err := waitDone(t, workerDone, 5*time.Second, "worker"); err != nil {
		t.Fatalf("worker: %v", err)
	}
	if err := waitDone(t, creatorDone, 5*time.Second, "creator"); err != nil {
		t.Fatalf("creator: %v", err)
	}

	found := false
	for _, m := range creator.History() {
		if !strings.Contains(m.Content, "agent_idle") {
			continue
		}
		found = true
		if m.Type != sdk.MessageTypeProtocol {
			t.Errorf("idle notification type = %q, want %q", m.Type, sdk.MessageTypeProtocol)
		}
	}
	if !found {
		t.Fatal("creator history has no idle notification")
	}
}
