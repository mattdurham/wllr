package agent_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/mattdurham/wllr/modules/agent"
	"github.com/mattdurham/wllr/modules/extension"
	"github.com/mattdurham/wllr/modules/sdk"
	"github.com/mattdurham/wllr/modules/testutil"
)

func TestSpawner_Spawn_Basic(t *testing.T) {
	prov := testutil.NewFakeProvider()
	pool := agent.NewPool()
	pool.SetProvider(prov)
	pool.SetDefaultModelName("fake-model")

	// Spawn the main agent first so the pool is initialised.
	lm, err := pool.LanguageModelForModel(context.Background(), "fake-model")
	if err != nil {
		t.Fatalf("LanguageModelForModel: %v", err)
	}
	_, err = pool.Spawn("main", lm, agent.SpawnOpts{SystemPrompt: "main agent"})
	if err != nil {
		t.Fatalf("Spawn main: %v", err)
	}

	spawner := agent.NewSpawner(pool, nil, nil)

	err = spawner.Spawn(context.Background(), extension.SpawnRequest{
		ID:           "main/coder",
		Name:         "Coder",
		SystemPrompt: "you are a coder",
		ModelName:    "fake-model",
	})
	if err != nil {
		t.Fatalf("Spawn sub-agent: %v", err)
	}

	a := pool.Get("main/coder")
	if a == nil {
		t.Fatal("expected agent 'main/coder' to exist in pool")
	}
	if a.Name() != "Coder" {
		t.Errorf("expected name 'Coder', got %q", a.Name())
	}
}

func TestSpawner_Spawn_ParentIDConvention(t *testing.T) {
	// Verify that "main/coder" gets parentID="main" and "main/team/worker" gets parentID="main/team".
	prov := testutil.NewFakeProvider()
	pool := agent.NewPool()
	pool.SetProvider(prov)
	pool.SetDefaultModelName("fake-model")

	lm, _ := pool.LanguageModelForModel(context.Background(), "fake-model")
	_, _ = pool.Spawn("main", lm, agent.SpawnOpts{})

	spawner := agent.NewSpawner(pool, nil, nil)

	cases := []struct {
		id         string
		wantParent string
	}{
		{"main/coder", "main"},
		{"main/team/worker", "main/team"},
		{"toplevel", ""},
	}
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			_ = pool.Close(tc.id) // ensure clean state
			err := spawner.Spawn(context.Background(), extension.SpawnRequest{
				ID:        tc.id,
				ModelName: "fake-model",
			})
			if err != nil {
				t.Fatalf("Spawn %q: %v", tc.id, err)
			}
			a := pool.Get(tc.id)
			if a == nil {
				t.Fatalf("agent %q not found", tc.id)
			}
		})
	}
}

func TestSpawner_Spawn_AgentIdentitySuffix(t *testing.T) {
	prov := testutil.NewFakeProvider()
	pool := agent.NewPool()
	pool.SetProvider(prov)
	pool.SetDefaultModelName("fake-model")

	lm, _ := pool.LanguageModelForModel(context.Background(), "fake-model")
	_, _ = pool.Spawn("main", lm, agent.SpawnOpts{})

	spawner := agent.NewSpawner(pool, nil, nil)
	_ = spawner.Spawn(context.Background(), extension.SpawnRequest{
		ID:           "main/worker",
		SystemPrompt: "base prompt",
		ModelName:    "fake-model",
	})

	a := pool.Get("main/worker")
	if a == nil {
		t.Fatal("agent main/worker not in pool")
	}

	// Verify the agent-identity suffix was actually appended to the system prompt.
	sp := a.SystemPrompt()
	if !strings.Contains(sp, "main/worker") {
		t.Errorf("expected system prompt to contain agent ID 'main/worker'; got: %q", sp)
	}
	if !strings.Contains(sp, "base prompt") {
		t.Errorf("expected system prompt to contain 'base prompt'; got: %q", sp)
	}
	if !strings.Contains(sp, "## Your Agent Identity") {
		t.Errorf("expected system prompt to contain '## Your Agent Identity' suffix; got: %q", sp)
	}
}

