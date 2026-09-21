//go:build wasip1

package main

import (
	"encoding/json"
	"fmt"
)

type runnerState struct {
	ListID  string `json:"list_id"`
	TaskID  string `json:"task_id"`
	AgentID string `json:"agent_id"`
	Phase   string `json:"phase"`
}

type task struct {
	TaskID          string `json:"task_id"`
	ListID          string `json:"list_id"`
	Title           string `json:"title"`
	Description     string `json:"description"`
	Status          string `json:"status"`
	AttemptID       string `json:"attempt_id"`
	AssigneeAgentID string `json:"assignee_agent_id"`
	Version         int64  `json:"version"`
}

var callerByCall = map[string]string{}

func call(method string, params any, out any) error {
	raw, hostErr := _sdkCallResultWithError(method, params)
	if hostErr != "" {
		return fmt.Errorf("%s: %s", method, hostErr)
	}
	if out != nil && json.Unmarshal(raw, out) != nil {
		return fmt.Errorf("%s: invalid host response", method)
	}
	return nil
}

func state() (runnerState, bool) {
	raw, ok := StoreGet("runner")
	if !ok {
		return runnerState{}, false
	}
	var s runnerState
	return s, json.Unmarshal([]byte(raw), &s) == nil && s.ListID != ""
}

func save(s runnerState) { b, _ := json.Marshal(s); StoreSet("runner", string(b)) }

func init() {
	RegisterToolWithOutput(
		"task_runner_create",
		"Create an ordered task list and start its first sub-agent.",
		json.RawMessage(
			`{"type":"object","properties":{"name":{"type":"string"},"description":{"type":"string"},"tasks":{"type":"array","items":{"type":"object","properties":{"title":{"type":"string"},"description":{"type":"string"}},"required":["title"]}}},"required":["name","tasks"]}`,
		),
		json.RawMessage(`{"type":"object"}`),
	)
	RegisterToolWithOutput(
		"task_runner_start",
		"Start an existing durable task list.",
		json.RawMessage(`{"type":"object","properties":{"list_id":{"type":"string"}},"required":["list_id"]}`),
		json.RawMessage(`{"type":"object"}`),
	)
	RegisterToolWithOutput(
		"mark_task_completed",
		"Complete the current assigned task with evidence.",
		json.RawMessage(
			`{"type":"object","properties":{"task_id":{"type":"string"},"result":{},"summary":{"type":"string"}},"required":["task_id","result"]}`,
		),
		json.RawMessage(`{"type":"object"}`),
	)
	RegisterToolWithOutput(
		"request_task_review",
		"Pause the current task and request human review in chat.",
		json.RawMessage(
			`{"type":"object","properties":{"task_id":{"type":"string"},"reason":{"type":"string"},"details":{"type":"string"}},"required":["task_id","reason"]}`,
		),
		json.RawMessage(`{"type":"object"}`),
	)
	RegisterToolWithOutput(
		"task_runner_status",
		"Show the ordered task runner state.",
		json.RawMessage(`{"type":"object"}`),
		json.RawMessage(`{"type":"object"}`),
	)
	RegisterCommand("task-runner", "Start, stop, or inspect an ordered task runner")
	OnBeforeToolCall(func(payload json.RawMessage) {
		var p struct{ AgentID, ToolCallID string }
		if json.Unmarshal(payload, &p) == nil && p.ToolCallID != "" {
			callerByCall[p.ToolCallID] = p.AgentID
		}
	})
	OnToolCall(handleTool)
	OnCommand("task-runner", handleCommand)
	OnTick(func() { advanceAfterShutdown() })
}

func handleTool(callID, name string, input json.RawMessage) (string, bool) {
	var p map[string]any
	if json.Unmarshal(input, &p) != nil {
		return name + ": invalid JSON", true
	}
	switch name {
	case "task_runner_create":
		return createAndStart(p)
	case "task_runner_start":
		listID, _ := p["list_id"].(string)
		return start(listID)
	case "mark_task_completed":
		return complete(callID, p)
	case "request_task_review":
		return review(callID, p)
	case "task_runner_status":
		s, ok := state()
		if !ok {
			return `{"phase":"idle"}`, false
		}
		b, _ := json.Marshal(s)
		return string(b), false
	default:
		return "", false
	}
}

func createAndStart(p map[string]any) (string, bool) {
	name, _ := p["name"].(string)
	if name == "" {
		return "name is required", true
	}
	var lists struct {
		List struct {
			ListID string `json:"list_id"`
		} `json:"list"`
	}
	if err := call("tasklist_create", map[string]any{"name": name, "description": p["description"]}, &lists); err != nil {
		return err.Error(), true
	}
	items, _ := p["tasks"].([]any)
	for _, item := range items {
		m, _ := item.(map[string]any)
		if err := call("tasks_create", map[string]any{"list_id": lists.List.ListID, "title": m["title"], "description": m["description"]}, nil); err != nil {
			return err.Error(), true
		}
	}
	return start(lists.List.ListID)
}

func start(listID string) (string, bool) {
	if listID == "" {
		return "list_id is required", true
	}
	if s, ok := state(); ok && s.Phase != "done" && s.Phase != "review" && s.Phase != "stopped" {
		return "runner already active: " + s.ListID, true
	}
	s := runnerState{ListID: listID, Phase: "starting"}
	save(s)
	if err := startNext(&s); err != nil {
		return err.Error(), true
	}
	return `{"status":"started"}`, false
}

