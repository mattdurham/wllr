package main

import (
	"testing"
	"time"
)

func TestNewPlanAssignsStepDefaults(t *testing.T) {
	plan, err := newPlan("plan-1", "Ship it", "description", "content", []PlanStep{{Title: "Test"}}, time.Unix(1, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	if plan.Status != planActive || plan.Steps[0].ID != "step-1" || plan.Steps[0].Status != stepPending {
		t.Fatalf("unexpected defaults: %+v", plan)
	}
}

func TestNewPlanRejectsInvalidInput(t *testing.T) {
	if _, err := newPlan("plan-1", " ", "", "", nil, time.Now()); err == nil {
		t.Fatal("expected empty title to fail")
	}
	if _, err := newPlan("plan-1", "title", "", "", []PlanStep{{}}, time.Now()); err == nil {
		t.Fatal("expected empty step title to fail")
	}
}

func TestSortedPlansIsDeterministic(t *testing.T) {
	now := time.Unix(10, 0).UTC()
	plans := map[string]*Plan{
		"b": {ID: "b", UpdatedAt: now},
		"a": {ID: "a", UpdatedAt: now},
	}
	sorted := sortedPlans(plans)
	if sorted[0].ID != "a" || sorted[1].ID != "b" {
		t.Fatalf("unexpected order: %s, %s", sorted[0].ID, sorted[1].ID)
	}
}

func TestIncompleteStepsAndCompletionGate(t *testing.T) {
	plan := &Plan{Steps: []PlanStep{{ID: "one", Status: stepCompleted}, {ID: "two", Status: stepBlocked}}}
	missing := incompleteSteps(plan)
	if len(missing) != 1 || missing[0] != "two" {
		t.Fatalf("unexpected incomplete steps: %#v", missing)
	}
	plan.Steps[1].Status = stepCompleted
	if got := incompleteSteps(plan); len(got) != 0 {
		t.Fatalf("expected all steps complete, got %#v", got)
	}
}
