package sdk

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"encoding/json"
	"time"
)

type TaskStatus string

const (
	TaskPending    TaskStatus = "pending"
	TaskInProgress TaskStatus = "in_progress"
	TaskCompleted  TaskStatus = "completed"
	TaskBlocked    TaskStatus = "blocked"
	TaskFailed     TaskStatus = "failed"
	TaskCancelled  TaskStatus = "cancelled"
)

type TaskWorkspaceMode string

const (
	TaskWorkspaceShared   TaskWorkspaceMode = "shared"
	TaskWorkspaceWorktree TaskWorkspaceMode = "worktree"
	TaskWorkspaceReadonly TaskWorkspaceMode = "readonly"
)

type TaskList struct {
	ListID       string `json:"list_id"`
	Name         string `json:"name"`
	Description  string `json:"description,omitempty"`
	OwnerAgentID string `json:"owner_agent_id,omitempty"`
	Cursor       int64  `json:"cursor"`
	Version      int64  `json:"version"`
}

type TaskRecord struct {
	CreatedAt       time.Time         `json:"created_at"`
	UpdatedAt       time.Time         `json:"updated_at"`
	TaskID          string            `json:"task_id"`
	ListID          string            `json:"list_id"`
	ParentTaskID    string            `json:"parent_task_id,omitempty"`
	OwnerAgentID    string            `json:"owner_agent_id,omitempty"`
	AssigneeAgentID string            `json:"assignee_agent_id,omitempty"`
	Title           string            `json:"title"`
	Description     string            `json:"description,omitempty"`
	Status          TaskStatus        `json:"status"`
	Error           string            `json:"error,omitempty"`
	Reason          string            `json:"reason,omitempty"`
	WorkspaceMode   TaskWorkspaceMode `json:"workspace_mode"`
	AttemptID       string            `json:"attempt_id,omitempty"`
	DependsOn       []string          `json:"depends_on,omitempty"`
	Result          json.RawMessage   `json:"result,omitempty"`
	Priority        int               `json:"priority,omitempty"`
	Version         int64             `json:"version"`
}

type TaskAttempt struct {
	AttemptID  string     `json:"attempt_id"`
	TaskID     string     `json:"task_id"`
	AgentID    string     `json:"agent_id"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	Status     TaskStatus `json:"status"`
}

type TaskEvent struct {
	CreatedAt    time.Time       `json:"created_at"`
	EventID      string          `json:"event_id"`
	ListID       string          `json:"list_id"`
	TaskID       string          `json:"task_id,omitempty"`
	AttemptID    string          `json:"attempt_id,omitempty"`
	Event        string          `json:"event"`
	ActorAgentID string          `json:"actor_agent_id,omitempty"`
	Snapshot     json.RawMessage `json:"snapshot,omitempty"`
	Version      int64           `json:"version"`
}

type TaskEventEnvelope struct {
	EventID      string          `json:"event_id"`
	ListID       string          `json:"list_id"`
	TaskID       string          `json:"task_id,omitempty"`
	AttemptID    string          `json:"attempt_id,omitempty"`
	Event        string          `json:"event"`
	ActorAgentID string          `json:"actor_agent_id,omitempty"`
	Reference    string          `json:"reference,omitempty"`
	Snapshot     json.RawMessage `json:"snapshot,omitempty"`
	Version      int64           `json:"version"`
}

type TasklistCreateRequest struct {
	Name         string `json:"name"`
	Description  string `json:"description,omitempty"`
	OwnerAgentID string `json:"owner_agent_id,omitempty"`
}
type TasksCreateRequest struct {
	ListID          string            `json:"list_id"`
	ParentTaskID    string            `json:"parent_task_id,omitempty"`
	OwnerAgentID    string            `json:"owner_agent_id,omitempty"`
	AssigneeAgentID string            `json:"assignee_agent_id,omitempty"`
	Title           string            `json:"title"`
	Description     string            `json:"description,omitempty"`
	WorkspaceMode   TaskWorkspaceMode `json:"workspace_mode"`
	DependsOn       []string          `json:"depends_on,omitempty"`
	Priority        int               `json:"priority,omitempty"`
}
type TasksClaimRequest struct {
	ListID          string `json:"list_id"`
	TaskID          string `json:"task_id"`
	AgentID         string `json:"agent_id"`
	ExpectedVersion int64  `json:"expected_version"`
}
type TasksUpdateRequest struct {
	Title           *string            `json:"title,omitempty"`
	Description     *string            `json:"description,omitempty"`
	Priority        *int               `json:"priority,omitempty"`
	AssigneeAgentID *string            `json:"assignee_agent_id,omitempty"`
	WorkspaceMode   *TaskWorkspaceMode `json:"workspace_mode,omitempty"`
	ListID          string             `json:"list_id"`
	TaskID          string             `json:"task_id"`
	AgentID         string             `json:"agent_id,omitempty"`
	ExpectedVersion int64              `json:"expected_version"`
}
type TasksReportRequest struct {
	ListID    string          `json:"list_id"`
	TaskID    string          `json:"task_id"`
	AttemptID string          `json:"attempt_id"`
	AgentID   string          `json:"agent_id"`
	Status    TaskStatus      `json:"status"`
	Error     string          `json:"error,omitempty"`
	Reason    string          `json:"reason,omitempty"`
	Result    json.RawMessage `json:"result,omitempty"`
}
type TasksGetRequest struct {
	ListID string `json:"list_id"`
	TaskID string `json:"task_id"`
}
type TasksListRequest struct {
	ListID string `json:"list_id"`
	Cursor int64  `json:"cursor,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}
type TasksEventsAfterRequest struct {
	ListID string `json:"list_id"`
	Cursor int64  `json:"cursor"`
	Limit  int    `json:"limit,omitempty"`
}

type TaskListResponse struct {
	List TaskList `json:"list"`
}
type TaskRecordResponse struct {
	Task TaskRecord `json:"task"`
}
type TaskListRecordsResponse struct {
	Tasks      []TaskRecord `json:"tasks"`
	Cursor     int64        `json:"cursor"`
	NextCursor int64        `json:"next_cursor"`
}
type TaskEventsResponse struct {
	Events     []TaskEvent `json:"events"`
	Cursor     int64       `json:"cursor"`
	NextCursor int64       `json:"next_cursor"`
}
