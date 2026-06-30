//go:build !wasip1

package main

import (
	"fmt"
	"sync"
	"testing"
)

// newList builds a TaskList with the given tasks for claim tests.
func newList(tasks ...*Task) *TaskList {
	tl := &TaskList{ID: "list-1", Tasks: make(map[string]*Task, len(tasks))}
	for _, t := range tasks {
		tl.Tasks[t.ID] = t
	}
	return tl
}

func TestClaimNext_PicksLowestPending(t *testing.T) {
	tl := newList(
		&Task{ID: "task-3", Status: "pending"},
		&Task{ID: "task-1", Status: "pending"},
		&Task{ID: "task-2", Status: "pending"},
	)
	got := claimNext(tl, "w1")
	if got == nil || got.ID != "task-1" {
		t.Fatalf("claimNext picked %v, want task-1", got)
	}
	if got.Status != "in_progress" || got.Assignee != "w1" {
		t.Errorf("claimed task status=%q assignee=%q, want in_progress/w1", got.Status, got.Assignee)
	}
}

func TestClaimNext_SkipsNonPending(t *testing.T) {
	tl := newList(
		&Task{ID: "task-1", Status: "in_progress", Assignee: "other"},
		&Task{ID: "task-2", Status: "completed"},
		&Task{ID: "task-3", Status: "pending"},
	)
	got := claimNext(tl, "w1")
	if got == nil || got.ID != "task-3" {
		t.Fatalf("claimNext picked %v, want task-3 (only pending)", got)
	}
}

func TestClaimNext_RespectsDependencies(t *testing.T) {
	// task-2 depends on task-1, which is still pending → task-2 not claimable.
	// task-1 is the only eligible task.
	tl := newList(
		&Task{ID: "task-1", Status: "pending"},
		&Task{ID: "task-2", Status: "pending", Dependencies: []string{"task-1"}},
	)
	got := claimNext(tl, "w1")
	if got == nil || got.ID != "task-1" {
		t.Fatalf("claimNext picked %v, want task-1 (task-2 blocked by dep)", got)
	}

	// task-2 still blocked (task-1 now in_progress, not completed).
	if got := claimNext(tl, "w2"); got != nil {
		t.Fatalf("claimNext picked %v, want nil (task-2 dep not completed)", got)
	}

	// Complete task-1 → task-2 becomes claimable.
	tl.Tasks["task-1"].Status = "completed"
	if got := claimNext(tl, "w2"); got == nil || got.ID != "task-2" {
		t.Fatalf("claimNext picked %v, want task-2 (dep now satisfied)", got)
	}
}

func TestClaimNext_NoneAvailable(t *testing.T) {
	tl := newList(
		&Task{ID: "task-1", Status: "completed"},
		&Task{ID: "task-2", Status: "in_progress"},
	)
	if got := claimNext(tl, "w1"); got != nil {
		t.Fatalf("claimNext picked %v, want nil (nothing pending)", got)
	}
}

func TestClaimNext_EmptyList(t *testing.T) {
	if got := claimNext(newList(), "w1"); got != nil {
		t.Fatalf("claimNext on empty list = %v, want nil", got)
	}
}

// TestClaimNext_ConcurrentNoDoubleAssignment exercises the REAL shipped
// find-and-claim logic (claimNext) under concurrency, holding tl.mu exactly as
// handleTasksClaim does. Two workers drain a shared list; no task may be
// assigned twice and every task must end up claimed exactly once.
func TestClaimNext_ConcurrentNoDoubleAssignment(t *testing.T) {
	const nTasks = 50
	tl := &TaskList{ID: "list-1", Tasks: make(map[string]*Task, nTasks)}
	for i := 1; i <= nTasks; i++ {
		id := fmt.Sprintf("task-%d", i)
		tl.Tasks[id] = &Task{ID: id, Status: "pending"}
	}

	var mu sync.Mutex
	assignees := make(map[string]string)
	double := false

	// claimUnderLock mirrors handleTasksClaim's locking discipline exactly.
	claimUnderLock := func(agentID string) *Task {
		tl.mu.Lock()
		defer tl.mu.Unlock()
		return claimNext(tl, agentID)
	}

	var wg sync.WaitGroup
	for _, worker := range []string{"w1", "w2", "w3"} {
		wg.Add(1)
		go func(agentID string) {
			defer wg.Done()
			for {
				claimed := claimUnderLock(agentID)
				if claimed == nil {
					return
				}
				mu.Lock()
				if _, seen := assignees[claimed.ID]; seen {
					double = true
				}
				assignees[claimed.ID] = agentID
				if claimed.Assignee != agentID {
					t.Errorf("task %s assignee=%q, want %q", claimed.ID, claimed.Assignee, agentID)
				}
				mu.Unlock()
			}
		}(worker)
	}
	wg.Wait()

	if double {
		t.Error("a task was claimed by more than one worker")
	}
	if len(assignees) != nTasks {
		t.Errorf("claimed %d distinct tasks, want %d", len(assignees), nTasks)
	}
	for id, tk := range tl.Tasks {
		if tk.Status != "in_progress" || tk.Assignee == "" {
			t.Errorf("task %s left status=%q assignee=%q after drain", id, tk.Status, tk.Assignee)
		}
	}
}

func TestTaskIDNum(t *testing.T) {
	cases := []struct {
		id     string
		want   int
		wantOK bool
	}{
		{"task-1", 1, true},
		{"task-42", 42, true},
		{"task-0", 0, true},
		{"task-", 0, false},
		{"list-1", 0, false},
		{"task-abc", 0, false},
		{"", 0, false},
	}
	for _, c := range cases {
		got, ok := taskIDNum(c.id)
		if got != c.want || ok != c.wantOK {
			t.Errorf("taskIDNum(%q) = (%d,%v), want (%d,%v)", c.id, got, ok, c.want, c.wantOK)
		}
	}
}

func TestSortTaskIDs_NumericOrder(t *testing.T) {
	ids := []string{"task-10", "task-2", "task-1", "task-21"}
	sortTaskIDs(ids)
	want := []string{"task-1", "task-2", "task-10", "task-21"}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("sortTaskIDs = %v, want %v", ids, want)
		}
	}
}

func TestSortTaskIDs_UnparseableSortAfterNumeric(t *testing.T) {
	ids := []string{"zeta", "task-2", "alpha", "task-1"}
	sortTaskIDs(ids)
	want := []string{"task-1", "task-2", "alpha", "zeta"}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("sortTaskIDs = %v, want %v", ids, want)
		}
	}
}

func TestDependenciesSatisfied(t *testing.T) {
	completed := map[string]bool{"task-1": true, "task-2": true}

	if !dependenciesSatisfied(&Task{Dependencies: nil}, completed) {
		t.Error("no dependencies must be satisfied")
	}
	if !dependenciesSatisfied(&Task{Dependencies: []string{"task-1"}}, completed) {
		t.Error("satisfied dependency must report true")
	}
	if dependenciesSatisfied(&Task{Dependencies: []string{"task-1", "task-3"}}, completed) {
		t.Error("unmet dependency must report false")
	}
}
