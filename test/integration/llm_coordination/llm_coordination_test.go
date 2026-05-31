//go:build integration

// Integration tests for the graceful agent shutdown protocol.
// Verifies the full flow: shutdown_request system message → finishTurn detection
// → AGENT_SHUTDOWN system message → pool removal → LLM context filtering.
//
// This test uses a scripted fake language model — no real API calls are made.
// It still requires the integration build tag because it exercises the full
// in-process agent coordination stack end-to-end.
//
// Run: go test -tags integration -v -timeout 120s ./test/integration/llm_coordination/
package llm_coordination_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"charm.land/fantasy"
	"github.com/mattdurham/wllr/modules/agent"
	"github.com/mattdurham/wllr/modules/sdk"
)

// ─── Scripted fake language model ────────────────────────────────────────────

// scriptedLM is a fantasy.LanguageModel that returns preset text responses
// one per call, blocking between calls via a release channel so tests can
// control exactly when a turn finishes.
type scriptedLM struct {
	responses []string
	idx       int
	release   chan struct{}
}

func newScriptedLM(responses ...string) *scriptedLM {
	return &scriptedLM{
		responses: responses,
		release:   make(chan struct{}),
	}
}

func (s *scriptedLM) Model() string    { return "scripted-model" }
func (s *scriptedLM) Provider() string { return "scripted" }

func (s *scriptedLM) Stream(ctx context.Context, _ fantasy.Call) (fantasy.StreamResponse, error) {
	text := ""
	if s.idx < len(s.responses) {
		text = s.responses[s.idx]
		s.idx++
	}
	rel := s.release
	return func(yield func(fantasy.StreamPart) bool) {
		// Block until released or context cancelled.
		select {
		case <-rel:
		case <-ctx.Done():
			return
		}
		if text != "" {
			if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextDelta, Delta: text}) {
				return
			}
		}
		yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeFinish, FinishReason: fantasy.FinishReasonStop})
	}, nil
}

func (s *scriptedLM) Release() { close(s.release) }

func (s *scriptedLM) Generate(_ context.Context, _ fantasy.Call) (*fantasy.Response, error) {
	return &fantasy.Response{}, nil
}

func (s *scriptedLM) GenerateObject(_ context.Context, _ fantasy.ObjectCall) (*fantasy.ObjectResponse, error) {
	return &fantasy.ObjectResponse{}, nil
}

func (s *scriptedLM) StreamObject(_ context.Context, _ fantasy.ObjectCall) (fantasy.ObjectStreamResponse, error) {
	return func(yield func(fantasy.ObjectStreamPart) bool) {}, nil
}

// instantLM is a fantasy.LanguageModel that returns a response immediately
// without blocking. Used for the orchestrator which just needs to complete turns.
type instantLM struct {
	response string
	calls    []fantasy.Call
}

func (m *instantLM) Model() string    { return "instant-model" }
func (m *instantLM) Provider() string { return "instant" }

func (m *instantLM) Stream(_ context.Context, call fantasy.Call) (fantasy.StreamResponse, error) {
	m.calls = append(m.calls, call)
	text := m.response
	return func(yield func(fantasy.StreamPart) bool) {
		if text != "" {
			if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextDelta, Delta: text}) {
				return
			}
		}
		yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeFinish, FinishReason: fantasy.FinishReasonStop})
	}, nil
}

func (m *instantLM) Generate(_ context.Context, _ fantasy.Call) (*fantasy.Response, error) {
	return &fantasy.Response{}, nil
}

func (m *instantLM) GenerateObject(_ context.Context, _ fantasy.ObjectCall) (*fantasy.ObjectResponse, error) {
	return &fantasy.ObjectResponse{}, nil
}

func (m *instantLM) StreamObject(_ context.Context, _ fantasy.ObjectCall) (fantasy.ObjectStreamResponse, error) {
	return func(yield func(fantasy.ObjectStreamPart) bool) {}, nil
}

// ─── Test helpers ─────────────────────────────────────────────────────────────

// sendShutdownRequest injects a system shutdown_request into the target agent's
// inbox, simulating what the shutdown_agent WASM tool does.
func sendShutdownRequest(pool *agent.AgentPool, targetID, fromID string) error {
	payload, _ := json.Marshal(map[string]string{
		"event": "shutdown_request",
		"from":  fromID,
	})
	return pool.SendMessage(targetID, sdk.Message{
		Role:    sdk.RoleUser,
		Content: string(payload),
		Type:    sdk.MessageTypeSystem,
	})
}

// ─── Tests ────────────────────────────────────────────────────────────────────