func startNext(s *runnerState) error {
	var out struct {
		Task task `json:"task"`
	}
	var listed struct {
		Tasks []task `json:"tasks"`
	}
	if err := call("tasks_list", map[string]any{"list_id": s.ListID, "limit": 100}, &listed); err != nil {
		return err
	}
	for _, candidate := range listed.Tasks {
		if candidate.Status != "pending" {
			continue
		}
		if err := call("tasks_claim", map[string]any{
			"list_id":          s.ListID,
			"task_id":          candidate.TaskID,
			"agent_id":         "task-runner",
			"expected_version": candidate.Version,
		}, &out); err == nil {
			break
		} else {
			return err
		}
	}
	if out.Task.TaskID == "" {
		s.Phase = "done"
		s.TaskID = ""
		s.AgentID = ""
		save(*s)
		Notify("Task runner finished list " + s.ListID)
		return nil
	}
	agentID := "task-runner/" + out.Task.TaskID
	prompt := fmt.Sprintf(
		"You are executing task %s in list %s.\n\nTitle: %s\nDescription: %s\n\nWork only on this task. When complete, call mark_task_completed with task_id %q and structured evidence. If blocked or a decision is needed, call request_task_review with task_id %q and explain why. Do not claim completion in prose only.",
		out.Task.TaskID,
		s.ListID,
		out.Task.Title,
		out.Task.Description,
		out.Task.TaskID,
		out.Task.TaskID,
	)
	var spawned struct {
		Error string `json:"error"`
	}
	if err := call("agent_spawn", map[string]any{"id": agentID, "name": "task-" + out.Task.TaskID, "system_prompt": "You are a focused task worker. Follow the task runner completion protocol exactly.", "initial_prompt": prompt, "caller_id": "main"}, &spawned); err != nil ||
		spawned.Error != "" {
		return fmt.Errorf("agent_spawn: %v", err)
	}
	s.TaskID = out.Task.TaskID
	s.AgentID = agentID
	s.Phase = "running"
	save(*s)
	Notify("Task runner started " + out.Task.Title)
	return nil
}

func owned(callID string, p map[string]any) (runnerState, task, error) {
	s, ok := state()
	if !ok || s.Phase != "running" {
		return s, task{}, fmt.Errorf("no running task")
	}
	if p["task_id"] != s.TaskID {
		return s, task{}, fmt.Errorf("task_id does not match active task")
	}
	if callerByCall[callID] != s.AgentID {
		return s, task{}, fmt.Errorf("tool caller does not own active task")
	}
	var out struct {
		Task task `json:"task"`
	}
	if err := call("tasks_get", map[string]any{"list_id": s.ListID, "task_id": s.TaskID}, &out); err != nil {
		return s, task{}, err
	}
	if out.Task.AttemptID == "" {
		return s, task{}, fmt.Errorf("task has no active attempt")
	}
	return s, out.Task, nil
}

func complete(callID string, p map[string]any) (string, bool) {
	s, t, err := owned(callID, p)
	if err != nil {
		return err.Error(), true
	}
	if err = call("tasks_report", map[string]any{"list_id": s.ListID, "task_id": t.TaskID, "attempt_id": t.AttemptID, "agent_id": s.AgentID, "status": "completed", "result": p["result"]}, nil); err != nil {
		return err.Error(), true
	}
	s.Phase = "stopping"
	save(s)
	shutdown(s.AgentID)
	return `{"status":"completed","next":"pending_shutdown"}`, false
}

func review(callID string, p map[string]any) (string, bool) {
	s, t, err := owned(callID, p)
	if err != nil {
		return err.Error(), true
	}
	reason, _ := p["reason"].(string)
	details, _ := p["details"].(string)
	err = call(
		"tasks_report",
		map[string]any{
			"list_id":    s.ListID,
			"task_id":    t.TaskID,
			"attempt_id": t.AttemptID,
			"agent_id":   s.AgentID,
			"status":     "blocked",
			"reason":     "review_required: " + reason,
		},
		nil,
	)
	if err != nil {
		return err.Error(), true
	}
	s.Phase = "review"
	save(s)
	shutdown(s.AgentID)
	Notify("Task runner needs review for " + t.Title + ": " + reason + " " + details)
	return `{"status":"review_requested"}`, false
}

func shutdown(id string) {
	call(
		"agent_deliver",
		map[string]any{"id": id, "type": "system", "message": `{"event":"shutdown_request","from":"main"}`},
		nil,
	)
}

func advanceAfterShutdown() {
	s, ok := state()
	if !ok || s.Phase != "stopping" {
		return
	}
	var out struct {
		Agents []struct {
			ID string `json:"id"`
		} `json:"agents"`
	}
	if call("agent_list", nil, &out) != nil {
		return
	}
	for _, a := range out.Agents {
		if a.ID == s.AgentID {
			return
		}
	}
	s.Phase = "starting"
	save(s)
	if err := startNext(&s); err != nil {
		Notify("Task runner error: " + err.Error())
	}
}

func handleCommand(args []string) {
	if len(args) == 0 || args[0] == "status" {
		s, ok := state()
		if ok {
			Notify("Task runner: " + s.Phase + " list=" + s.ListID + " task=" + s.TaskID)
		} else {
			Notify("Task runner is idle")
		}
		return
	}
	if args[0] == "start" && len(args) > 1 {
		if _, failed := start(args[1]); failed {
			Notify("Task runner: start failed")
		}
		return
	}
	if args[0] == "stop" {
		if s, ok := state(); ok && s.AgentID != "" {
			shutdown(s.AgentID)
			s.Phase = "stopped"
			save(s)
			Notify("Task runner stopped")
		}
		return
	}
	Notify("Usage: /task-runner status | start <list_id> | stop")
}

func main() {}
