// Package agentcoord_test exercises the agent coordination pattern:
// orchestrator spawns a worker, worker claims and completes tasks, sends
// IDLE signal, orchestrator receives it and reacts. No real LLM API calls.
//
// Run: go test ./test/coordination/ -v -timeout 15s
package agentcoord_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"charm.land/fantasy"
	"github.com/mattdurham/wllr/modules/agent"
	"github.com/mattdurham/wllr/modules/extension"
	"github.com/mattdurham/wllr/modules/sdk"
	"github.com/mattdurham/wllr/modules/testutil"
	"github.com/mattdurham/wllr/modules/tools"
)

// ─── In-process task store ────────────────────────────────────────────────────

// taskStore is a minimal in-memory task store for the native task tools.
type taskStore struct {
	mu      sync.Mutex
	lists   map[string]*taskList
	listSeq int
	taskSeq int
}

type taskList struct {
	id    string
	tasks map[string]*task
}

type task struct {
	id     string
	title  string
	status string
}

func newTaskStore() *taskStore {
	return &taskStore{lists: make(map[string]*taskList)}
}

func (s *taskStore) createList(name string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.listSeq++
	id := fmt.Sprintf("list-%d", s.listSeq)
	s.lists[id] = &taskList{id: id, tasks: make(map[string]*task)}
	_ = name
	return id
}

func (s *taskStore) createTask(listID, title string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	l, ok := s.lists[listID]
	if !ok {
		return "", fmt.Errorf("list %q not found", listID)
	}
	s.taskSeq++
	id := fmt.Sprintf("task-%d", s.taskSeq)
	l.tasks[id] = &task{id: id, title: title, status: "pending"}
	return id, nil
}

func (s *taskStore) updateTask(listID, taskID, status string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	l, ok := s.lists[listID]
	if !ok {
		return fmt.Errorf("list %q not found", listID)
	}
	t, ok := l.tasks[taskID]
	if !ok {
		return fmt.Errorf("task %q not found", taskID)
	}
	t.status = status
	return nil
}

func (s *taskStore) listByStatus(listID, statusFilter string) ([]*task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	l, ok := s.lists[listID]
	if !ok {
		return nil, fmt.Errorf("list %q not found", listID)
	}
	var out []*task
	for _, t := range l.tasks {
		if statusFilter == "" || t.status == statusFilter {
			out = append(out, t)
		}
	}
	return out, nil
}

// ─── Coordination environment ─────────────────────────────────────────────────

// coordEnv holds the shared pool, host, and task store.
type coordEnv struct {
	pool  *agent.AgentPool
	host  *extension.Host
	store *taskStore
	ctx   context.Context
}

func newCoordEnv(t *testing.T) *coordEnv {
	t.Helper()
	ctx := context.Background()
	pool := agent.NewPool()
	store := newTaskStore()
	host := extension.NewHost(nil)

	host.SetAgentBridge(&poolAgentBridge{pool: pool, ctx: ctx})

	registerTaskTools(t, host, store)
	registerSendMessageTool(t, host, pool)

	t.Cleanup(func() { host.Close(ctx) })
	return &coordEnv{pool: pool, host: host, store: store, ctx: ctx}
}

// spawn creates an agent with the given FakeLM and wires host tools.
func (e *coordEnv) spawn(t *testing.T, id string, lm *testutil.FakeLM) *agent.Agent {
	t.Helper()
	f := false
	a, err := e.pool.Spawn(id, lm, agent.SpawnOpts{
		SystemPrompt:      "Test agent.",
		InheritBasePrompt: &f,
		TurnTimeout:       -1,
	})
	if err != nil {
		t.Fatalf("Spawn %q: %v", id, err)
	}
	h := e.host
	a.SetToolsFn(func() []fantasy.AgentTool {
		return tools.BuildFantasyTools(h, id, nil)
	})
	return a
}

// ─── Native tool registrations ────────────────────────────────────────────────

