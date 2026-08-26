package extension

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"bufio"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/mattdurham/wllr/modules/sdk"
)

var ErrTaskNotFound = errors.New("task not found")
var ErrTaskListNotFound = errors.New("task list not found")
var ErrTaskVersion = errors.New("task version conflict")
var ErrTaskInvalid = errors.New("invalid task transition")

type taskJournalRecord struct {
	Schema   int                        `json:"schema"`
	Sequence int64                      `json:"sequence"`
	Lists    map[string]sdk.TaskList    `json:"lists"`
	Tasks    map[string]sdk.TaskRecord  `json:"tasks"`
	Events   map[string][]sdk.TaskEvent `json:"events"`
	Digest   string                     `json:"digest"`
}
type taskSnapshot struct {
	Schema   int                        `json:"schema"`
	Sequence int64                      `json:"sequence"`
	Lists    map[string]sdk.TaskList    `json:"lists"`
	Tasks    map[string]sdk.TaskRecord  `json:"tasks"`
	Events   map[string][]sdk.TaskEvent `json:"events"`
}

type TaskLedger struct {
	mu       sync.Mutex
	dir      string
	journal  *os.File
	sequence int64
	lists    map[string]sdk.TaskList
	tasks    map[string]sdk.TaskRecord
	events   map[string][]sdk.TaskEvent
	notify   func(string, sdk.TaskEvent) error
	closed   bool
}

func NewTaskLedger(dir string, notify func(string, sdk.TaskEvent) error) (*TaskLedger, error) {
	return openTaskLedger(dir, notify, false)
}
func OpenTaskLedger(dir string, notify func(string, sdk.TaskEvent) error) (*TaskLedger, error) {
	return openTaskLedger(dir, notify, true)
}
func openTaskLedger(dir string, notify func(string, sdk.TaskEvent) error, recover bool) (*TaskLedger, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create task ledger directory: %w", err)
	}
	l := &TaskLedger{dir: dir, lists: map[string]sdk.TaskList{}, tasks: map[string]sdk.TaskRecord{}, events: map[string][]sdk.TaskEvent{}, notify: notify}
	if err := l.loadSnapshot(); err != nil {
		return nil, err
	}
	if err := l.replay(); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(dir, "tasks.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open task journal: %w", err)
	}
	l.journal = f
	_ = recover
	return l, nil
}

func (l *TaskLedger) loadSnapshot() error {
	b, err := os.ReadFile(filepath.Join(l.dir, "tasks.snapshot.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read task snapshot: %w", err)
	}
	var s taskSnapshot
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("corrupt task snapshot: %w", err)
	}
	if s.Schema != 1 {
		return fmt.Errorf("unsupported task snapshot schema %d", s.Schema)
	}
	l.sequence = s.Sequence
	l.lists = s.Lists
	l.tasks = s.Tasks
	l.events = s.Events
	return nil
}

