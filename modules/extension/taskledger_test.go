package extension

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
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

func TestTaskLedgerDigestIsIndependentOfFieldOrder(t *testing.T) {
	dir := t.TempDir()
	l, err := NewTaskLedger(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	list, err := l.CreateList(sdk.TasklistCreateRequest{Name: "work"})
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "tasks.jsonl")
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var env struct {
		Body json.RawMessage `json:"body"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(raw))), &env); err != nil {
		t.Fatal(err)
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(env.Body, &body); err != nil {
		t.Fatal(err)
	}
	// Re-emit the body with the keys in an order that no Go struct layout
	// produces. JSON objects are unordered, so replay must still verify: the
	// digest covers the stored bytes, not a re-serialization of a fixed field
	// order. A reader that re-marshaled the parsed struct would reject this.
	order := []string{"tasks", "events", "lists", "sequence", "schema"}
	parts := make([]string, 0, len(order))
	for _, k := range order {
		parts = append(parts, fmt.Sprintf("%q:%s", k, body[k]))
	}
	bodyJSON := "{" + strings.Join(parts, ",") + "}"
	sum := sha256.Sum256([]byte(bodyJSON))
	line := fmt.Sprintf("{\"body\":%s,\"digest\":%q}\n", bodyJSON, hex.EncodeToString(sum[:]))
	if err := os.WriteFile(p, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	l2, err := OpenTaskLedger(dir, nil)
	if err != nil {
		t.Fatalf("reordered journal rejected: %v", err)
	}
	defer l2.Close()
	if _, _, _, err := l2.List(list.ListID, 0, 100); err != nil {
		t.Fatalf("reordered journal lost state: %v", err)
	}
}

func TestTaskLedgerRejectsTamperedBody(t *testing.T) {
	dir := t.TempDir()
	l, err := NewTaskLedger(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.CreateList(sdk.TasklistCreateRequest{Name: "x"}); err != nil {
		t.Fatal(err)
	}
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "tasks.jsonl")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	tampered := strings.Replace(string(b), `"sequence":1`, `"sequence":2`, 1)
	if tampered == string(b) {
		t.Fatal("test fixture did not change the body")
	}
	if err := os.WriteFile(p, []byte(tampered), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenTaskLedger(dir, nil); err == nil {
		t.Fatal("expected checksum failure on tampered body")
	}
}
