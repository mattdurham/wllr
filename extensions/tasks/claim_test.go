//go:build !wasip1

package main

import "testing"

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
