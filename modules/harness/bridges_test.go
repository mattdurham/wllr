package harness

import (
	"testing"

	"github.com/mattdurham/wllr/modules/agent"
	"github.com/mattdurham/wllr/modules/sdk"
)

func TestHarnessAgentBridgeListIncludesRuntimeState(t *testing.T) {
	pool := agent.NewPool()
	if _, err := pool.Spawn("worker", newMockLM("ok"), agent.SpawnOpts{Name: "Worker"}); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if err := pool.SendMessage("worker", sdk.Message{Role: sdk.RoleUser, Content: "queued"}); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}

	bridge := &harnessAgentBridge{pool: pool}
	infos, err := bridge.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(infos) != 1 {
		t.Fatalf("List returned %d agents, want 1", len(infos))
	}
	if infos[0].ID != "worker" {
		t.Errorf("ID = %q, want worker", infos[0].ID)
	}
	if infos[0].Name != "Worker" {
		t.Errorf("Name = %q, want Worker", infos[0].Name)
	}
	if infos[0].IsRunning {
		t.Error("IsRunning = true, want false for idle agent")
	}
	if infos[0].PendingMessages != 1 {
		t.Errorf("PendingMessages = %d, want 1", infos[0].PendingMessages)
	}
}
