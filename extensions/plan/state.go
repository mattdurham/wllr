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
	return &Plan{ID: id, Title: title, Description: description, Content: content, Status: planActive, Steps: steps, CreatedAt: now, UpdatedAt: now}, nil
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
