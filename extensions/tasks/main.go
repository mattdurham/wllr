//go:build wasip1

package main

import (
	"encoding/json"
	"fmt"
	"sync"
	"unsafe"
)

//go:wasmimport env host_log
func hostLog(level, ptr, length uint32)

//go:wasmimport env host_call
func hostCall(reqPtr, reqLen, respPtrPtr, respLenPtr uint32) uint32

var pinned = map[uintptr][]byte{}

// Task represents a task in a task list
type Task struct {
	ID           string   `json:"id"`
	Title        string   `json:"title"`
	Description  string   `json:"description"`
	Status       string   `json:"status"` // pending, in_progress, completed, blocked
	Priority     string   `json:"priority"` // low, medium, high, critical
	Tags         []string `json:"tags"`
	Dependencies []string `json:"dependencies"` // Task IDs this task depends on
	CreatedAt    int64    `json:"created_at"`
	UpdatedAt    int64    `json:"updated_at"`
}

// TaskList represents a collection of tasks
type TaskList struct {
	ID          string           `json:"id"`
	Name        string           `json:"name"`
	Description string           `json:"description"`
	Tasks       map[string]*Task `json:"tasks"`
	mu          sync.RWMutex
}

var (
	taskLists     = make(map[string]*TaskList)
	taskListMu    sync.RWMutex
	listCounter   int
	taskCounters  = make(map[string]int)
	counterMu     sync.Mutex
)

//go:wasmexport _alloc
func extensionAlloc(size int32) int32 {
	if size <= 0 {
		return 0
	}
	buf := make([]byte, size)
	ptr := uintptr(unsafe.Pointer(&buf[0]))
	pinned[ptr] = buf
	return int32(ptr)
}

//go:wasmexport _free
func extensionFree(ptr int32) {
	delete(pinned, uintptr(ptr))
}

//go:wasmexport _init
func extensionInit() int32 {
	tools := []struct {
		name   string
		desc   string
		schema string
	}{
		{
			"tasklist_create",
			"Create a new task list and return its unique ID",
			`{"type":"object","properties":{"name":{"type":"string","description":"Name of the task list"},"description":{"type":"string","description":"Description of the task list"}},"required":["name"]}`,
		},
		{
			"tasks_create",
			"Create a new task in a task list",
			`{"type":"object","properties":{"list_id":{"type":"string","description":"Task list ID"},"title":{"type":"string","description":"Task title"},"description":{"type":"string","description":"Task description"},"priority":{"type":"string","enum":["low","medium","high","critical"],"description":"Task priority"},"tags":{"type":"array","items":{"type":"string"},"description":"Task tags"},"dependencies":{"type":"array","items":{"type":"string"},"description":"Task IDs this task depends on"}},"required":["list_id","title"]}`,
		},
		{
			"tasks_update",
			"Update an existing task",
			`{"type":"object","properties":{"list_id":{"type":"string","description":"Task list ID"},"task_id":{"type":"string","description":"Task ID"},"title":{"type":"string","description":"New title"},"description":{"type":"string","description":"New description"},"status":{"type":"string","enum":["pending","in_progress","completed","blocked"],"description":"New status"},"priority":{"type":"string","enum":["low","medium","high","critical"],"description":"New priority"},"tags":{"type":"array","items":{"type":"string"},"description":"New tags"},"dependencies":{"type":"array","items":{"type":"string"},"description":"New dependencies"}},"required":["list_id","task_id"]}`,
		},
		{
			"tasks_list",
			"List all tasks in a task list",
			`{"type":"object","properties":{"list_id":{"type":"string","description":"Task list ID"},"status":{"type":"string","enum":["pending","in_progress","completed","blocked"],"description":"Filter by status (optional)"}},"required":["list_id"]}`,
		},
		{
			"tasks_get",
			"Get details of a specific task",
			`{"type":"object","properties":{"list_id":{"type":"string","description":"Task list ID"},"task_id":{"type":"string","description":"Task ID"}},"required":["list_id","task_id"]}`,
		},
	}

	for _, t := range tools {
		if rc := registerTool(t.name, t.desc, t.schema); rc != 0 {
			return rc
		}
	}
	return 0
}

//go:wasmexport _on_event
func extensionOnEvent(ptr, length int32) int32 {
	data := unsafe.Slice((*byte)(unsafe.Pointer(uintptr(ptr))), length)

	var evt struct {
		Type    string          `json:"type"`
		Payload json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(data, &evt); err != nil {
		return 0
	}

	if evt.Type == "before_tool_call" {
		onBeforeToolCall(evt.Payload)
	}
	return 0
}

type beforeToolCallPayload struct {
	AgentID    string          `json:"agent_id"`
	ToolCallID string          `json:"tool_call_id"`
	ToolName   string          `json:"tool_name"`
	Input      json.RawMessage `json:"input"`
}

func onBeforeToolCall(raw json.RawMessage) {
	var p beforeToolCallPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return
	}

	switch p.ToolName {
	case "tasklist_create":
		handleTasklistCreate(p)
	case "tasks_create":
		handleTasksCreate(p)
	case "tasks_update":
		handleTasksUpdate(p)
	case "tasks_list":
		handleTasksList(p)
	case "tasks_get":
		handleTasksGet(p)
	}
}

