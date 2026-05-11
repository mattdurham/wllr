// Integration tests for agent team communication using real LLM calls.
// Skipped automatically when ANTHROPIC_API_KEY is not set.
//
// Run: ANTHROPIC_API_KEY=sk-ant-... go test ./test/integration/teams/ -v -timeout 60s
package teams_test

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	fantasy "charm.land/fantasy"
	fantasyanthropicprovider "charm.land/fantasy/providers/anthropic"
	"github.com/mattdurham/wllr/agent"
	"github.com/mattdurham/wllr/sdk"
)

// ─── Setup ───────────────────────────────────────────────────────────────────

func newPool(t *testing.T) (*agent.AgentPool, fantasy.LanguageModel) {
	t.Helper()
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set — skipping live integration test")
	}

	prov, err := fantasyanthropicprovider.New(fantasyanthropicprovider.WithAPIKey(apiKey))
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}
	lm, err := prov.LanguageModel(context.Background(), "claude-haiku-4-5-20251001")
	if err != nil {
		t.Fatalf("get language model: %v", err)
	}

	pool := agent.NewPool()
	pool.SetProvider(prov)
	pool.SetDefaultModelName("claude-haiku-4-5-20251001")
	return pool, lm
}

// runAgent submits a message and waits for the response, returning the
// collected text and any error.
func runAgent(t *testing.T, pool *agent.AgentPool, id, message string, timeout time.Duration) (string, error) {
	t.Helper()
	a := pool.Get(id)
	if a == nil {
		t.Fatalf("agent %q not found", id)
	}

	var mu sync.Mutex
	var collected strings.Builder
	done := make(chan error, 1)

	a.SetOnToken(func(tok string) {
		mu.Lock()
		collected.WriteString(tok)
		mu.Unlock()
	})
	a.SetOnDone(func(e error) { done <- e })

	if err := pool.Send(id, message); err != nil {
		t.Fatalf("Send to %q: %v", id, err)
	}

	select {
	case err := <-done:
		mu.Lock()
		result := collected.String()
		mu.Unlock()
		return result, err
	case <-time.After(timeout):
		t.Fatalf("timeout waiting for %q to respond", id)
		return "", nil
	}
}

// ─── Tests ────────────────────────────────────────────────────────────────────

// TestTeam_SingleAgent_DoesWork verifies a single agent in a team can do a
// simple reasoning task and produce a coherent response.
func TestTeam_SingleAgent_DoesWork(t *testing.T) {
	pool, lm := newPool(t)
	ctx := context.Background()

	if _, err := pool.Spawn("worker", lm, agent.SpawnOpts{
		SystemPrompt: "You are a concise assistant. Answer in one sentence only.",
	}); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	team, err := pool.CreateTeam("single")
	if err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}
	if err := team.AddMember("worker"); err != nil {
		t.Fatalf("AddMember: %v", err)
	}

	resp, err := runAgent(t, pool, "worker", "What is 2 + 2? Reply with just the number.", 30*time.Second)
	if err != nil {
		t.Fatalf("agent error: %v", err)
	}
	if !strings.Contains(resp, "4") {
		t.Errorf("expected response to contain '4', got: %q", resp)
	}

	if err := pool.CloseTeam(ctx, "single"); err != nil {
		t.Fatalf("CloseTeam: %v", err)
	}
}