func registerTaskTools(_ *testing.T, host *extension.Host, store *taskStore) {
	host.RegisterNativeTool(sdk.Tool{
		Name:        "tasklist_create",
		Description: "Create a task list",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"}},"required":["name"]}`),
	}, func(_ context.Context, input json.RawMessage) (string, bool) {
		var in struct{ Name string `json:"name"` }
		if err := json.Unmarshal(input, &in); err != nil {
			return "bad input: " + err.Error(), true
		}
		id := store.createList(in.Name)
		b, _ := json.Marshal(map[string]string{"list_id": id})
		return string(b), false
	})

	host.RegisterNativeTool(sdk.Tool{
		Name:        "tasks_create",
		Description: "Create a task",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"list_id":{"type":"string"},"title":{"type":"string"}},"required":["list_id","title"]}`),
	}, func(_ context.Context, input json.RawMessage) (string, bool) {
		var in struct {
			ListID string `json:"list_id"`
			Title  string `json:"title"`
		}
		if err := json.Unmarshal(input, &in); err != nil {
			return "bad input: " + err.Error(), true
		}
		id, err := store.createTask(in.ListID, in.Title)
		if err != nil {
			return err.Error(), true
		}
		b, _ := json.Marshal(map[string]string{"task_id": id})
		return string(b), false
	})

	host.RegisterNativeTool(sdk.Tool{
		Name:        "tasks_update",
		Description: "Update task status",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"list_id":{"type":"string"},"task_id":{"type":"string"},"status":{"type":"string"}},"required":["list_id","task_id","status"]}`),
	}, func(_ context.Context, input json.RawMessage) (string, bool) {
		var in struct {
			ListID string `json:"list_id"`
			TaskID string `json:"task_id"`
			Status string `json:"status"`
		}
		if err := json.Unmarshal(input, &in); err != nil {
			return "bad input: " + err.Error(), true
		}
		if err := store.updateTask(in.ListID, in.TaskID, in.Status); err != nil {
			return err.Error(), true
		}
		b, _ := json.Marshal(map[string]bool{"success": true})
		return string(b), false
	})

	host.RegisterNativeTool(sdk.Tool{
		Name:        "tasks_list",
		Description: "List tasks, optionally filtered by status",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"list_id":{"type":"string"},"status":{"type":"string"}},"required":["list_id"]}`),
	}, func(_ context.Context, input json.RawMessage) (string, bool) {
		var in struct {
			ListID string `json:"list_id"`
			Status string `json:"status"`
		}
		if err := json.Unmarshal(input, &in); err != nil {
			return "bad input: " + err.Error(), true
		}
		tasks, err := store.listByStatus(in.ListID, in.Status)
		if err != nil {
			return err.Error(), true
		}
		type tj struct {
			ID     string `json:"id"`
			Title  string `json:"title"`
			Status string `json:"status"`
		}
		out := make([]tj, len(tasks))
		for i, t := range tasks {
			out[i] = tj{ID: t.id, Title: t.title, Status: t.status}
		}
		b, _ := json.Marshal(map[string]interface{}{"tasks": out})
		return string(b), false
	})
}

func registerSendMessageTool(_ *testing.T, host *extension.Host, pool *agent.AgentPool) {
	host.RegisterNativeTool(sdk.Tool{
		Name:        "send_message",
		Description: "Send a message to another agent",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"agent_id":{"type":"string"},"message":{"type":"string"}},"required":["agent_id","message"]}`),
	}, func(_ context.Context, input json.RawMessage) (string, bool) {
		var in struct {
			AgentID string `json:"agent_id"`
			Message string `json:"message"`
		}
		if err := json.Unmarshal(input, &in); err != nil {
			return "bad input: " + err.Error(), true
		}
		// Prefix with agent label matching the real agents extension protocol.
		content := "[from agent 'worker']: " + in.Message
		if err := pool.SendMessage(in.AgentID, sdk.Message{
			Role:    sdk.RoleUser,
			Content: content,
		}); err != nil {
			return fmt.Sprintf("send_message: %v", err), true
		}
		b, _ := json.Marshal(map[string]bool{"ok": true})
		return string(b), false
	})
}

// ─── poolAgentBridge adapts AgentPool to extension.AgentBridge ──────────────

type poolAgentBridge struct {
	pool *agent.AgentPool
	ctx  context.Context
}