func handleTasklistCreate(p beforeToolCallPayload) {
	var input struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal(p.Input, &input); err != nil || input.Name == "" {
		sendToolResult(p.ToolCallID, "tasklist_create: name is required", true)
		return
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
	sendToolResult(p.ToolCallID, string(out), false)
}

func handleTasksCreate(p beforeToolCallPayload) {
	var input struct {
		ListID       string   `json:"list_id"`
		Title        string   `json:"title"`
		Description  string   `json:"description"`
		Priority     string   `json:"priority"`
		Tags         []string `json:"tags"`
		Dependencies []string `json:"dependencies"`
	}
	if err := json.Unmarshal(p.Input, &input); err != nil || input.ListID == "" || input.Title == "" {
		sendToolResult(p.ToolCallID, "tasks_create: list_id and title are required", true)
		return
	}

	taskListMu.RLock()
	taskList, exists := taskLists[input.ListID]
	taskListMu.RUnlock()

	if !exists {
		sendToolResult(p.ToolCallID, "tasks_create: task list not found", true)
		return
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
	sendToolResult(p.ToolCallID, string(out), false)
}

func handleTasksUpdate(p beforeToolCallPayload) {
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
		sendToolResult(p.ToolCallID, "tasks_update: list_id and task_id are required", true)
		return
	}

	taskListMu.RLock()
	taskList, exists := taskLists[input.ListID]
	taskListMu.RUnlock()

	if !exists {
		sendToolResult(p.ToolCallID, "tasks_update: task list not found", true)
		return
	}

	taskList.mu.Lock()
	task, exists := taskList.Tasks[input.TaskID]
	if !exists {
		taskList.mu.Unlock()
		sendToolResult(p.ToolCallID, "tasks_update: task not found", true)
		return
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
	sendToolResult(p.ToolCallID, string(out), false)
}

func handleTasksList(p beforeToolCallPayload) {
	var input struct {
		ListID string `json:"list_id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(p.Input, &input); err != nil || input.ListID == "" {
		sendToolResult(p.ToolCallID, "tasks_list: list_id is required", true)
		return
	}

	taskListMu.RLock()
	taskList, exists := taskLists[input.ListID]
	taskListMu.RUnlock()

	if !exists {
		sendToolResult(p.ToolCallID, "tasks_list: task list not found", true)
		return
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
	sendToolResult(p.ToolCallID, string(out), false)
}

func handleTasksGet(p beforeToolCallPayload) {
	var input struct {
		ListID string `json:"list_id"`
		TaskID string `json:"task_id"`
	}
	if err := json.Unmarshal(p.Input, &input); err != nil || input.ListID == "" || input.TaskID == "" {
		sendToolResult(p.ToolCallID, "tasks_get: list_id and task_id are required", true)
		return
	}

	taskListMu.RLock()
	taskList, exists := taskLists[input.ListID]
	taskListMu.RUnlock()

	if !exists {
		sendToolResult(p.ToolCallID, "tasks_get: task list not found", true)
		return
	}

	taskList.mu.RLock()
	task, exists := taskList.Tasks[input.TaskID]
	taskList.mu.RUnlock()

	if !exists {
		sendToolResult(p.ToolCallID, "tasks_get: task not found", true)
		return
	}

	out, _ := json.Marshal(task)
	sendToolResult(p.ToolCallID, string(out), false)
}

func registerTool(name, desc, inputSchema string) int32 {
	type toolParams struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		InputSchema json.RawMessage `json:"input_schema"`
	}
	rc := hostCallJSON(
		"register_tool",
		toolParams{Name: name, Description: desc, InputSchema: json.RawMessage(inputSchema)},
	)
	if rc != 0 {
		return rc
	}
	return hostCallJSON("subscribe", map[string]string{"event": "before_tool_call"})
}

func sendToolResult(toolCallID, result string, isError bool) {
	hostCallJSON("tool_result", map[string]any{"tool_call_id": toolCallID, "result": result, "is_error": isError})
}

func hostCallJSON(method string, params any) int32 {
	type request struct {
		Method string `json:"method"`
		Params any    `json:"params,omitempty"`
	}
	reqBytes, err := json.Marshal(request{Method: method, Params: params})
	if err != nil {
		return 1
	}
	reqBuf := make([]byte, len(reqBytes))
	copy(reqBuf, reqBytes)
	reqPtr := uintptr(unsafe.Pointer(&reqBuf[0]))
	var respPtr, respLen uint32
	rc := hostCall(
		uint32(reqPtr), uint32(len(reqBuf)),
		uint32(uintptr(unsafe.Pointer(&respPtr))),
		uint32(uintptr(unsafe.Pointer(&respLen))),
	)
	if respPtr != 0 {
		delete(pinned, uintptr(respPtr))
	}
	return int32(rc)
}

func main() {}
