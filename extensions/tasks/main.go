//go:build wasip1

package main

import (
	"encoding/json"
	"sort"
)

type taskListResponse struct {
	List struct {
		ListID  string `json:"list_id"`
		Version int64  `json:"version"`
	} `json:"list"`
}
type taskResponse struct {
	Task taskRecord `json:"task"`
}
type tasksResponse struct {
	Tasks      []taskRecord `json:"tasks"`
	Cursor     int64        `json:"cursor"`
	NextCursor int64        `json:"next_cursor"`
}
type eventsResponse struct {
	Events     []taskEvent `json:"events"`
	Cursor     int64       `json:"cursor"`
	NextCursor int64       `json:"next_cursor"`
}
type taskRecord struct {
	TaskID          string          `json:"task_id"`
	ListID          string          `json:"list_id"`
	Title           string          `json:"title"`
	Description     string          `json:"description,omitempty"`
	Status          string          `json:"status"`
	Priority        int             `json:"priority,omitempty"`
	DependsOn       []string        `json:"depends_on,omitempty"`
	Result          json.RawMessage `json:"result,omitempty"`
	Error           string          `json:"error,omitempty"`
	Reason          string          `json:"reason,omitempty"`
	ParentTaskID    string          `json:"parent_task_id,omitempty"`
	OwnerAgentID    string          `json:"owner_agent_id,omitempty"`
	AssigneeAgentID string          `json:"assignee_agent_id,omitempty"`
	WorkspaceMode   string          `json:"workspace_mode"`
	AttemptID       string          `json:"attempt_id,omitempty"`
	Version         int64           `json:"version"`
}
type taskEvent struct {
	EventID      string          `json:"event_id"`
	ListID       string          `json:"list_id"`
	TaskID       string          `json:"task_id,omitempty"`
	AttemptID    string          `json:"attempt_id,omitempty"`
	Event        string          `json:"event"`
	Version      int64           `json:"version"`
	ActorAgentID string          `json:"actor_agent_id,omitempty"`
	Snapshot     json.RawMessage `json:"snapshot,omitempty"`
}

func ledgerCall(method string, params any, out any) (string, bool) {
	raw, hostErr := _sdkCallResultWithError(method, params)
	if hostErr != "" {
		return method + ": " + hostErr, true
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			return method + ": invalid host result: " + err.Error(), true
		}
	}
	return string(raw), false
}