func (b *poolAgentBridge) Spawn(_ context.Context, req extension.SpawnRequest) error {
	lm := testutil.NewFakeLMWithResponses("ok")
	_, err := b.pool.Spawn(req.ID, lm, agent.SpawnOpts{SystemPrompt: req.SystemPrompt})
	return err
}
func (b *poolAgentBridge) Close(id string) error { return b.pool.Close(id) }
func (b *poolAgentBridge) SendMessage(id, message string) error {
	return b.pool.SendMessage(id, sdk.Message{Role: sdk.RoleUser, Content: message})
}
func (b *poolAgentBridge) Run(id string) error {
	a := b.pool.Get(id)
	if a == nil {
		return fmt.Errorf("agent %q not found", id)
	}
	a.Submit(b.ctx, "")
	return nil
}
func (b *poolAgentBridge) List() ([]extension.AgentInfo, error) {
	ids := b.pool.ListAgents()
	out := make([]extension.AgentInfo, len(ids))
	for i, id := range ids {
		out[i] = extension.AgentInfo{ID: id}
	}
	return out, nil
}
func (b *poolAgentBridge) TokenCount() int64              { return b.pool.TokenCount() }
func (b *poolAgentBridge) SetHistory(_ string, _ []sdk.Message) error { return nil }
func (b *poolAgentBridge) WaitForAll(_ string, _ []string, _ int) (extension.WaitResult, error) {
	return extension.WaitResult{Status: "ok"}, nil
}

// ─── Tests ────────────────────────────────────────────────────────────────────