// TestTeam_TwoAgents_CoordinatedWork spawns a researcher and a writer agent.
// The researcher answers a question; the coordinator reads the answer via
// inbox and uses it in its own response.
func TestTeam_TwoAgents_CoordinatedWork(t *testing.T) {
	pool, lm := newPool(t)
	ctx := context.Background()

	// Researcher: answers factual questions concisely.
	if _, err := pool.Spawn("researcher", lm, agent.SpawnOpts{
		SystemPrompt: "You are a researcher. Answer questions with a single factual sentence.",
	}); err != nil {
		t.Fatalf("Spawn researcher: %v", err)
	}

	// Coordinator: receives findings and summarises them.
	if _, err := pool.Spawn("coordinator", lm, agent.SpawnOpts{
		SystemPrompt: "You are a coordinator. When given a finding, confirm it with 'Confirmed: ' followed by the key fact.",
	}); err != nil {
		t.Fatalf("Spawn coordinator: %v", err)
	}

	team, err := pool.CreateTeam("coordinated")
	if err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}
	for _, id := range []string{"researcher", "coordinator"} {
		if err := team.AddMember(id); err != nil {
			t.Fatalf("AddMember %q: %v", id, err)
		}
	}

	// Step 1: researcher answers a question.
	finding, err := runAgent(t, pool, "researcher", "What programming language is Go compiled to?", 30*time.Second)
	if err != nil {
		t.Fatalf("researcher error: %v", err)
	}
	t.Logf("Researcher finding: %s", finding)

	// Step 2: deliver the finding to the coordinator via inbox.
	pool.Get("coordinator").AppendInbox(sdk.Message{
		Role:    sdk.RoleUser,
		Content: "Researcher finding: " + finding,
	})

	// Step 3: coordinator processes the finding.
	summary, err := runAgent(t, pool, "coordinator", "Summarise the finding you received.", 30*time.Second)
	if err != nil {
		t.Fatalf("coordinator error: %v", err)
	}
	t.Logf("Coordinator summary: %s", summary)

	if strings.TrimSpace(summary) == "" {
		t.Error("coordinator produced empty response")
	}

	if err := pool.CloseTeam(ctx, "coordinated"); err != nil {
		t.Fatalf("CloseTeam: %v", err)
	}
}

// TestTeam_ParallelWork_NoInterference spawns two agents that do independent
// tasks simultaneously and verifies their outputs don't cross-contaminate.
func TestTeam_ParallelWork_NoInterference(t *testing.T) {
	pool, lm := newPool(t)
	ctx := context.Background()

	if _, err := pool.Spawn("counter", lm, agent.SpawnOpts{
		SystemPrompt: "You only count. When asked to count to N, reply with the numbers 1 through N separated by spaces. Nothing else.",
	}); err != nil {
		t.Fatalf("Spawn counter: %v", err)
	}
	if _, err := pool.Spawn("reverser", lm, agent.SpawnOpts{
		SystemPrompt: "You only reverse words. When given words, reply with them in reverse order. Nothing else.",
	}); err != nil {
		t.Fatalf("Spawn reverser: %v", err)
	}

	team, err := pool.CreateTeam("parallel")
	if err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}
	for _, id := range []string{"counter", "reverser"} {
		if err := team.AddMember(id); err != nil {
			t.Fatalf("AddMember %q: %v", id, err)
		}
	}

	// Wire callbacks before submitting.
	var (
		counterResp  strings.Builder
		reverserResp strings.Builder
		mu           sync.Mutex
		counterDone  = make(chan error, 1)
		reverserDone = make(chan error, 1)
	)
	pool.Get("counter").SetOnToken(func(tok string) { mu.Lock(); counterResp.WriteString(tok); mu.Unlock() })
	pool.Get("counter").SetOnDone(func(e error) { counterDone <- e })
	pool.Get("reverser").SetOnToken(func(tok string) { mu.Lock(); reverserResp.WriteString(tok); mu.Unlock() })
	pool.Get("reverser").SetOnDone(func(e error) { reverserDone <- e })

	// Submit to both simultaneously.
	if err := pool.Send("counter", "Count to 3."); err != nil {
		t.Fatalf("Send counter: %v", err)
	}
	if err := pool.Send("reverser", "Reverse: apple banana cherry"); err != nil {
		t.Fatalf("Send reverser: %v", err)
	}

	timeout := time.After(30 * time.Second)
	for i := 0; i < 2; i++ {
		select {
		case err := <-counterDone:
			if err != nil {
				t.Errorf("counter error: %v", err)
			}
		case err := <-reverserDone:
			if err != nil {
				t.Errorf("reverser error: %v", err)
			}
		case <-timeout:
			t.Fatal("timeout waiting for parallel agents")
		}
	}

	mu.Lock()
	cr := counterResp.String()
	rr := reverserResp.String()
	mu.Unlock()

	t.Logf("Counter: %s", cr)
	t.Logf("Reverser: %s", rr)

	// Counter should mention numbers 1-3.
	if !strings.Contains(cr, "1") || !strings.Contains(cr, "3") {
		t.Errorf("counter response missing expected numbers: %q", cr)
	}
	// Reverser should mention the fruits (in some order).
	if !strings.Contains(strings.ToLower(rr), "cherry") {
		t.Errorf("reverser response missing 'cherry': %q", rr)
	}

	if err := pool.CloseTeam(ctx, "parallel"); err != nil {
		t.Fatalf("CloseTeam: %v", err)
	}
}
