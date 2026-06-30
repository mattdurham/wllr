//go:build wasip1

package main

import (
	"encoding/json"
	"fmt"
	"sync"
)

// agentCall fires a host_call for agent-related methods and returns the raw
// response, or "" on error. It reuses the SDK's _sdkCallResult to avoid
// duplicating the WASM import.
func agentCall(method string, params any) string {
	raw := _sdkCallResult(method, params)
	if raw == nil {
		return ""
	}
	return string(raw)
}

// Task represents a task in a task list.

// pending, in_progress, completed, blocked
// low, medium, high, critical

// Task IDs this task depends on

// TaskList represents a collection of tasks.

var (
	taskLists    = make(map[string]*TaskList)
	taskListMu   sync.RWMutex
	listCounter  int
	taskCounters = make(map[string]int)
	counterMu    sync.Mutex
)

func init() {
	RegisterTool(
		"tasklist_create",
		"Create a new task list and return its unique ID",
		json.RawMessage(`{"type":"object","properties":{"name":{"type":"string","description":"Name of the task list"},"description":{"type":"string","description":"Description of the task list"},"owner_agent_id":{"type":"string","description":"Agent ID to notify on task completion or blocking"}},"required":["name"]}`),
	)
	RegisterTool(
		"tasks_create",
		"Create a new task in a task list",
		json.RawMessage(`{"type":"object","properties":{"list_id":{"type":"string","description":"Task list ID"},"title":{"type":"string","description":"Task title"},"description":{"type":"string","description":"Task description"},"priority":{"type":"string","enum":["low","medium","high","critical"],"description":"Task priority"},"tags":{"type":"array","items":{"type":"string"},"description":"Task tags"},"dependencies":{"type":"array","items":{"type":"string"},"description":"Task IDs this task depends on"}},"required":["list_id","title"]}`),
	)
	RegisterTool(
		"tasks_update",
		"Update an existing task",
		json.RawMessage(`{"type":"object","properties":{"list_id":{"type":"string","description":"Task list ID"},"task_id":{"type":"string","description":"Task ID"},"title":{"type":"string","description":"New title"},"description":{"type":"string","description":"New description"},"status":{"type":"string","enum":["pending","in_progress","completed","blocked"],"description":"New status"},"priority":{"type":"string","enum":["low","medium","high","critical"],"description":"New priority"},"tags":{"type":"array","items":{"type":"string"},"description":"New tags"},"dependencies":{"type":"array","items":{"type":"string"},"description":"New dependencies"}},"required":["list_id","task_id"]}`),
	)
	RegisterTool(
		"tasks_list",
		"List all tasks in a task list",
		json.RawMessage(`{"type":"object","properties":{"list_id":{"type":"string","description":"Task list ID"},"status":{"type":"string","enum":["pending","in_progress","completed","blocked"],"description":"Filter by status (optional)"}},"required":["list_id"]}`),
	)
	RegisterTool(
		"tasks_get",
		"Get details of a specific task",
		json.RawMessage(`{"type":"object","properties":{"list_id":{"type":"string","description":"Task list ID"},"task_id":{"type":"string","description":"Task ID"}},"required":["list_id","task_id"]}`),
	)

	OnToolCall(func(callID, toolName string, input json.RawMessage) (string, bool) {
		p := toolPayload{ToolCallID: callID, Input: input}
		switch toolName {
		case "tasklist_create":
			return handleTasklistCreate(p)
		case "tasks_create":
			return handleTasksCreate(p)
		case "tasks_update":
			return handleTasksUpdate(p)
		case "tasks_list":
			return handleTasksList(p)
		case "tasks_get":
			return handleTasksGet(p)
		default:
			return "", false
		}
	})
}

func handleTasklistCreate(p toolPayload) (string, bool) {
	var input struct {
		Name         string `json:"name"`
		Description  string `json:"description"`
		OwnerAgentID string `json:"owner_agent_id"`
	}
	if err := json.Unmarshal(p.Input, &input); err != nil || input.Name == "" {
		return "tasklist_create: name is required", true
	}

	counterMu.Lock()
	listCounter++
	listID := fmt.Sprintf("list-%d", listCounter)
	counterMu.Unlock()

	taskList := &TaskList{
		ID:           listID,
		Name:         input.Name,
		Description:  input.Description,
		Tasks:        make(map[string]*Task),
		OwnerAgentID: input.OwnerAgentID,
	}

	taskListMu.Lock()
	taskLists[listID] = taskList
	taskCounters[listID] = 0
	taskListMu.Unlock()

	out, _ := json.Marshal(map[string]string{"list_id": listID})
	return string(out), false
}