func TestSpawner_Spawn_NoProvider(t *testing.T) {
	// With no provider set, LanguageModelForModel should fail.
	pool := agent.NewPool()
	// No SetProvider call — pool has no provider.

	spawner := agent.NewSpawner(pool, nil, nil)
	err := spawner.Spawn(context.Background(), extension.SpawnRequest{
		ID:        "main/coder",
		ModelName: "fake-model",
	})
	if err == nil {
		t.Fatal("expected error for pool with no provider, got nil")
	}
}

func TestSpawner_Spawn_NilPool(t *testing.T) {
	// Spawner with nil pool must return an error immediately without panicking.
	spawner := agent.NewSpawner(nil, nil, nil)
	err := spawner.Spawn(context.Background(), extension.SpawnRequest{
		ID:        "main/coder",
		ModelName: "fake-model",
	})
	if err == nil {
		t.Fatal("expected error for nil pool, got nil")
	}
}

func TestSpawner_Spawn_NilToolsFn(t *testing.T) {
	prov := testutil.NewFakeProvider()
	pool := agent.NewPool()
	pool.SetProvider(prov)
	pool.SetDefaultModelName("fake-model")

	lm, _ := pool.LanguageModelForModel(context.Background(), "fake-model")
	_, _ = pool.Spawn("main", lm, agent.SpawnOpts{})

	// nil toolsFn should not panic
	spawner := agent.NewSpawner(pool, nil, nil)
	err := spawner.Spawn(context.Background(), extension.SpawnRequest{
		ID:        "main/niltools",
		ModelName: "fake-model",
	})
	if err != nil {
		t.Fatalf("Spawn with nil toolsFn: %v", err)
	}
}

func TestSpawner_Spawn_CallerIDPropagated(t *testing.T) {
	prov := testutil.NewFakeProvider()
	pool := agent.NewPool()
	pool.SetProvider(prov)
	pool.SetDefaultModelName("fake-model")

	lm, _ := pool.LanguageModelForModel(context.Background(), "fake-model")
	_, _ = pool.Spawn("main", lm, agent.SpawnOpts{})

	spawner := agent.NewSpawner(pool, nil, nil)
	err := spawner.Spawn(context.Background(), extension.SpawnRequest{
		ID:        "main/worker",
		ModelName: "fake-model",
		CallerID:  "main",
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	a := pool.Get("main/worker")
	if a == nil {
		t.Fatal("expected agent 'main/worker' to exist in pool")
	}
	if got := a.CreatorID(); got != "main" {
		t.Errorf("CreatorID = %q, want %q", got, "main")
	}
}

func TestSpawner_FailureNotificationTargetsCreator(t *testing.T) {
	prov := testutil.NewFakeProvider()
	pool := agent.NewPool()
	pool.SetProvider(prov)
	pool.SetDefaultModelName("fake-model")
	lm, _ := pool.LanguageModelForModel(context.Background(), "fake-model")
	_, _ = pool.Spawn("main", lm, agent.SpawnOpts{})

	parent, err := pool.Spawn("main/parent", lm, agent.SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn parent: %v", err)
	}
	parentTurn := make(chan []sdk.Message, 1)
	parent.SetOnTurnStart(func(_ string, inbox []sdk.Message) { parentTurn <- inbox })

	spawner := agent.NewSpawner(pool, nil, nil)
	if err := spawner.Spawn(context.Background(), extension.SpawnRequest{
		ID: "main/parent/worker", Name: "worker", ModelName: "fake-model", CallerID: "main/parent",
	}); err != nil {
		t.Fatalf("Spawn worker: %v", err)
	}
	worker := pool.Get("main/parent/worker")
	worker.SetModel(&errStreamLM{}, "err-model")
	worker.Submit(context.Background(), "fail")

	select {
	case inbox := <-parentTurn:
		if len(inbox) != 1 {
			t.Fatalf("parent inbox = %+v, want one lifecycle notification", inbox)
		}
		var event struct {
			Event     string `json:"event"`
			AgentID   string `json:"agent_id"`
			CreatorID string `json:"creator_id"`
			Error     string `json:"error"`
		}
		if err := json.Unmarshal([]byte(inbox[0].Content), &event); err != nil {
			t.Fatalf("decode failure notification: %v", err)
		}
		if event.Event != "agent_failed" || event.AgentID != "main/parent/worker" || event.CreatorID != "main/parent" || event.Error == "" {
			t.Errorf("failure notification = %+v, want nested creator and error", event)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("parent was not woken by child failure")
	}
}
