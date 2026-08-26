package extension

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/mattdurham/wllr/modules/sdk"
)

func TestTaskLedgerLifecycleAndRecovery(t *testing.T) {
	dir := t.TempDir()
	l, err := NewTaskLedger(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	list, err := l.CreateList(sdk.TasklistCreateRequest{Name: "work"})
	if err != nil {
		t.Fatal(err)
	}
	task, err := l.CreateTask(sdk.TasksCreateRequest{ListID: list.ListID, Title: "one"})
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := l.Claim(sdk.TasksClaimRequest{ListID: list.ListID, TaskID: task.TaskID, AgentID: "agent"})
	if err != nil {
		t.Fatal(err)
	}
	if claimed.AttemptID == "" || claimed.Status != sdk.TaskInProgress {
		t.Fatalf("bad claim: %#v", claimed)
	}
	if _, err := l.Report(sdk.TasksReportRequest{ListID: list.ListID, TaskID: task.TaskID, AttemptID: claimed.AttemptID, AgentID: "agent", Status: sdk.TaskCompleted, Result: []byte(`{"ok":true}`)}); err != nil {
		t.Fatal(err)
	}
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	l2, err := OpenTaskLedger(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer l2.Close()
	got, err := l2.Get(list.ListID, task.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != sdk.TaskCompleted || got.TaskID != task.TaskID {
		t.Fatalf("recovery lost task: %#v", got)
	}
	events, _, _, err := l2.EventsAfter(list.ListID, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 4 || events[0].EventID == "" {
		t.Fatalf("events: %#v", events)
	}
}

func TestTaskLedgerConcurrentClaimAndDependency(t *testing.T) {
	l, err := NewTaskLedger(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	list, _ := l.CreateList(sdk.TasklistCreateRequest{Name: "x"})
	task, _ := l.CreateTask(sdk.TasksCreateRequest{ListID: list.ListID, Title: "x"})
	var wg sync.WaitGroup
	var mu sync.Mutex
	successes := 0
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := l.Claim(sdk.TasksClaimRequest{ListID: list.ListID, TaskID: task.TaskID, AgentID: "a"}); err == nil {
				mu.Lock()
				successes++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if successes != 1 {
		t.Fatalf("claims succeeded: %d", successes)
	}
}

func TestTaskLedgerTruncatedTailAndEarlierCorruption(t *testing.T) {
	dir := t.TempDir()
	l, _ := NewTaskLedger(dir, nil)
	list, _ := l.CreateList(sdk.TasklistCreateRequest{Name: "x"})
	l.Close()
	p := filepath.Join(dir, "tasks.jsonl")
	f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0o600)
	f.WriteString(`{"schema":1,"sequence":99`)
	f.Close()
	if recovered, err := OpenTaskLedger(dir, nil); err != nil {
		t.Fatal(err)
	} else {
		recovered.Close()
	}
	b, _ := os.ReadFile(p)
	lines := strings.Split(string(b), "\n")
	if len(lines) < 2 {
		t.Fatal("journal unexpectedly empty")
	}
	good := lines[0] + "\n{"
	os.WriteFile(p, []byte(good+"\n"+strings.Join(lines[1:], "\n")), 0o600)
	if _, err := OpenTaskLedger(dir, nil); err == nil {
		t.Fatal("expected earlier corruption error")
	}
	_ = list
}
