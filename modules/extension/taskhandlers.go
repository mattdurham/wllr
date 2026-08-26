package extension

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"github.com/mattdurham/wllr/modules/sdk"
)

func decodeTask(req sdk.HostCallRequest, v any) error {
	d := json.NewDecoder(bytes.NewReader(req.Params))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return err
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return fmt.Errorf("trailing JSON")
	}
	return nil
}
func taskResult(v any) sdk.HostCallResponse {
	b, err := json.Marshal(v)
	if err != nil {
		return sdk.HostCallResponse{Error: err.Error()}
	}
	return sdk.HostCallResponse{Result: b}
}
func (h *Host) withLedger(name string, fn func(*TaskLedger) sdk.HostCallResponse) sdk.HostCallResponse {
	l := h.taskLedgerSnapshot()
	if l == nil {
		return sdk.HostCallResponse{Error: name + ": task ledger is not configured"}
	}
	return fn(l)
}
func (h *Host) handleTasklistCreate(req sdk.HostCallRequest) sdk.HostCallResponse {
	var p sdk.TasklistCreateRequest
	if err := decodeTask(req, &p); err != nil {
		return sdk.HostCallResponse{Error: "tasklist_create: " + err.Error()}
	}
	return h.withLedger("tasklist_create", func(l *TaskLedger) sdk.HostCallResponse {
		v, err := l.CreateList(p)
		if err != nil {
			return sdk.HostCallResponse{Error: err.Error()}
		}
		return taskResult(sdk.TaskListResponse{List: v})
	})
}
func (h *Host) handleTasksCreate(req sdk.HostCallRequest) sdk.HostCallResponse {
	var p sdk.TasksCreateRequest
	if err := decodeTask(req, &p); err != nil {
		return sdk.HostCallResponse{Error: "tasks_create: " + err.Error()}
	}
	return h.withLedger("tasks_create", func(l *TaskLedger) sdk.HostCallResponse {
		v, err := l.CreateTask(p)
		if err != nil {
			return sdk.HostCallResponse{Error: err.Error()}
		}
		return taskResult(sdk.TaskRecordResponse{Task: v})
	})
}
func (h *Host) handleTasksClaim(req sdk.HostCallRequest) sdk.HostCallResponse {
	var p sdk.TasksClaimRequest
	if err := decodeTask(req, &p); err != nil {
		return sdk.HostCallResponse{Error: "tasks_claim: " + err.Error()}
	}
	return h.withLedger("tasks_claim", func(l *TaskLedger) sdk.HostCallResponse {
		v, err := l.Claim(p)
		if err != nil {
			return sdk.HostCallResponse{Error: err.Error()}
		}
		return taskResult(sdk.TaskRecordResponse{Task: v})
	})
}
func (h *Host) handleTasksUpdate(req sdk.HostCallRequest) sdk.HostCallResponse {
	var p sdk.TasksUpdateRequest
	if err := decodeTask(req, &p); err != nil {
		return sdk.HostCallResponse{Error: "tasks_update: " + err.Error()}
	}
	return h.withLedger("tasks_update", func(l *TaskLedger) sdk.HostCallResponse {
		v, err := l.UpdateCAS(p)
		if err != nil {
			return sdk.HostCallResponse{Error: err.Error()}
		}
		return taskResult(sdk.TaskRecordResponse{Task: v})
	})
}
func (h *Host) handleTasksReport(req sdk.HostCallRequest) sdk.HostCallResponse {
	var p sdk.TasksReportRequest
	if err := decodeTask(req, &p); err != nil {
		return sdk.HostCallResponse{Error: "tasks_report: " + err.Error()}
	}
	return h.withLedger("tasks_report", func(l *TaskLedger) sdk.HostCallResponse {
		v, err := l.Report(p)
		if err != nil {
			return sdk.HostCallResponse{Error: err.Error()}
		}
		return taskResult(sdk.TaskRecordResponse{Task: v})
	})
}
func (h *Host) handleTasksGet(req sdk.HostCallRequest) sdk.HostCallResponse {
	var p sdk.TasksGetRequest
	if err := decodeTask(req, &p); err != nil {
		return sdk.HostCallResponse{Error: "tasks_get: " + err.Error()}
	}
	return h.withLedger("tasks_get", func(l *TaskLedger) sdk.HostCallResponse {
		v, err := l.Get(p.ListID, p.TaskID)
		if err != nil {
			return sdk.HostCallResponse{Error: err.Error()}
		}
		return taskResult(sdk.TaskRecordResponse{Task: v})
	})
}
func (h *Host) handleTasksList(req sdk.HostCallRequest) sdk.HostCallResponse {
	var p sdk.TasksListRequest
	if err := decodeTask(req, &p); err != nil {
		return sdk.HostCallResponse{Error: "tasks_list: " + err.Error()}
	}
	return h.withLedger("tasks_list", func(l *TaskLedger) sdk.HostCallResponse {
		v, c, n, err := l.List(p.ListID, p.Cursor, p.Limit)
		if err != nil {
			return sdk.HostCallResponse{Error: err.Error()}
		}
		return taskResult(sdk.TaskListRecordsResponse{Tasks: v, Cursor: c, NextCursor: n})
	})
}
func (h *Host) handleTasksEventsAfter(req sdk.HostCallRequest) sdk.HostCallResponse {
	var p sdk.TasksEventsAfterRequest
	if err := decodeTask(req, &p); err != nil {
		return sdk.HostCallResponse{Error: "tasks_events_after: " + err.Error()}
	}
	return h.withLedger("tasks_events_after", func(l *TaskLedger) sdk.HostCallResponse {
		v, c, n, err := l.EventsAfter(p.ListID, p.Cursor, p.Limit)
		if err != nil {
			return sdk.HostCallResponse{Error: err.Error()}
		}
		return taskResult(sdk.TaskEventsResponse{Events: v, Cursor: c, NextCursor: n})
	})
}

func (h *Host) notifyTask(listID string, event sdk.TaskEvent) error {
	if h.AgentBridge() == nil {
		return nil
	}
	to := event.ActorAgentID
	if event.TaskID != "" {
		if t, err := h.taskLedgerSnapshot().Get(listID, event.TaskID); err == nil {
			if t.AssigneeAgentID != "" {
				to = t.AssigneeAgentID
			} else if t.OwnerAgentID != "" {
				to = t.OwnerAgentID
			}
		}
	}
	if to == "" {
		return nil
	}
	b, _ := json.Marshal(sdk.TaskEventEnvelope{EventID: event.EventID, ListID: event.ListID, TaskID: event.TaskID, AttemptID: event.AttemptID, Event: event.Event, Version: event.Version, ActorAgentID: event.ActorAgentID, Snapshot: event.Snapshot, Reference: event.EventID})
	if err := h.AgentBridge().Deliver(to, sdk.Message{Role: sdk.RoleUser, Content: string(b)}, true); err != nil {
		h.logger.Warn("extension: task notification failed", "event_id", event.EventID, "err", err)
	}
	return nil
}
