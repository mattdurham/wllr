//go:build integration

// Integration tests for agent team communication using real LLM calls.
// Agents are given actual tools (exec, write_file, read_file) and real tasks.
// Requires ANTHROPIC_API_KEY — fails if not set.
//
// Run: ANTHROPIC_API_KEY=sk-ant-... go test -tags integration ./test/integration/teams/ -v -timeout 120s
package teams_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	fantasy "charm.land/fantasy"
	fantasyanthropicprovider "charm.land/fantasy/providers/anthropic"
	"github.com/mattdurham/wllr/agent"
	"github.com/mattdurham/wllr/extension"
	"github.com/mattdurham/wllr/harness"
	"github.com/mattdurham/wllr/sdk"
)

// ─── Setup ───────────────────────────────────────────────────────────────────

// testEnv holds a pool and extension host wired together so agents have tools.
type testEnv struct {
	pool *agent.AgentPool
	host *extension.Host
	lm   fantasy.LanguageModel
	ctx  context.Context
}

func newEnv(t *testing.T) *testEnv {
	t.Helper()
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Fatal("ANTHROPIC_API_KEY must be set to run integration tests")
	}

	prov, err := fantasyanthropicprovider.New(fantasyanthropicprovider.WithAPIKey(apiKey))
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}
	ctx := context.Background()
	lm, err := prov.LanguageModel(ctx, "claude-haiku-4-5-20251001")
	if err != nil {
		t.Fatalf("get language model: %v", err)
	}

	pool := agent.NewPool()
	pool.SetProvider(prov)
	pool.SetDefaultModelName("claude-haiku-4-5-20251001")

	// Build extension host and register native tools so agents can do real work.
	host := extension.NewHost(nil)

	host.RegisterNativeTool(sdk.Tool{
		Name:        "write_file",
		Description: "Write content to a file",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"content":{"type":"string"}},"required":["path","content"]}`),
	}, func(_ context.Context, input json.RawMessage) (string, bool) {
		var in struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		}
		if err := json.Unmarshal(input, &in); err != nil || in.Path == "" {
			return "path required", true
		}
		if err := os.WriteFile(in.Path, []byte(in.Content), 0o644); err != nil {
			return err.Error(), true
		}
		return "written " + in.Path, false
	})

	host.RegisterNativeTool(sdk.Tool{
		Name:        "read_file",
		Description: "Read the contents of a file",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`),
	}, func(_ context.Context, input json.RawMessage) (string, bool) {
		var in struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal(input, &in); err != nil || in.Path == "" {
			return "path required", true
		}
		b, err := os.ReadFile(in.Path)
		if err != nil {
			return err.Error(), true
		}
		return string(b), false
	})

	// Wire host tools into agent turns.
	host.OnAfterToolCall = func(callID, toolName, result string, isError bool) {}

	env := &testEnv{pool: pool, host: host, lm: lm, ctx: ctx}
	t.Cleanup(func() { host.Close(ctx) })
	return env
}

// spawn creates an agent with the env's tools wired in.
func (e *testEnv) spawn(t *testing.T, id, systemPrompt string) *agent.Agent {
	t.Helper()
	a, err := e.pool.Spawn(id, e.lm, agent.SpawnOpts{SystemPrompt: systemPrompt})
	if err != nil {
		t.Fatalf("Spawn %q: %v", id, err)
	}
	// Wire tools from the extension host into this agent.
	host := e.host
	a.SetToolsFn(func() []fantasy.AgentTool {
		return buildTools(host, id)
	})
	return a
}

// buildTools returns the host's registered tools as fantasy.AgentTool using
// the harness adapter so we don't duplicate the interface implementation.
func buildTools(host *extension.Host, agentID string) []fantasy.AgentTool {
	return harness.BuildFantasyTools(host, agentID, nil)
}

// run submits a message to an agent and waits for the response.
func (e *testEnv) run(t *testing.T, id, message string, timeout time.Duration) string {
	t.Helper()
	a := e.pool.Get(id)
	if a == nil {
		t.Fatalf("agent %q not found", id)
	}
	var mu sync.Mutex
	var sb strings.Builder
	done := make(chan error, 1)
	a.SetOnToken(func(tok string) { mu.Lock(); sb.WriteString(tok); mu.Unlock() })
	a.SetOnDone(func(e error) { done <- e })

	if err := e.pool.Send(id, message); err != nil {
		t.Fatalf("Send to %q: %v", id, err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("agent %q error: %v", id, err)
		}
	case <-time.After(timeout):
		t.Fatalf("timeout waiting for %q", id)
	}
	mu.Lock()
	defer mu.Unlock()
	return sb.String()
}

// ─── Tests ────────────────────────────────────────────────────────────────────

// TestTeam_AgentWritesFile verifies an agent can use the write_file tool to
// complete a real task — not just produce text.
func TestTeam_AgentWritesFile(t *testing.T) {
	env := newEnv(t)
	dir := t.TempDir()
	outPath := filepath.Join(dir, "result.txt")

	env.spawn(t, "writer", "You are a file writer. When given a task, use write_file immediately to complete it. Do not explain — just write the file.")

	team, err := env.pool.CreateTeam("write-test")
	if err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}
	if err := team.AddMember("writer"); err != nil {
		t.Fatalf("AddMember: %v", err)
	}

	resp := env.run(t, "writer",
		"Write exactly the text '42' to "+outPath+" using the write_file tool.",
		60*time.Second)
	t.Logf("Agent response: %s", resp)

	content, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("file not created: %v", err)
	}
	if !strings.Contains(strings.TrimSpace(string(content)), "42") {
		t.Errorf("file content %q does not contain '42'", string(content))
	}

	if err := env.pool.CloseTeam(env.ctx, "write-test"); err != nil {
		t.Fatalf("CloseTeam: %v", err)
	}
}

