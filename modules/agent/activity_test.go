package agent_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/mattdurham/wllr/modules/agent"
	"github.com/mattdurham/wllr/modules/sdk"
	"github.com/mattdurham/wllr/modules/testutil"
)

func TestAgentActivity_TracksRunningTurnAndToolCall(t *testing.T) {
	pool := agent.NewPool()
	lm := testutil.NewFakeLM()
	lm.SetScript([]testutil.ScriptedTurn{{
		Text: "working",
		ToolCalls: []testutil.ScriptedToolCall{{
			ID:    "call-1",
			Name:  "read_file",
			Input: json.RawMessage(`{"path":"x"}`),
		}},
	}})
	a, err := pool.Spawn("worker", lm, agent.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	done := make(chan error, 1)
	a.SetOnDone(func(err error) { done <- err })

	a.Submit(context.Background(), "inspect")
	if err := waitDone(t, done, 5*time.Second, "worker"); err != nil {
		t.Fatalf("turn error: %v", err)
	}

	activity := a.Activity()
	if activity.TurnStartedAt.IsZero() {
		t.Fatal("TurnStartedAt should be set")
	}
	if activity.LastActivityAt.IsZero() {
		t.Fatal("LastActivityAt should be set")
	}
	if activity.LastToolCallAt.IsZero() {
		t.Fatal("LastToolCallAt should be set")
	}
	if activity.LastToolName != "read_file" {
		t.Fatalf("LastToolName = %q, want read_file", activity.LastToolName)
	}
	if activity.ActiveToolName != "" {
		t.Fatalf("ActiveToolName after turn = %q, want empty", activity.ActiveToolName)
	}
}

func TestAgentActivity_ShutdownRequestedWhileRunning(t *testing.T) {
	pool := agent.NewPool()
	lm := newGatedLM()
	a, err := pool.Spawn("worker", lm, agent.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	done := make(chan error, 1)
	a.SetOnDone(func(err error) { done <- err })
	a.Submit(context.Background(), "long task")
	select {
	case <-lm.started:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for turn to start")
	}

	payload, _ := json.Marshal(map[string]string{"event": "shutdown_request", "from": "main"})
	if err := pool.Deliver("worker", sdk.Message{
		Role:    sdk.RoleUser,
		Content: string(payload),
		Type:    sdk.MessageTypeSystem,
	}, true); err != nil {
		t.Fatalf("Deliver shutdown: %v", err)
	}

	if !a.Activity().ShutdownRequested {
		t.Fatal("ShutdownRequested should be true after shutdown request is queued")
	}
	close(lm.release)
	_ = waitDone(t, done, 5*time.Second, "worker")
}