func (l *TaskLedger) replay() (err error) {
	f, err := os.Open(filepath.Join(l.dir, "tasks.jsonl"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open task journal: %w", err)
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close task journal: %w", closeErr)
		}
	}()
	r := bufio.NewReader(f)
	lineNo := 0
	for {
		line, readErr := r.ReadBytes('\n')
		if len(line) > 0 {
			lineNo++
			if line[len(line)-1] == '\n' {
				line = line[:len(line)-1]
			}
			var rec taskJournalRecord
			if err := json.Unmarshal(line, &rec); err != nil {
				if readErr == io.EOF {
					return nil
				}
				return fmt.Errorf("corrupt task journal line %d: %w", lineNo, err)
			}
			digest := rec.Digest
			rec.Digest = ""
			raw, _ := json.Marshal(rec)
			sum := sha256.Sum256(raw)
			if digest != hex.EncodeToString(sum[:]) {
				return fmt.Errorf("corrupt task journal checksum line %d", lineNo)
			}
			if rec.Schema != 1 {
				return fmt.Errorf("unsupported task journal schema %d", rec.Schema)
			}
			if rec.Sequence > l.sequence {
				l.sequence = rec.Sequence
				l.lists = rec.Lists
				l.tasks = rec.Tasks
				l.events = rec.Events
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return fmt.Errorf("read task journal: %w", readErr)
		}
	}
	return nil
}

func opaqueID(prefix string) (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return prefix + "_" + hex.EncodeToString(b), nil
}
func (l *TaskLedger) commitLocked() (err error) {
	rec := taskJournalRecord{1, l.sequence, l.lists, l.tasks, l.events, ""}
	raw, _ := json.Marshal(rec)
	sum := sha256.Sum256(raw)
	rec.Digest = hex.EncodeToString(sum[:])
	line, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	line = append(line, '\n')
	if _, err = l.journal.Write(line); err != nil {
		return fmt.Errorf("write task journal: %w", err)
	}
	if err := l.journal.Sync(); err != nil {
		return fmt.Errorf("flush task journal: %w", err)
	}
	s := taskSnapshot{1, l.sequence, l.lists, l.tasks, l.events}
	sb, _ := json.Marshal(s)
	f, err := os.CreateTemp(l.dir, "snapshot-*.tmp")
	if err != nil {
		return err
	}
	name := f.Name()
	defer func() {
		if removeErr := os.Remove(name); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) && err == nil {
			err = fmt.Errorf("remove temporary task snapshot: %w", removeErr)
		}
	}()
	if _, err = f.Write(sb); err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("write task snapshot: %w", err)
	}
	return os.Rename(name, filepath.Join(l.dir, "tasks.snapshot.json"))
}
func (l *TaskLedger) mutate(listID string, event sdk.TaskEvent, fn func()) error {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return errors.New("task ledger is closed")
	}
	eventID, err := opaqueID("event")
	if err != nil {
		l.mu.Unlock()
		return err
	}
	event.EventID = eventID
	l.sequence++
	event.Version = l.sequence
	event.CreatedAt = time.Now().UTC()
	l.events[listID] = append(l.events[listID], event)
	fn()
	if list, ok := l.lists[listID]; ok {
		list.Cursor = int64(len(l.events[listID]))
		list.Version++
		l.lists[listID] = list
	}
	if err := l.commitLocked(); err != nil {
		l.mu.Unlock()
		return err
	}
	l.mu.Unlock()
	if l.notify != nil {
		if err := l.notify(listID, event); err != nil {
			return fmt.Errorf("task notification: %w", err)
		}
	}
	return nil
}