// TestTeam_TwoAgents_CoordinatedWork has a writer agent write a file, then a
// reader agent read it and report back — end-to-end cross-agent coordination.
func TestTeam_TwoAgents_CoordinatedWork(t *testing.T) {
	env := newEnv(t)
	dir := t.TempDir()
	sharedFile := filepath.Join(dir, "shared.txt")

	env.spawn(t, "writer",
		"You write files. Use write_file immediately when given a task. No explanations.")
	env.spawn(t, "reader",
		"You read files and summarise their content. Use read_file, then reply with what you found.")

	team, err := env.pool.CreateTeam("coordinated")
	if err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}
	for _, id := range []string{"writer", "reader"} {
		if err := team.AddMember(id); err != nil {
			t.Fatalf("AddMember %q: %v", id, err)
		}
	}

	// Step 1: writer creates the file.
	env.run(t, "writer",
		"Write 'the answer is 42' to "+sharedFile,
		60*time.Second)

	if _, err := os.Stat(sharedFile); err != nil {
		t.Fatalf("writer did not create file: %v", err)
	}
	t.Logf("Writer created file at %s", sharedFile)

	// Step 2: deliver the file path to the reader via inbox.
	env.pool.Get("reader").AppendInbox(sdk.Message{
		Role:    sdk.RoleUser,
		Content: "The writer just wrote a file at: " + sharedFile,
	})

	// Step 3: reader reads the file and reports.
	report := env.run(t, "reader",
		"Read the file the writer created and tell me what it contains.",
		60*time.Second)
	t.Logf("Reader report: %s", report)

	if !strings.Contains(strings.ToLower(report), "42") {
		t.Errorf("reader report %q does not mention '42' from the file", report)
	}

	if err := env.pool.CloseTeam(env.ctx, "coordinated"); err != nil {
		t.Fatalf("CloseTeam: %v", err)
	}
}

// TestTeam_ParallelAgents_WriteDistinctFiles verifies that two agents running
// in parallel each complete their own distinct tasks without interference.
func TestTeam_ParallelAgents_WriteDistinctFiles(t *testing.T) {
	env := newEnv(t)
	dir := t.TempDir()

	for _, id := range []string{"agent-a", "agent-b"} {
		env.spawn(t, id, "You write files. Use write_file immediately. No explanations.")
	}

	team, err := env.pool.CreateTeam("parallel")
	if err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}
	for _, id := range []string{"agent-a", "agent-b"} {
		if err := team.AddMember(id); err != nil {
			t.Fatalf("AddMember %q: %v", id, err)
		}
	}

	// Wire both agents' callbacks before submitting.
	var (
		wg    sync.WaitGroup
		errA  error
		errB  error
		doneA = make(chan error, 1)
		doneB = make(chan error, 1)
	)
	env.pool.Get("agent-a").SetOnDone(func(e error) { doneA <- e })
	env.pool.Get("agent-b").SetOnDone(func(e error) { doneB <- e })

	pathA := filepath.Join(dir, "a.txt")
	pathB := filepath.Join(dir, "b.txt")

	wg.Add(2)
	go func() {
		defer wg.Done()
		if err := env.pool.Send("agent-a", "Write 'from-a' to "+pathA); err != nil {
			errA = err
		}
	}()
	go func() {
		defer wg.Done()
		if err := env.pool.Send("agent-b", "Write 'from-b' to "+pathB); err != nil {
			errB = err
		}
	}()
	wg.Wait()

	if errA != nil {
		t.Fatalf("Send to agent-a: %v", errA)
	}
	if errB != nil {
		t.Fatalf("Send to agent-b: %v", errB)
	}

	timeout := time.After(60 * time.Second)
	for i := 0; i < 2; i++ {
		select {
		case e := <-doneA:
			if e != nil {
				t.Errorf("agent-a error: %v", e)
			}
		case e := <-doneB:
			if e != nil {
				t.Errorf("agent-b error: %v", e)
			}
		case <-timeout:
			t.Fatal("timeout waiting for parallel agents")
		}
	}

	// Verify each agent wrote its own distinct file.
	contentA, err := os.ReadFile(pathA)
	if err != nil {
		t.Fatalf("agent-a did not write file: %v", err)
	}
	contentB, err := os.ReadFile(pathB)
	if err != nil {
		t.Fatalf("agent-b did not write file: %v", err)
	}

	t.Logf("agent-a wrote: %s", contentA)
	t.Logf("agent-b wrote: %s", contentB)

	if strings.Contains(string(contentA), "from-b") {
		t.Error("agent-a file contains agent-b content — cross-contamination")
	}
	if strings.Contains(string(contentB), "from-a") {
		t.Error("agent-b file contains agent-a content — cross-contamination")
	}

	if err := env.pool.CloseTeam(env.ctx, "parallel"); err != nil {
		t.Fatalf("CloseTeam: %v", err)
	}
}