func init() {
	RegisterToolWithOutput("tasklist_create", "Create a durable task list.", json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"},"description":{"type":"string"},"owner_agent_id":{"type":"string"}},"required":["name"]}`), json.RawMessage(`{"type":"object"}`))
	RegisterToolWithOutput("tasks_create", "Create a durable task; workspace_mode is metadata only.", json.RawMessage(`{"type":"object","properties":{"list_id":{"type":"string"},"title":{"type":"string"},"description":{"type":"string"},"priority":{"type":"integer"},"parent_task_id":{"type":"string"},"owner_agent_id":{"type":"string"},"workspace_mode":{"type":"string","enum":["shared","worktree","readonly"]},"depends_on":{"type":"array","items":{"type":"string"}}},"required":["list_id","title"]}`), json.RawMessage(`{"type":"object"}`))
	RegisterToolWithOutput("tasks_update", "CAS-update a durable task; pass the returned version as expected_version.", json.RawMessage(`{"type":"object","properties":{"list_id":{"type":"string"},"task_id":{"type":"string"},"expected_version":{"type":"integer"},"title":{"type":"string"},"description":{"type":"string"},"priority":{"type":"integer"},"assignee_agent_id":{"type":"string"},"workspace_mode":{"type":"string","enum":["shared","worktree","readonly"]}},"required":["list_id","task_id","expected_version"]}`), json.RawMessage(`{"type":"object"}`))
	RegisterToolWithOutput("tasks_list", "List durable tasks with a bounded cursor.", json.RawMessage(`{"type":"object","properties":{"list_id":{"type":"string"},"cursor":{"type":"integer"},"limit":{"type":"integer"},"status":{"type":"string"}},"required":["list_id"]}`), json.RawMessage(`{"type":"object"}`))
	RegisterToolWithOutput("tasks_get", "Get the authoritative durable task record.", json.RawMessage(`{"type":"object","properties":{"list_id":{"type":"string"},"task_id":{"type":"string"}},"required":["list_id","task_id"]}`), json.RawMessage(`{"type":"object"}`))
	RegisterToolWithOutput("tasks_claim", "Claim the next eligible task atomically.", json.RawMessage(`{"type":"object","properties":{"list_id":{"type":"string"},"agent_id":{"type":"string"}},"required":["list_id","agent_id"]}`), json.RawMessage(`{"type":"object"}`))
	RegisterToolWithOutput("tasks_report", "Report a claimed task result exactly once.", json.RawMessage(`{"type":"object","properties":{"list_id":{"type":"string"},"task_id":{"type":"string"},"attempt_id":{"type":"string"},"agent_id":{"type":"string"},"status":{"type":"string","enum":["completed","blocked","failed","cancelled"]},"result":{},"error":{"type":"string"},"reason":{"type":"string"}},"required":["list_id","task_id","attempt_id","agent_id","status"]}`), json.RawMessage(`{"type":"object"}`))
	RegisterToolWithOutput("tasks_events_after", "Replay task events after a cursor; use this after every wake or compaction.", json.RawMessage(`{"type":"object","properties":{"list_id":{"type":"string"},"cursor":{"type":"integer"},"limit":{"type":"integer"}},"required":["list_id","cursor"]}`), json.RawMessage(`{"type":"object"}`))
	OnToolCall(func(_ string, name string, input json.RawMessage) (string, bool) {
		var params map[string]any
		if err := json.Unmarshal(input, &params); err != nil {
			return name + ": invalid JSON: " + err.Error(), true
		}
		switch name {
		case "tasklist_create":
			var out taskListResponse
			return ledgerCall(name, params, &out)
		case "tasks_create", "tasks_update", "tasks_get", "tasks_report":
			var out taskResponse
			return ledgerCall(name, params, &out)
		case "tasks_events_after":
			var out eventsResponse
			return ledgerCall(name, params, &out)
		case "tasks_list":
			return listTasks(params)
		case "tasks_claim":
			return claimNextTask(params)
		default:
			return "", false
		}
	})
}

func listTasks(params map[string]any) (string, bool) {
	var out tasksResponse
	result, failed := ledgerCall("tasks_list", params, &out)
	if failed || params["status"] == nil {
		return result, failed
	}
	want, _ := params["status"].(string)
	filtered := out.Tasks[:0]
	for _, task := range out.Tasks {
		if task.Status == want {
			filtered = append(filtered, task)
		}
	}
	out.Tasks = filtered
	b, _ := json.Marshal(out)
	return string(b), false
}

func claimNextTask(params map[string]any) (string, bool) {
	listID, _ := params["list_id"].(string)
	agentID, _ := params["agent_id"].(string)
	var listed tasksResponse
	if _, failed := ledgerCall("tasks_list", map[string]any{"list_id": listID, "limit": 100}, &listed); failed {
		return "tasks_claim: unable to read task list", true
	}
	sort.Slice(listed.Tasks, func(i, j int) bool { return listed.Tasks[i].TaskID < listed.Tasks[j].TaskID })
	for _, task := range listed.Tasks {
		if task.Status != "pending" {
			continue
		}
		var claimed taskResponse
		result, failed := ledgerCall("tasks_claim", map[string]any{"list_id": listID, "task_id": task.TaskID, "agent_id": agentID, "expected_version": task.Version}, &claimed)
		if !failed {
			return result, false
		}
	}
	return `{"task":null}`, false
}

func main() {}
