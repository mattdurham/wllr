package harness

import (
	"context"
	"testing"
	"time"

	"github.com/mattdurham/wllr/modules/agent"
	"github.com/mattdurham/wllr/modules/extension"
	"github.com/mattdurham/wllr/modules/sdk"
)

func TestHarnessAgentBridgeListIncludesRuntimeState(t *testing.T) {
	pool := agent.NewPool()
	lm := &blockingLM{started: make(chan struct{})}
	a, err := pool.Spawn("worker", lm, agent.SpawnOpts{Name: "Worker"})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if err := pool.SendMessage("worker", sdk.Message{Role: sdk.RoleUser, Content: "drained"}); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if _, err := pool.Spawn("idle", newMockLM("ok"), agent.SpawnOpts{Name: "Idle"}); err != nil {
		t.Fatalf("Spawn idle: %v", err)
	}
	if err := pool.SendMessage("idle", sdk.Message{Role: sdk.RoleUser, Content: "queued"}); err != nil {
		t.Fatalf("SendMessage idle: %v", err)
	}
	a.Submit(context.Background(), "work")
	select {
	case <-lm.started:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for agent to run")
	}
	defer a.Cancel()

	bridge := &harnessAgentBridge{pool: pool}
	infos, err := bridge.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(infos) != 2 {
		t.Fatalf("List returned %d agents, want 2", len(infos))
	}
	byID := make(map[string]extension.AgentInfo, len(infos))
	for _, info := range infos {
		byID[info.ID] = info
	}
	worker := byID["worker"]
	if worker.ID != "worker" {
		t.Fatal("worker not found in List result")
	}
	if worker.Name != "Worker" {
		t.Errorf("Name = %q, want Worker", worker.Name)
	}
	if !worker.IsRunning {
		t.Error("IsRunning = false, want true for running agent")
	}
	if !worker.Working {
		t.Error("Working = false, want true for running agent")
	}
	if worker.Liveness != "working" {
		t.Errorf("Liveness = %q, want working", worker.Liveness)
	}
	if worker.TurnDurationMS < 0 {
		t.Errorf("TurnDurationMS = %d, want non-negative", worker.TurnDurationMS)
	}
	if worker.LastActivityAgeMS < 0 {
		t.Errorf("LastActivityAgeMS = %d, want non-negative", worker.LastActivityAgeMS)
	}
	idle := byID["idle"]
	if idle.ID != "idle" {
		t.Fatal("idle not found in List result")
	}
	if idle.PendingMessages != 1 {
		t.Errorf("PendingMessages = %d, want 1", idle.PendingMessages)
	}
}