// TestGracefulAgentShutdown_ShutdownRequestFlow verifies the full graceful
// shutdown protocol:
//
//  1. Orchestrator sends a system shutdown_request to worker's inbox.
//  2. Worker (scripted FakeLM) finishes its current scripted turn first.
//  3. Worker's finishTurn detects shutdown_request → sends AGENT_SHUTDOWN to
//     orchestrator's inbox → removes itself from pool.
//  4. Orchestrator receives AGENT_SHUTDOWN (Type==MessageTypeSystem) in inbox.
//  5. Worker is gone from pool (pool.Get returns nil).
//  6. AGENT_SHUTDOWN does NOT appear in orchestrator's LLM context (filtered by
//     sdkToFantasyMessages when the orchestrator runs its next turn).
func TestGracefulAgentShutdown_ShutdownRequestFlow(t *testing.T) {
	pool := agent.NewPool()

	// Orchestrator uses an instant LM so we can inspect the messages it receives.
	orchLM := &instantLM{response: "acknowledged"}
	orch, err := pool.Spawn("main", orchLM, agent.SpawnOpts{
		SystemPrompt: "You are an orchestrator.",
	})
	if err != nil {
		t.Fatalf("Spawn orchestrator: %v", err)
	}

	// Worker uses a scripted blocking LM — one preset response.
	workerLM := newScriptedLM("task complete")
	_, err = pool.Spawn("main/worker", workerLM, agent.SpawnOpts{
		CreatorID:    "main",
		SystemPrompt: "You are a worker.",
	})
	if err != nil {
		t.Fatalf("Spawn worker: %v", err)
	}

	// Wire a done channel for the worker.
	worker := pool.Get("main/worker")
	if worker == nil {
		t.Fatal("worker not found after spawn")
	}
	workerDone := make(chan error, 1)
	worker.SetOnDone(func(e error) { workerDone <- e })

	// Step 1: Start a worker turn — it blocks until we release the scripted LM.
	if err := pool.Send("main/worker", "please complete the task"); err != nil {
		t.Fatalf("pool.Send worker: %v", err)
	}
	// Give the goroutine time to start and block.
	time.Sleep(20 * time.Millisecond)

	// Step 2: Orchestrator sends shutdown_request system message to worker's inbox.
	// (This is what shutdown_agent WASM tool does after the message-type change.)
	if err := sendShutdownRequest(pool, "main/worker", "main"); err != nil {
		t.Fatalf("sendShutdownRequest: %v", err)
	}

	// Step 3: Release the scripted LM so the worker turn finishes.
	// finishTurn will detect the shutdown_request, send AGENT_SHUTDOWN to "main",
	// and remove the worker from the pool.
	workerLM.Release()

	// Step 4: Wait for the worker's onDone callback.
	select {
	case err := <-workerDone:
		if err != nil {
			t.Fatalf("worker turn error: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for worker shutdown")
	}

	// Step 5: Worker must be gone from pool.
	if pool.Get("main/worker") != nil {
		t.Error("worker is still in pool after graceful shutdown — expected nil")
	}

	// Step 6: Orchestrator inbox must contain AGENT_SHUTDOWN as a system message.
	orchInbox := orch.DrainInbox()
	var shutdownMsg *sdk.Message
	for i := range orchInbox {
		m := orchInbox[i]
		if m.Type != sdk.MessageTypeSystem {
			continue
		}
		var evt struct {
			Event   string `json:"event"`
			AgentID string `json:"agent_id"`
		}
		if err := json.Unmarshal([]byte(m.Content), &evt); err != nil {
			continue
		}
		if evt.Event == "AGENT_SHUTDOWN" {
			shutdownMsg = &orchInbox[i]
			break
		}
	}
	if shutdownMsg == nil {
		t.Fatalf("AGENT_SHUTDOWN system message not found in orchestrator inbox; got %d messages: %+v",
			len(orchInbox), orchInbox)
	}

	// Confirm the agent_id field identifies the worker.
	var evt struct {
		AgentID string `json:"agent_id"`
	}
	if err := json.Unmarshal([]byte(shutdownMsg.Content), &evt); err != nil {
		t.Fatalf("unmarshal AGENT_SHUTDOWN payload: %v", err)
	}
	if evt.AgentID != "main/worker" {
		t.Errorf("AGENT_SHUTDOWN agent_id: got %q, want %q", evt.AgentID, "main/worker")
	}

	// Step 7: AGENT_SHUTDOWN must NOT appear in the orchestrator's LLM context.
	// Place the AGENT_SHUTDOWN back in the orchestrator's inbox alongside a regular
	// message so the turn has valid content, then run a turn and capture the
	// messages passed to the LLM — the system message must be absent.
	orch.AppendInbox(*shutdownMsg) // system message — must be filtered
	orch.AppendInbox(sdk.Message{
		Role:    sdk.RoleUser,
		Content: "worker shutdown confirmed",
	}) // normal message — provides the content for the LLM turn

	orchTurnDone := make(chan error, 1)
	orch.SetOnDone(func(e error) { orchTurnDone <- e })
	orch.Submit(context.Background(), "")

	select {
	case err := <-orchTurnDone:
		if err != nil {
			t.Fatalf("orchestrator turn error: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for orchestrator turn")
	}

	// Inspect the calls the instant LM received.
	calls := orchLM.calls
	if len(calls) == 0 {
		t.Fatal("orchestrator LM received no calls")
	}
	lastCall := calls[len(calls)-1]

	// The AGENT_SHUTDOWN system message must not appear in the messages slice
	// passed to the LLM (sdkToFantasyMessages must have filtered it out).
	for _, msg := range lastCall.Prompt {
		for _, part := range msg.Content {
			if tp, ok := part.(fantasy.TextPart); ok {
				var evt struct {
					Event string `json:"event"`
				}
				if json.Unmarshal([]byte(tp.Text), &evt) == nil && evt.Event == "AGENT_SHUTDOWN" {
					t.Errorf("AGENT_SHUTDOWN found in LLM context — sdkToFantasyMessages must filter system messages; msg: %q", tp.Text)
				}
			}
		}
	}
}

// TestGracefulShutdown_WorkerFinishesCurrentTurn verifies that a worker with
// both a normal pending message and a shutdown_request in its inbox finishes
// processing the normal message first, then shuts down — the drain-until-empty
// ordering guarantee.
func TestGracefulShutdown_WorkerFinishesCurrentTurn(t *testing.T) {
	pool := agent.NewPool()

	orchLM := &instantLM{response: "ok"}
	_, err := pool.Spawn("main", orchLM, agent.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn orchestrator: %v", err)
	}

	workerLM := newScriptedLM("first response", "second response")
	_, err = pool.Spawn("main/worker", workerLM, agent.SpawnOpts{
		CreatorID: "main",
	})
	if err != nil {
		t.Fatalf("Spawn worker: %v", err)
	}

	worker := pool.Get("main/worker")
	workerDone := make(chan error, 1)
	worker.SetOnDone(func(e error) { workerDone <- e })

	// Start the worker's first turn — it blocks until released.
	if err := pool.Send("main/worker", "first task"); err != nil {
		t.Fatalf("pool.Send worker: %v", err)
	}
	time.Sleep(20 * time.Millisecond)

	// While the first turn is running, queue a normal message AND a shutdown_request.
	// The normal message must be processed before the shutdown_request.
	worker.AppendInbox(sdk.Message{Role: sdk.RoleUser, Content: "do some more work"})
	if err := sendShutdownRequest(pool, "main/worker", "main"); err != nil {
		t.Fatalf("sendShutdownRequest: %v", err)
	}

	// Release the first turn; a second scripted response exists for the drain turn.
	workerLM.Release()

	// Wait for the final shutdown to complete.
	select {
	case err := <-workerDone:
		if err != nil {
			t.Fatalf("worker error: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for shutdown after drain")
	}

	// Worker must be gone — shutdown happened after the drain turn.
	if pool.Get("main/worker") != nil {
		t.Error("worker still in pool after drain-then-shutdown")
	}

	// Orchestrator must have received AGENT_SHUTDOWN.
	orch := pool.Get("main")
	if orch == nil {
		t.Fatal("orchestrator not found")
	}
	orchInbox := orch.DrainInbox()
	found := false
	for _, m := range orchInbox {
		if m.Type != sdk.MessageTypeSystem {
			continue
		}
		var evt struct {
			Event string `json:"event"`
		}
		if json.Unmarshal([]byte(m.Content), &evt) == nil && evt.Event == "AGENT_SHUTDOWN" {
			found = true
		}
	}
	if !found {
		t.Error("AGENT_SHUTDOWN not found in orchestrator inbox after drain-then-shutdown")
	}
}

// TestGracefulShutdown_AgentShutdownMessageType verifies that the AGENT_SHUTDOWN
// message delivered to the orchestrator carries Type == MessageTypeSystem.
func TestGracefulShutdown_AgentShutdownMessageType(t *testing.T) {
	pool := agent.NewPool()

	_, err := pool.Spawn("main", &instantLM{response: "ok"}, agent.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn orchestrator: %v", err)
	}

	workerLM := newScriptedLM("done")
	_, err = pool.Spawn("main/worker", workerLM, agent.SpawnOpts{CreatorID: "main"})
	if err != nil {
		t.Fatalf("Spawn worker: %v", err)
	}

	worker := pool.Get("main/worker")
	done := make(chan error, 1)
	worker.SetOnDone(func(e error) { done <- e })

	if err := pool.Send("main/worker", "task"); err != nil {
		t.Fatalf("pool.Send: %v", err)
	}
	time.Sleep(20 * time.Millisecond)

	if err := sendShutdownRequest(pool, "main/worker", "main"); err != nil {
		t.Fatalf("sendShutdownRequest: %v", err)
	}
	workerLM.Release()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("worker error: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout")
	}

	orch := pool.Get("main")
	if orch == nil {
		t.Fatal("orchestrator not found")
	}
	inbox := orch.DrainInbox()
	for _, m := range inbox {
		var evt struct {
			Event string `json:"event"`
		}
		if json.Unmarshal([]byte(m.Content), &evt) == nil && evt.Event == "AGENT_SHUTDOWN" {
			if m.Type != sdk.MessageTypeSystem {
				t.Errorf("AGENT_SHUTDOWN message has Type %q; want %q", m.Type, sdk.MessageTypeSystem)
			}
			return // test passes
		}
	}
	t.Error("AGENT_SHUTDOWN message not found in orchestrator inbox")
}