func handleTasksCreate(p toolPayload) (string, bool) {
	var input struct {
		ListID       string   `json:"list_id"`
		Title        string   `json:"title"`
		Description  string   `json:"description"`
		Priority     string   `json:"priority"`
		Tags         []string `json:"tags"`
		Dependencies []string `json:"dependencies"`
	}
	if err := json.Unmarshal(p.Input, &input); err != nil || input.ListID == "" || input.Title == "" {
		return "tasks_create: list_id and title are required", true
	}

	taskListMu.RLock()
	taskList, exists := taskLists[input.ListID]
	taskListMu.RUnlock()

	if !exists {
		return "tasks_create: task list not found", true
	}

	counterMu.Lock()
	taskCounters[input.ListID]++
	taskID := fmt.Sprintf("task-%d", taskCounters[input.ListID])
	counterMu.Unlock()

	if input.Priority == "" {
		input.Priority = "medium"
	}

	task := &Task{
		ID:           taskID,
		Title:        input.Title,
		Description:  input.Description,
		Status:       "pending",
		Priority:     input.Priority,
		Tags:         input.Tags,
		Dependencies: input.Dependencies,
	}

	taskList.mu.Lock()
	taskList.Tasks[taskID] = task
	taskList.mu.Unlock()

	out, _ := json.Marshal(map[string]string{"task_id": taskID})
	return string(out), false
}

func handleTasksUpdate(p toolPayload) (string, bool) {
	var input struct {
		ListID       string   `json:"list_id"`
		TaskID       string   `json:"task_id"`
		Title        string   `json:"title"`
		Description  string   `json:"description"`
		Status       string   `json:"status"`
		Priority     string   `json:"priority"`
		Tags         []string `json:"tags"`
		Dependencies []string `json:"dependencies"`
	}
	if err := json.Unmarshal(p.Input, &input); err != nil || input.ListID == "" || input.TaskID == "" {
		return "tasks_update: list_id and task_id are required", true
	}

	taskListMu.RLock()
	taskList, exists := taskLists[input.ListID]
	taskListMu.RUnlock()

	if !exists {
		return "tasks_update: task list not found", true
	}

	taskList.mu.Lock()
	task, exists := taskList.Tasks[input.TaskID]
	if !exists {
		taskList.mu.Unlock()
		return "tasks_update: task not found", true
	}

	oldStatus := task.Status

	if input.Title != "" {
		task.Title = input.Title
	}
	if input.Description != "" {
		task.Description = input.Description
	}
	if input.Status != "" {
		task.Status = input.Status
	}
	if input.Priority != "" {
		task.Priority = input.Priority
	}
	if input.Tags != nil {
		task.Tags = input.Tags
	}
	if input.Dependencies != nil {
		task.Dependencies = input.Dependencies
	}

	ownerAgentID := taskList.OwnerAgentID
	taskTitle := task.Title
	taskID := task.ID
	newStatus := task.Status
	taskList.mu.Unlock()

	if shouldNotify(oldStatus, newStatus) && ownerAgentID != "" {
		// agent_deliver queues the TASK_DONE notification AND wakes the owner so it
		// reacts immediately. The prior agent_send_message-only path left the
		// notification sitting in the owner's inbox until it happened to run for
		// some other reason — a silent stall in the task-coordination pattern.
		resp := agentCall("agent_deliver", map[string]string{
			"id":      ownerAgentID,
			"message": fmt.Sprintf("TASK_DONE: %s %s", taskID, taskTitle),
		})
		if resp != "" {
			var errResp struct {
				Error string `json:"error"`
			}
			if jsonErr := json.Unmarshal([]byte(resp), &errResp); jsonErr == nil && errResp.Error != "" {
				fmt.Printf("tasks: agent_deliver warning: %s\n", errResp.Error)
			}
		}
	}

	out, _ := json.Marshal(map[string]bool{"success": true})
	return string(out), false
}

func handleTasksList(p toolPayload) (string, bool) {
	var input struct {
		ListID string `json:"list_id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(p.Input, &input); err != nil || input.ListID == "" {
		return "tasks_list: list_id is required", true
	}

	taskListMu.RLock()
	taskList, exists := taskLists[input.ListID]
	taskListMu.RUnlock()

	if !exists {
		return "tasks_list: task list not found", true
	}

	taskList.mu.RLock()
	var tasks []*Task
	for _, task := range taskList.Tasks {
		if input.Status == "" || task.Status == input.Status {
			tasks = append(tasks, task)
		}
	}
	taskList.mu.RUnlock()

	out, _ := json.Marshal(map[string][]*Task{"tasks": tasks})
	return string(out), false
}

func handleTasksGet(p toolPayload) (string, bool) {
	var input struct {
		ListID string `json:"list_id"`
		TaskID string `json:"task_id"`
	}
	if err := json.Unmarshal(p.Input, &input); err != nil || input.ListID == "" || input.TaskID == "" {
		return "tasks_get: list_id and task_id are required", true
	}

	taskListMu.RLock()
	taskList, exists := taskLists[input.ListID]
	taskListMu.RUnlock()

	if !exists {
		return "tasks_get: task list not found", true
	}

	taskList.mu.RLock()
	task, exists := taskList.Tasks[input.TaskID]
	taskList.mu.RUnlock()

	if !exists {
		return "tasks_get: task not found", true
	}

	out, _ := json.Marshal(task)
	return string(out), false
}

func main() {}
