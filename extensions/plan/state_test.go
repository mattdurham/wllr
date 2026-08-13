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

func TestNewPlanInitialisesVersion(t *testing.T) {
	plan, err := newPlan("plan-1", "Title", "", "", []PlanStep{{Title: "S"}}, time.Unix(1, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	if plan.Version != 1 {
		t.Fatalf("expected initial version 1, got %d", plan.Version)
	}
}

func TestMissingEvidenceGate(t *testing.T) {
	plan := &Plan{Steps: []PlanStep{
		{ID: "a", Status: stepCompleted, Evidence: []string{"test passed"}},
		{ID: "b", Status: stepCompleted, Evidence: nil},
		{ID: "c", Status: stepPending},
	}}
	got := missingEvidence(plan)
	if len(got) != 1 || got[0] != "b" {
		t.Fatalf("expected only completed step b without evidence, got %#v", got)
	}
	// Pending/blocked steps are not flagged by the evidence gate; they are
	// already caught by the incomplete-steps gate.
}

func TestMissingEvidenceAllowsEvidence(t *testing.T) {
	plan := &Plan{Steps: []PlanStep{
		{ID: "a", Status: stepCompleted, Evidence: []string{"x"}},
	}}
	if got := missingEvidence(plan); len(got) != 0 {
		t.Fatalf("expected no missing evidence, got %#v", got)
	}
}

// Assignment is distinct from provenance: Assignee records who is responsible
// for a step, AgentID records who last touched it.
func TestAssigneeIsDistinctFromProvenance(t *testing.T) {
	plan := &Plan{Steps: []PlanStep{
		{ID: "s1", Title: "Implement", Status: stepPending, Assignee: "main/coder", AgentID: "main/planner"},
	}}
	if plan.Steps[0].Assignee != "main/coder" {
		t.Fatalf("expected assignee main/coder, got %q", plan.Steps[0].Assignee)
	}
	if plan.Steps[0].AgentID != "main/planner" {
		t.Fatalf("expected provenance main/planner, got %q", plan.Steps[0].AgentID)
	}
}

func TestNextStepAssignee(t *testing.T) {
	plan := &Plan{Steps: []PlanStep{
		{ID: "a", Status: stepCompleted},
		{ID: "b", Status: stepPending, Assignee: "main/gpu"},
		{ID: "c", Status: stepPending},
	}}
	if got := nextStepAssignee(plan); got != "main/gpu" {
		t.Fatalf("expected main/gpu, got %q", got)
	}
	// No assignee on the next step -> empty, not panic.
	plan.Steps[1].Assignee = ""
	if got := nextStepAssignee(plan); got != "" {
		t.Fatalf("expected empty assignee, got %q", got)
	}
}
