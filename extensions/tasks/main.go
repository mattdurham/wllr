//go:build wasip1

package main

import (
	"encoding/json"
	"fmt"
	"sync"
)

// Task represents a task in a task list.
type Task struct {
	ID           string   `json:"id"`
	Title        string   `json:"title"`
	Description  string   `json:"description"`
	Status       string   `json:"status"`   // pending, in_progress, completed, blocked
	Priority     string   `json:"priority"` // low, medium, high, critical
	Tags         []string `json:"tags"`
	Dependencies []string `json:"dependencies"` // Task IDs this task depends on
	CreatedAt    int64    `json:"created_at"`
	UpdatedAt    int64    `json:"updated_at"`
}

// TaskList represents a collection of tasks.
type TaskList struct {
	ID          string           `json:"id"`
	Name        string           `json:"name"`
	Description string           `json:"description"`
	Tasks       map[string]*Task `json:"tasks"`
	mu          sync.RWMutex
}

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
		json.RawMessage(`{"type":"object","properties":{"name":{"type":"string","description":"Name of the task list"},"description":{"type":"string","description":"Description of the task list"}},"required":["name"]}`),
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

type toolPayload struct {
	ToolCallID string
	Input      json.RawMessage
}

func handleTasklistCreate(p toolPayload) (string, bool) {
	var input struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal(p.Input, &input); err != nil || input.Name == "" {
		return "tasklist_create: name is required", true
	}

	counterMu.Lock()
	listCounter++
	listID := fmt.Sprintf("list-%d", listCounter)
	counterMu.Unlock()

	taskList := &TaskList{
		ID:          listID,
		Name:        input.Name,
		Description: input.Description,
		Tasks:       make(map[string]*Task),
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
	taskList.mu.Unlock()

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