func (l *TaskLedger) CreateList(req sdk.TasklistCreateRequest) (sdk.TaskList, error) {
	id, err := opaqueID("list")
	if err != nil {
		return sdk.TaskList{}, err
	}
	out := sdk.TaskList{ListID: id, Name: req.Name, Description: req.Description, OwnerAgentID: req.OwnerAgentID, Version: 1}
	err = l.mutate(id, sdk.TaskEvent{ListID: id, Event: "list_created", ActorAgentID: req.OwnerAgentID}, func() { l.lists[id] = out })
	return out, err
}
func (l *TaskLedger) CreateTask(req sdk.TasksCreateRequest) (sdk.TaskRecord, error) {
	l.mu.Lock()
	_, ok := l.lists[req.ListID]
	l.mu.Unlock()
	if !ok {
		return sdk.TaskRecord{}, ErrTaskListNotFound
	}
	id, err := opaqueID("task")
	if err != nil {
		return sdk.TaskRecord{}, err
	}
	now := time.Now().UTC()
	mode := req.WorkspaceMode
	if mode == "" {
		mode = sdk.TaskWorkspaceShared
	}
	out := sdk.TaskRecord{TaskID: id, ListID: req.ListID, ParentTaskID: req.ParentTaskID, OwnerAgentID: req.OwnerAgentID, AssigneeAgentID: req.AssigneeAgentID, Title: req.Title, Description: req.Description, Status: sdk.TaskPending, Priority: req.Priority, DependsOn: append([]string(nil), req.DependsOn...), WorkspaceMode: mode, CreatedAt: now, UpdatedAt: now, Version: 1}
	err = l.mutate(req.ListID, sdk.TaskEvent{ListID: req.ListID, TaskID: id, Event: "task_created", ActorAgentID: req.OwnerAgentID}, func() { l.tasks[id] = out })
	return out, err
}
func (l *TaskLedger) Get(listID, taskID string) (sdk.TaskRecord, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	t, ok := l.tasks[taskID]
	if !ok || t.ListID != listID {
		return sdk.TaskRecord{}, ErrTaskNotFound
	}
	return t, nil
}
func (l *TaskLedger) Claim(req sdk.TasksClaimRequest) (sdk.TaskRecord, error) {
	l.mu.Lock()
	t, ok := l.tasks[req.TaskID]
	if !ok || t.ListID != req.ListID {
		l.mu.Unlock()
		return sdk.TaskRecord{}, ErrTaskNotFound
	}
	if req.ExpectedVersion != 0 && t.Version != req.ExpectedVersion {
		l.mu.Unlock()
		return sdk.TaskRecord{}, ErrTaskVersion
	}
	if t.Status != sdk.TaskPending {
		l.mu.Unlock()
		return sdk.TaskRecord{}, ErrTaskInvalid
	}
	for _, id := range t.DependsOn {
		if d, ok := l.tasks[id]; !ok || d.Status != sdk.TaskCompleted {
			l.mu.Unlock()
			return sdk.TaskRecord{}, fmt.Errorf("%w: dependency %s", ErrTaskInvalid, id)
		}
	}
	attempt, err := opaqueID("attempt")
	if err != nil {
		l.mu.Unlock()
		return sdk.TaskRecord{}, err
	}
	t.Status = sdk.TaskInProgress
	t.AttemptID = attempt
	t.AssigneeAgentID = req.AgentID
	t.UpdatedAt = time.Now().UTC()
	t.Version++
	l.tasks[t.TaskID] = t
	l.sequence++
	eventID, err := opaqueID("event")
	if err != nil {
		l.mu.Unlock()
		return sdk.TaskRecord{}, err
	}
	ev := sdk.TaskEvent{EventID: eventID, ListID: t.ListID, TaskID: t.TaskID, AttemptID: attempt, Event: "task_claimed", Version: l.sequence, ActorAgentID: req.AgentID, CreatedAt: time.Now().UTC()}
	l.events[req.ListID] = append(l.events[req.ListID], ev)
	err = l.commitLocked()
	l.mu.Unlock()
	if err == nil && l.notify != nil {
		err = l.notify(req.ListID, ev)
	}
	return t, err
}
func (l *TaskLedger) UpdateCAS(req sdk.TasksUpdateRequest) (sdk.TaskRecord, error) {
	l.mu.Lock()
	t, ok := l.tasks[req.TaskID]
	if !ok || t.ListID != req.ListID {
		l.mu.Unlock()
		return sdk.TaskRecord{}, ErrTaskNotFound
	}
	if t.Version != req.ExpectedVersion {
		l.mu.Unlock()
		return sdk.TaskRecord{}, ErrTaskVersion
	}
	if req.Title != nil {
		t.Title = *req.Title
	}
	if req.Description != nil {
		t.Description = *req.Description
	}
	if req.Priority != nil {
		t.Priority = *req.Priority
	}
	if req.AssigneeAgentID != nil {
		t.AssigneeAgentID = *req.AssigneeAgentID
	}
	if req.WorkspaceMode != nil {
		t.WorkspaceMode = *req.WorkspaceMode
	}
	t.UpdatedAt = time.Now().UTC()
	t.Version++
	l.tasks[t.TaskID] = t
	l.sequence++
	eventID, err := opaqueID("event")
	if err != nil {
		l.mu.Unlock()
		return sdk.TaskRecord{}, err
	}
	ev := sdk.TaskEvent{EventID: eventID, ListID: t.ListID, TaskID: t.TaskID, Event: "task_updated", Version: l.sequence, ActorAgentID: req.AgentID, CreatedAt: time.Now().UTC()}
	l.events[req.ListID] = append(l.events[req.ListID], ev)
	err = l.commitLocked()
	l.mu.Unlock()
	if err == nil && l.notify != nil {
		err = l.notify(req.ListID, ev)
	}
	return t, err
}
func (l *TaskLedger) Report(req sdk.TasksReportRequest) (sdk.TaskRecord, error) {
	l.mu.Lock()
	t, ok := l.tasks[req.TaskID]
	if !ok || t.ListID != req.ListID {
		l.mu.Unlock()
		return sdk.TaskRecord{}, ErrTaskNotFound
	}
	if t.Status != sdk.TaskInProgress || t.AttemptID != req.AttemptID {
		l.mu.Unlock()
		return sdk.TaskRecord{}, ErrTaskInvalid
	}
	switch req.Status {
	case sdk.TaskCompleted:
		if len(req.Result) == 0 {
			l.mu.Unlock()
			return sdk.TaskRecord{}, errors.New("completed task requires result")
		}
	case sdk.TaskBlocked, sdk.TaskFailed, sdk.TaskCancelled:
		if req.Reason == "" && req.Error == "" {
			l.mu.Unlock()
			return sdk.TaskRecord{}, errors.New("terminal task requires reason or error")
		}
	default:
		l.mu.Unlock()
		return sdk.TaskRecord{}, ErrTaskInvalid
	}
	t.Status = req.Status
	t.Result = append(json.RawMessage(nil), req.Result...)
	t.Error = req.Error
	t.Reason = req.Reason
	t.UpdatedAt = time.Now().UTC()
	t.Version++
	l.tasks[t.TaskID] = t
	l.sequence++
	eventID, err := opaqueID("event")
	if err != nil {
		l.mu.Unlock()
		return sdk.TaskRecord{}, err
	}
	ev := sdk.TaskEvent{EventID: eventID, ListID: t.ListID, TaskID: t.TaskID, AttemptID: t.AttemptID, Event: "task_reported", Version: l.sequence, ActorAgentID: req.AgentID, CreatedAt: time.Now().UTC()}
	l.events[req.ListID] = append(l.events[req.ListID], ev)
	err = l.commitLocked()
	l.mu.Unlock()
	if err == nil && l.notify != nil {
		err = l.notify(req.ListID, ev)
	}
	return t, err
}
func (l *TaskLedger) List(listID string, cursor int64, limit int) ([]sdk.TaskRecord, int64, int64, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, ok := l.lists[listID]; !ok {
		return nil, 0, 0, ErrTaskListNotFound
	}
	all := []sdk.TaskRecord{}
	for _, t := range l.tasks {
		if t.ListID == listID {
			all = append(all, t)
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].TaskID < all[j].TaskID })
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	start := int(cursor)
	if start > len(all) {
		start = len(all)
	}
	end := start + limit
	if end > len(all) {
		end = len(all)
	}
	next := int64(end)
	if end == len(all) {
		next = 0
	}
	return all[start:end], int64(start), next, nil
}
func (l *TaskLedger) EventsAfter(listID string, cursor int64, limit int) ([]sdk.TaskEvent, int64, int64, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, ok := l.lists[listID]; !ok {
		return nil, 0, 0, ErrTaskListNotFound
	}
	all := l.events[listID]
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	start := int(cursor)
	if start > len(all) {
		start = len(all)
	}
	end := start + limit
	if end > len(all) {
		end = len(all)
	}
	next := int64(end)
	if end == len(all) {
		next = 0
	}
	return append([]sdk.TaskEvent(nil), all[start:end]...), int64(start), next, nil
}
func (l *TaskLedger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return nil
	}
	l.closed = true
	if l.journal != nil {
		return l.journal.Close()
	}
	return nil
}