// TestAgentCoordination_WorkerCompletesTasksAndSignalsIdle verifies the
// orchestrator→worker→IDLE coordination pattern using scripted FakeLM responses
// and native task tools. No WASM, no real LLM API calls.
//
// The test uses a real-world simulation where:
//  1. The orchestrator runs turn 1 (text only, ends its turn).
//  2. The worker runs turn 1, calls task tools, then calls send_message which
//     delivers an IDLE signal into the orchestrator's inbox.
//  3. The test detects the inbox message and explicitly triggers orchestrator
//     turn 2 (simulating what the harness does in production via its event loop).
//  4. Orchestrator turn 2 processes the IDLE signal.
//
// Note: In production the harness's event loop monitors the inbox and triggers
// new turns automatically. In unit tests we simulate this explicitly.
func TestAgentCoordination_WorkerCompletesTasksAndSignalsIdle(t *testing.T) {
	env := newCoordEnv(t)

	// Pre-populate task list.
	listID := env.store.createList("work")
	taskAID, err := env.store.createTask(listID, "Task A")
	if err != nil {
		t.Fatalf("createTask A: %v", err)
	}
	taskBID, err := env.store.createTask(listID, "Task B")
	if err != nil {
		t.Fatalf("createTask B: %v", err)
	}
	t.Logf("list=%s taskA=%s taskB=%s", listID, taskAID, taskBID)

	marshalInput := func(v any) json.RawMessage {
		b, _ := json.Marshal(v)
		return b
	}
	listPendingInput := marshalInput(map[string]string{"list_id": listID, "status": "pending"})
	updateAInProgress := marshalInput(map[string]string{"list_id": listID, "task_id": taskAID, "status": "in_progress"})
	updateACompleted := marshalInput(map[string]string{"list_id": listID, "task_id": taskAID, "status": "completed"})
	updateBInProgress := marshalInput(map[string]string{"list_id": listID, "task_id": taskBID, "status": "in_progress"})
	updateBCompleted := marshalInput(map[string]string{"list_id": listID, "task_id": taskBID, "status": "completed"})
	sendIdleInput := marshalInput(map[string]string{"agent_id": "main", "message": "IDLE: all done"})

	// Orchestrator: two text-only turns (no tools needed in this test).
	orchLM := testutil.NewFakeLM()
	orchLM.SetScript([]testutil.ScriptedTurn{
		{Text: "Waiting for worker."},
		{Text: "Got IDLE signal, wrapping up."},
	})

	// Worker: claims both tasks, completes them, sends IDLE.
	workerLM := testutil.NewFakeLM()
	workerLM.SetScript([]testutil.ScriptedTurn{
		{
			ToolCalls: []testutil.ScriptedToolCall{
				{ID: "tl1", Name: "tasks_list", Input: listPendingInput},
				{ID: "tu1", Name: "tasks_update", Input: updateAInProgress},
				{ID: "tu2", Name: "tasks_update", Input: updateACompleted},
				{ID: "tu3", Name: "tasks_update", Input: updateBInProgress},
				{ID: "tu4", Name: "tasks_update", Input: updateBCompleted},
				{ID: "tl2", Name: "tasks_list", Input: listPendingInput},
				{ID: "sm1", Name: "send_message", Input: sendIdleInput},
			},
		},
	})

	orch := env.spawn(t, "main", orchLM)
	worker := env.spawn(t, "worker", workerLM)

	// Run orchestrator turn 1.
	orchDone := make(chan error, 2) // buffered for turn1 + turn2
	orch.SetOnDone(func(e error) { orchDone <- e })
	orch.Submit(env.ctx, "Start coordination.")
	select {
	case err := <-orchDone:
		if err != nil {
			t.Fatalf("orchestrator turn 1 error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout: orchestrator turn 1")
	}
	t.Log("Orchestrator turn 1 complete")

	// Run worker turn: tool calls execute, IDLE message lands in orch inbox.
	workerDone := make(chan error, 1)
	worker.SetOnDone(func(e error) { workerDone <- e })
	worker.Submit(env.ctx, "Process tasks.")
	select {
	case err := <-workerDone:
		if err != nil {
			t.Fatalf("worker turn error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout: worker turn")
	}
	t.Log("Worker turn complete")

	// Verify IDLE message was delivered to orchestrator inbox.
	// Drain inbox to inspect.
	inbox := orch.DrainInbox()
	var idleMsg *sdk.Message
	for _, m := range inbox {
		if strings.Contains(m.Content, "IDLE:") {
			mc := m
			idleMsg = &mc
			break
		}
	}
	if idleMsg == nil {
		t.Fatal("orchestrator inbox does not contain IDLE message after worker completes")
	}
	t.Logf("IDLE message in orchestrator inbox: %q", idleMsg.Content)

	// Restore inbox messages (re-append them so turn 2 sees them).
	for _, m := range inbox {
		orch.AppendInbox(m)
	}

	// Trigger orchestrator turn 2 by submitting with empty content.
	// In production, the harness event loop does this automatically.
	orch.SetOnDone(func(e error) { orchDone <- e })
	orch.Submit(env.ctx, "")
	select {
	case err := <-orchDone:
		if err != nil {
			t.Fatalf("orchestrator turn 2 error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout: orchestrator turn 2 (IDLE wakeup)")
	}
	t.Log("Orchestrator turn 2 complete")

	// Verify: both tasks are completed.
	completedTasks, err := env.store.listByStatus(listID, "completed")
	if err != nil {
		t.Fatalf("listByStatus: %v", err)
	}
	if len(completedTasks) != 2 {
		t.Errorf("expected 2 completed tasks, got %d", len(completedTasks))
	}

	// Verify: orchestrator history contains the IDLE message.
	history := orch.History()
	var foundIdle bool
	for _, msg := range history {
		if strings.Contains(msg.Content, "IDLE:") {
			foundIdle = true
			t.Logf("IDLE in orchestrator history: %q", msg.Content)
			break
		}
	}
	if !foundIdle {
		t.Errorf("orchestrator history does not contain IDLE message; history: %+v", history)
	}

	// Verify: no real API calls (FakeLM only).
	if n := len(workerLM.Calls()); n < 1 {
		t.Errorf("workerLM: expected ≥1 recorded call, got %d", n)
	}
	if n := len(orchLM.Calls()); n < 2 {
		t.Errorf("orchLM: expected ≥2 recorded calls (2 turns), got %d", n)
	}
}

// TestAgentCoordination_EmptyTaskList_IdleSignaledImmediately verifies that a
// worker finding no pending tasks immediately sends the IDLE signal.
func TestAgentCoordination_EmptyTaskList_IdleSignaledImmediately(t *testing.T) {
	env := newCoordEnv(t)
	listID := env.store.createList("empty-work")

	marshalInput := func(v any) json.RawMessage {
		b, _ := json.Marshal(v)
		return b
	}
	listEmptyInput := marshalInput(map[string]string{"list_id": listID, "status": "pending"})
	sendIdleInput := marshalInput(map[string]string{"agent_id": "main", "message": "IDLE: no pending tasks"})

	orchLM := testutil.NewFakeLM()
	orchLM.SetScript([]testutil.ScriptedTurn{
		{Text: "Waiting."},
		{Text: "Got IDLE, nothing to do."},
	})

	workerLM := testutil.NewFakeLM()
	workerLM.SetScript([]testutil.ScriptedTurn{
		{
			ToolCalls: []testutil.ScriptedToolCall{
				{ID: "tl1", Name: "tasks_list", Input: listEmptyInput},
				{ID: "sm1", Name: "send_message", Input: sendIdleInput},
			},
		},
	})

	orch := env.spawn(t, "main", orchLM)
	worker := env.spawn(t, "worker", workerLM)

	// Orchestrator turn 1.
	orchDone := make(chan error, 2)
	orch.SetOnDone(func(e error) { orchDone <- e })
	orch.Submit(env.ctx, "Start.")
	select {
	case err := <-orchDone:
		if err != nil {
			t.Fatalf("orch turn 1 error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout: orch turn 1")
	}

	// Worker turn.
	workerDone := make(chan error, 1)
	worker.SetOnDone(func(e error) { workerDone <- e })
	worker.Submit(env.ctx, "Check tasks.")
	select {
	case err := <-workerDone:
		if err != nil {
			t.Fatalf("worker error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout: worker turn")
	}

	// Verify IDLE delivered and re-append for turn 2.
	inbox := orch.DrainInbox()
	var found bool
	for _, m := range inbox {
		if strings.Contains(m.Content, "IDLE:") {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("orchestrator inbox missing IDLE message")
	}
	for _, m := range inbox {
		orch.AppendInbox(m)
	}

	// Orchestrator turn 2.
	orch.SetOnDone(func(e error) { orchDone <- e })
	orch.Submit(env.ctx, "")
	select {
	case err := <-orchDone:
		if err != nil {
			t.Fatalf("orch turn 2 error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout: orch turn 2")
	}

	// Verify IDLE in history.
	history := orch.History()
	var foundInHistory bool
	for _, msg := range history {
		if strings.Contains(msg.Content, "IDLE:") {
			foundInHistory = true
			break
		}
	}
	if !foundInHistory {
		t.Errorf("IDLE not in orchestrator history; history: %+v", history)
	}
}

// TestAgentCoordination_WorkerToolCallsExecuted verifies that scripted FakeLM
// tool calls are actually executed (not just emitted): tasks_update calls
// change the in-process task store state.
func TestAgentCoordination_WorkerToolCallsExecuted(t *testing.T) {
	env := newCoordEnv(t)
	listID := env.store.createList("exec-work")
	taskID, err := env.store.createTask(listID, "Executable Task")
	if err != nil {
		t.Fatalf("createTask: %v", err)
	}

	marshalInput := func(v any) json.RawMessage {
		b, _ := json.Marshal(v)
		return b
	}
	claimInput := marshalInput(map[string]string{"list_id": listID, "task_id": taskID, "status": "in_progress"})
	completeInput := marshalInput(map[string]string{"list_id": listID, "task_id": taskID, "status": "completed"})
	// Use send_message to "main" but main doesn't exist yet — that's OK,
	// send_message will fail. Use a text-only turn instead for the second call.

	workerLM := testutil.NewFakeLM()
	workerLM.SetScript([]testutil.ScriptedTurn{
		{
			ToolCalls: []testutil.ScriptedToolCall{
				{ID: "tu1", Name: "tasks_update", Input: claimInput},
				{ID: "tu2", Name: "tasks_update", Input: completeInput},
			},
		},
	})

	// Spawn only the worker (no orchestrator needed for this test).
	worker := env.spawn(t, "worker", workerLM)

	workerDone := make(chan error, 1)
	worker.SetOnDone(func(e error) { workerDone <- e })
	worker.Submit(env.ctx, "Process.")
	select {
	case err := <-workerDone:
		if err != nil {
			t.Fatalf("worker error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout: worker")
	}

	// Verify task was updated to completed.
	tasks, err := env.store.listByStatus(listID, "completed")
	if err != nil {
		t.Fatalf("listByStatus: %v", err)
	}
	if len(tasks) != 1 || tasks[0].id != taskID {
		t.Errorf("expected task %q completed; completed tasks: %+v", taskID, tasks)
	}

	// Verify two tool calls were made and recorded by FakeLM.
	calls := workerLM.Calls()
	if len(calls) < 1 {
		t.Errorf("workerLM: expected ≥1 call, got %d", len(calls))
	}
}
