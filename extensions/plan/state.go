package main

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	planActive    = "active"
	planPaused    = "paused"
	planCompleted = "completed"
	planArchived  = "archived"

	stepPending    = "pending"
	stepInProgress = "in_progress"
	stepCompleted  = "completed"
	stepBlocked    = "blocked"
)

type PlanStep struct {
	ID               string    `json:"id"`
	Title            string    `json:"title"`
	Description      string    `json:"description,omitempty"`
	Status           string    `json:"status"`
	AcceptanceChecks []string  `json:"acceptance_checks,omitempty"`
	Evidence         []string  `json:"evidence,omitempty"`
	Notes            string    `json:"notes,omitempty"`
	UpdatedAt        time.Time `json:"updated_at"`
	// Assignee is the agent responsible for completing this step, set via
	// plan_assign. Distinct from AgentID provenance (last toucher).
	Assignee string `json:"assignee,omitempty"`
	// Provenance: which agent last touched this step and how many times it was
	// attempted. Attempts increment on each status transition toward completion.
	AgentID  string `json:"agent_id,omitempty"`
	Attempts int    `json:"attempts"`
}

type Plan struct {
	ID          string     `json:"id"`
	Title       string     `json:"title"`
	Description string     `json:"description,omitempty"`
	Status      string     `json:"status"`
	Content     string     `json:"content,omitempty"`
	Steps       []PlanStep `json:"steps,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	// Version increments on every mutation. plan_update/plan_step_update accept
	// an optional expected_version and fail on mismatch so concurrent agents
	// cannot silently overwrite each other's changes.
	Version int `json:"version"`
	// Provenance: who created the plan and the last agent to update it.
	CreatedBy string `json:"created_by,omitempty"`
	UpdatedBy string `json:"updated_by,omitempty"`
	// CompletionOverride records the explicit reason a plan was force-completed
	// despite unresolved steps or missing evidence. Empty unless overridden.
	CompletionOverride string `json:"completion_override,omitempty"`
}

type planState struct {
	Version  int              `json:"version"`
	ActiveID string           `json:"active_id,omitempty"`
	Plans    map[string]*Plan `json:"plans"`
}

func emptyPlanState() planState { return planState{Version: 1, Plans: map[string]*Plan{}} }

func validPlanStatus(status string) bool {
	switch status {
	case planActive, planPaused, planCompleted, planArchived:
		return true
	default:
		return false
	}
}

func validStepStatus(status string) bool {
	switch status {
	case stepPending, stepInProgress, stepCompleted, stepBlocked:
		return true
	default:
		return false
	}
}

func newPlan(id, title, description, content string, steps []PlanStep, now time.Time) (*Plan, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return nil, fmt.Errorf("title is required")
	}
	for i := range steps {
		steps[i].Title = strings.TrimSpace(steps[i].Title)
		if steps[i].Title == "" {
			return nil, fmt.Errorf("step %d: title is required", i+1)
		}
		if steps[i].ID == "" {
			steps[i].ID = fmt.Sprintf("step-%d", i+1)
		}
		if !validStepStatus(steps[i].Status) {
			steps[i].Status = stepPending
		}
		steps[i].UpdatedAt = now
	}
	return &Plan{ID: id, Title: title, Description: description, Content: content, Status: planActive, Steps: steps, CreatedAt: now, UpdatedAt: now, Version: 1}, nil
}

func sortedPlans(plans map[string]*Plan) []*Plan {
	out := make([]*Plan, 0, len(plans))
	for _, plan := range plans {
		out = append(out, plan)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].UpdatedAt.Equal(out[j].UpdatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].UpdatedAt.Before(out[j].UpdatedAt)
	})
	return out
}

func incompleteSteps(plan *Plan) []string {
	var ids []string
	for _, step := range plan.Steps {
		if step.Status != stepCompleted {
			ids = append(ids, step.ID)
		}
	}
	return ids
}

// missingEvidence returns the IDs of completed steps that carry no evidence.
// This is the evidence gate: completion must not proceed while a completed step
// lacks proof unless an explicit override reason is supplied.
func missingEvidence(plan *Plan) []string {
	var ids []string
	for _, step := range plan.Steps {
		if step.Status == stepCompleted && len(step.Evidence) == 0 {
			ids = append(ids, step.ID)
		}
	}
	return ids
}

// nextStepTitle returns the title of the first incomplete step for display.
func nextStepTitle(plan *Plan) string {
	for _, step := range plan.Steps {
		if step.Status != stepCompleted {
			return step.Title
		}
	}
	return ""
}

// nextStepAssignee returns the assignee of the first incomplete step, or "" if
// none is assigned. Used by the widget footer and plan_focus to show who owns
// the next step.
func nextStepAssignee(plan *Plan) string {
	for _, step := range plan.Steps {
		if step.Status != stepCompleted {
			return step.Assignee
		}
	}
	return ""
}
