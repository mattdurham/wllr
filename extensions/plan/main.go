//go:build wasip1

package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

const planStoreKey = "plan_state"

var (
	planMu     sync.RWMutex
	state      = emptyPlanState()
	stateReady bool
)

func init() {
	registerPlanTool("plan_create", "Create a durable plan with optional ordered steps.", `{"type":"object","properties":{"title":{"type":"string"},"description":{"type":"string"},"content":{"type":"string"},"agent_id":{"type":"string","description":"Agent creating the plan (provenance)"},"steps":{"type":"array","items":{"type":"object","properties":{"id":{"type":"string"},"title":{"type":"string"},"description":{"type":"string"},"acceptance_checks":{"type":"array","items":{"type":"string"}}},"required":["title"]}}},"required":["title"]}`)
	registerPlanTool("plan_get", "Retrieve a plan by ID.", `{"type":"object","properties":{"id":{"type":"string"}},"required":["id"]}`)
	registerPlanTool("plan_list", "List plans, optionally filtered by status.", `{"type":"object","properties":{"status":{"type":"string","enum":["active","paused","completed","archived"]}}}`)
	registerPlanTool("plan_update", "Update plan metadata or content. Supplied empty strings clear fields. Pass expected_version to fail if another agent changed the plan (optimistic concurrency).", `{"type":"object","properties":{"id":{"type":"string"},"title":{"type":"string"},"description":{"type":"string"},"status":{"type":"string","enum":["active","paused","completed","archived"]},"content":{"type":"string"},"expected_version":{"type":"integer"},"agent_id":{"type":"string","description":"Agent making the update (provenance)"}},"required":["id"]}`)
	registerPlanTool("plan_step_update", "Update a plan step's status, notes, or description. Pass expected_version to fail if another agent changed the plan (optimistic concurrency).", `{"type":"object","properties":{"plan_id":{"type":"string"},"step_id":{"type":"string"},"status":{"type":"string","enum":["pending","in_progress","completed","blocked"]},"description":{"type":"string"},"notes":{"type":"string"},"expected_version":{"type":"integer"},"agent_id":{"type":"string","description":"Agent making the update (provenance)"}},"required":["plan_id","step_id"]}`)
	registerPlanTool("plan_evidence", "Record evidence for a plan step.", `{"type":"object","properties":{"plan_id":{"type":"string"},"step_id":{"type":"string"},"evidence":{"type":"string"}},"required":["plan_id","step_id","evidence"]}`)
	registerPlanTool("plan_assign", "Assign an agent to a plan step (who is responsible for it). Pass expected_version to fail if another agent changed the plan.", `{"type":"object","properties":{"plan_id":{"type":"string"},"step_id":{"type":"string"},"assignee":{"type":"string","description":"Agent ID responsible for completing this step"},"expected_version":{"type":"integer"}},"required":["plan_id","step_id","assignee"]}`)
	registerPlanTool("plan_checkpoint", "Persist the current plan state and return a resumable checkpoint.", `{"type":"object","properties":{"plan_id":{"type":"string"}}}`)
	registerPlanTool("plan_complete", "Complete a plan only when every step is completed and each completed step has evidence. Supply override_reason to force completion past unresolved steps or missing evidence (recorded, requires explicit intent).", `{"type":"object","properties":{"plan_id":{"type":"string"},"override_reason":{"type":"string","description":"Explicit reason to force completion despite unresolved steps or missing evidence"}},"required":["plan_id"]}`)
	registerPlanTool("plan_focus", "Return the active plan and its next incomplete step.", `{"type":"object","properties":{}}`)
	registerPlanTool("plan_set_active", "Set the active plan by ID.", `{"type":"object","properties":{"id":{"type":"string"}},"required":["id"]}`)

	OnSessionStart(func() { loadPlanState() })
	OnShutdown(func(_ string) { savePlanState() })
	OnToolCall(handlePlanTool)

	initPlanWidget()
}

func registerPlanTool(name, description, schema string) {
	RegisterToolWithOutput(name, description, json.RawMessage(schema), json.RawMessage(`{"type":"object"}`))
}

func handlePlanTool(_ string, name string, input json.RawMessage) (string, bool) {
	loadPlanState()
	switch name {
	case "plan_create":
		return planCreate(input)
	case "plan_get":
		return planGet(input)
	case "plan_list":
		return planList(input)
	case "plan_update":
		return planUpdate(input)
	case "plan_step_update":
		return planStepUpdate(input)
	case "plan_evidence":
		return planEvidence(input)
	case "plan_assign":
		return planAssign(input)
	case "plan_checkpoint":
		return planCheckpoint(input)
	case "plan_complete":
		return planComplete(input)
	case "plan_focus":
		return planFocus()
	case "plan_set_active":
		return planSetActive(input)
	default:
		return "", false
	}
}

func loadPlanState() {
	planMu.Lock()
	if stateReady {
		planMu.Unlock()
		return
	}
	stateReady = true
	planMu.Unlock()
	// Source of truth is the durable on-disk snapshot (survives restart).
	// StoreGet remains as a fallback for hosts without WASI disk access.
	loadPlanStateFromDisk()
}

func savePlanState() error {
	planMu.RLock()
	data, err := json.Marshal(state)
	planMu.RUnlock()
	if err != nil {
		return err
	}
	// Durable snapshot (survives restart) plus in-process store fallback.
	// savePlanStateToDisk marshals again; pass the already-marshaled data by
	// writing it directly to avoid re-marshaling under no lock.
	if derr := persistSnapshot(data); derr != nil {
		return derr
	}
	StoreSet(planStoreKey, string(data))
	return nil
}

// savePlanStateLocked persists a snapshot while planMu is already held.
func savePlanStateLocked() error {
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	if derr := savePlanStateToDisk(); derr != nil {
		return derr
	}
	StoreSet(planStoreKey, string(data))
	return nil
}

func result(value any) (string, bool) {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf(`{"error":%q}`, err.Error()), true
	}
	return string(data), false
}

func failure(message string) (string, bool) { return result(map[string]string{"error": message}) }

func planCreate(input json.RawMessage) (string, bool) {
	var req struct {
		Title       string     `json:"title"`
		Description string     `json:"description"`
		Content     string     `json:"content"`
		AgentID     string     `json:"agent_id"`
		Steps       []PlanStep `json:"steps"`
	}
	if err := json.Unmarshal(input, &req); err != nil {
		return failure("invalid parameters: " + err.Error())
	}
	now := time.Now().UTC()
	id := fmt.Sprintf("plan-%d", now.UnixNano())
	plan, err := newPlan(id, req.Title, req.Description, req.Content, req.Steps, now)
	if err != nil {
		return failure(err.Error())
	}
	plan.CreatedBy = req.AgentID
	plan.UpdatedBy = req.AgentID
	planMu.Lock()
	state.Plans[id] = plan
	state.ActiveID = id
	planMu.Unlock()
	if err := savePlanState(); err != nil {
		return failure("could not persist plan: " + err.Error())
	}
	return result(map[string]any{"id": id, "title": plan.Title, "status": plan.Status, "active": true, "message": "plan created"})
}

func planGet(input json.RawMessage) (string, bool) {
	var req struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(input, &req); err != nil || strings.TrimSpace(req.ID) == "" {
		return failure("id is required")
	}
	planMu.RLock()
	plan, ok := state.Plans[req.ID]
	data, marshalErr := json.Marshal(plan)
	planMu.RUnlock()
	if !ok {
		return failure("plan not found: " + req.ID)
	}
	if marshalErr != nil {
		return failure("could not encode plan: " + marshalErr.Error())
	}
	return string(data), false
}

func planList(input json.RawMessage) (string, bool) {
	var req struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(input, &req); err != nil {
		return failure("invalid parameters: " + err.Error())
	}
	if req.Status != "" && !validPlanStatus(req.Status) {
		return failure("invalid plan status: " + req.Status)
	}
	planMu.RLock()
	all := sortedPlans(state.Plans)
	plans := make([]*Plan, 0, len(all))
	for _, plan := range all {
		if req.Status == "" || plan.Status == req.Status {
			plans = append(plans, plan)
		}
	}
	out, err := result(map[string]any{"plans": plans, "count": len(plans)})
	planMu.RUnlock()
	return out, err
}

func planUpdate(input json.RawMessage) (string, bool) {
	var req struct {
		ID              string  `json:"id"`
		Title           *string `json:"title"`
		Description     *string `json:"description"`
		Status          *string `json:"status"`
		Content         *string `json:"content"`
		ExpectedVersion *int    `json:"expected_version"`
		AgentID         string  `json:"agent_id"`
	}
	if err := json.Unmarshal(input, &req); err != nil || strings.TrimSpace(req.ID) == "" {
		return failure("id is required")
	}
	planMu.Lock()
	defer planMu.Unlock()
	plan, ok := state.Plans[req.ID]
	if !ok {
		return failure("plan not found: " + req.ID)
	}
	if req.ExpectedVersion != nil && *req.ExpectedVersion != plan.Version {
		return failure(fmt.Sprintf("plan version conflict: expected %d, got %d", *req.ExpectedVersion, plan.Version))
	}
	if req.Title != nil {
		if strings.TrimSpace(*req.Title) == "" {
			return failure("title cannot be empty")
		}
		plan.Title = strings.TrimSpace(*req.Title)
	}
	if req.Status != nil {
		if !validPlanStatus(*req.Status) {
			return failure("invalid plan status: " + *req.Status)
		}
		plan.Status = *req.Status
	}
	if req.Description != nil {
		plan.Description = *req.Description
	}
	if req.Content != nil {
		plan.Content = *req.Content
	}
	plan.UpdatedAt = time.Now().UTC()
	plan.Version++
	plan.UpdatedBy = req.AgentID
	if err := savePlanStateLocked(); err != nil {
		return failure("could not persist plan: " + err.Error())
	}
	return result(map[string]any{"id": plan.ID, "title": plan.Title, "status": plan.Status, "version": plan.Version, "message": "plan updated"})
}

func planStepUpdate(input json.RawMessage) (string, bool) {
	var req struct {
		PlanID          string  `json:"plan_id"`
		StepID          string  `json:"step_id"`
		Status          *string `json:"status"`
		Description     *string `json:"description"`
		Notes           *string `json:"notes"`
		ExpectedVersion *int    `json:"expected_version"`
		AgentID         string  `json:"agent_id"`
	}
	if err := json.Unmarshal(input, &req); err != nil || req.PlanID == "" || req.StepID == "" {
		return failure("plan_id and step_id are required")
	}
	planMu.Lock()
	defer planMu.Unlock()
	plan, ok := state.Plans[req.PlanID]
	if !ok {
		return failure("plan not found: " + req.PlanID)
	}
	if req.ExpectedVersion != nil && *req.ExpectedVersion != plan.Version {
		return failure(fmt.Sprintf("plan version conflict: expected %d, got %d", *req.ExpectedVersion, plan.Version))
	}
	for i := range plan.Steps {
		step := &plan.Steps[i]
		if step.ID != req.StepID {
			continue
		}
		if req.Status != nil {
			if !validStepStatus(*req.Status) {
				return failure("invalid step status: " + *req.Status)
			}
			step.Status = *req.Status
		}
		if req.Description != nil {
			step.Description = *req.Description
		}
		if req.Notes != nil {
			step.Notes = *req.Notes
		}
		// Provenance: record which agent touched this step and count attempts on
		// transitions toward completion.
		if req.AgentID != "" {
			step.AgentID = req.AgentID
		}
		if req.Status != nil && *req.Status == stepCompleted && step.Status != stepCompleted {
			step.Attempts++
		}
		step.UpdatedAt = time.Now().UTC()
		plan.UpdatedAt = step.UpdatedAt
		plan.Version++
		plan.UpdatedBy = req.AgentID
		if err := savePlanStateLocked(); err != nil {
			return failure("could not persist plan: " + err.Error())
		}
		return result(step)
	}
	return failure("step not found: " + req.StepID)
}

func planEvidence(input json.RawMessage) (string, bool) {
	var req struct {
		PlanID   string `json:"plan_id"`
		StepID   string `json:"step_id"`
		Evidence string `json:"evidence"`
	}
	if err := json.Unmarshal(input, &req); err != nil || req.PlanID == "" || req.StepID == "" || strings.TrimSpace(req.Evidence) == "" {
		return failure("plan_id, step_id, and evidence are required")
	}
	planMu.Lock()
	defer planMu.Unlock()
	plan, ok := state.Plans[req.PlanID]
	if !ok {
		return failure("plan not found: " + req.PlanID)
	}
	for i := range plan.Steps {
		if plan.Steps[i].ID == req.StepID {
			plan.Steps[i].Evidence = append(plan.Steps[i].Evidence, req.Evidence)
			plan.Steps[i].UpdatedAt = time.Now().UTC()
			plan.UpdatedAt = plan.Steps[i].UpdatedAt
			if err := savePlanStateLocked(); err != nil {
				return failure("could not persist plan: " + err.Error())
			}
			return result(plan.Steps[i])
		}
	}
	return failure("step not found: " + req.StepID)
}

func planAssign(input json.RawMessage) (string, bool) {
	var req struct {
		PlanID          string `json:"plan_id"`
		StepID          string `json:"step_id"`
		Assignee        string `json:"assignee"`
		ExpectedVersion *int   `json:"expected_version"`
	}
	if err := json.Unmarshal(input, &req); err != nil || req.PlanID == "" || req.StepID == "" || strings.TrimSpace(req.Assignee) == "" {
		return failure("plan_id, step_id, and assignee are required")
	}
	planMu.Lock()
	defer planMu.Unlock()
	plan, ok := state.Plans[req.PlanID]
	if !ok {
		return failure("plan not found: " + req.PlanID)
	}
	if req.ExpectedVersion != nil && *req.ExpectedVersion != plan.Version {
		return failure(fmt.Sprintf("plan version conflict: expected %d, got %d", *req.ExpectedVersion, plan.Version))
	}
	for i := range plan.Steps {
		step := &plan.Steps[i]
		if step.ID != req.StepID {
			continue
		}
		step.Assignee = strings.TrimSpace(req.Assignee)
		step.UpdatedAt = time.Now().UTC()
		plan.UpdatedAt = step.UpdatedAt
		plan.Version++
		if err := savePlanStateLocked(); err != nil {
			return failure("could not persist plan: " + err.Error())
		}
		return result(step)
	}
	return failure("step not found: " + req.StepID)
}

func planCheckpoint(input json.RawMessage) (string, bool) {
	var req struct {
		PlanID string `json:"plan_id"`
	}
	if err := json.Unmarshal(input, &req); err != nil {
		return failure("invalid parameters: " + err.Error())
	}
	planMu.RLock()
	id := state.ActiveID
	if req.PlanID != "" {
		id = req.PlanID
	}
	_, ok := state.Plans[id]
	activeID := state.ActiveID
	planMu.RUnlock()
	if id == "" || !ok {
		return failure("plan not found")
	}
	if err := savePlanState(); err != nil {
		return failure("could not persist checkpoint: " + err.Error())
	}
	return result(map[string]any{"plan_id": id, "active_id": activeID, "checkpointed": true})
}

func planComplete(input json.RawMessage) (string, bool) {
	var req struct {
		PlanID         string `json:"plan_id"`
		OverrideReason string `json:"override_reason"`
	}
	if err := json.Unmarshal(input, &req); err != nil || req.PlanID == "" {
		return failure("plan_id is required")
	}
	planMu.Lock()
	defer planMu.Unlock()
	plan, ok := state.Plans[req.PlanID]
	if !ok {
		return failure("plan not found: " + req.PlanID)
	}
	if missing := incompleteSteps(plan); len(missing) > 0 {
		if strings.TrimSpace(req.OverrideReason) == "" {
			out, _ := result(map[string]any{"completed": false, "incomplete_steps": missing, "reason": "steps incomplete"})
			return out, true
		}
		plan.CompletionOverride = req.OverrideReason
	}
	// Evidence gate: every completed step must carry evidence unless overridden.
	missingEv := missingEvidence(plan)
	if len(missingEv) > 0 && strings.TrimSpace(req.OverrideReason) == "" {
		out, _ := result(map[string]any{"completed": false, "missing_evidence": missingEv, "reason": "evidence required"})
		return out, true
	}
	plan.Status = planCompleted
	plan.UpdatedAt = time.Now().UTC()
	plan.Version++
	if err := savePlanStateLocked(); err != nil {
		return failure("could not persist plan: " + err.Error())
	}
	override := ""
	if plan.CompletionOverride != "" {
		override = plan.CompletionOverride
	}
	return result(map[string]any{"completed": true, "plan_id": plan.ID, "status": plan.Status, "override": override})
}

func planFocus() (string, bool) {
	planMu.RLock()
	defer planMu.RUnlock()
	if state.ActiveID == "" {
		return result(map[string]any{"active": false, "message": "no active plan"})
	}
	plan, ok := state.Plans[state.ActiveID]
	if !ok {
		return result(map[string]any{"active": false, "message": "active plan not found"})
	}
	response := map[string]any{"active": true, "plan": plan}
	for _, step := range plan.Steps {
		if step.Status != stepCompleted {
			response["next_step"] = step
			break
		}
	}
	return result(response)
}

func planSetActive(input json.RawMessage) (string, bool) {
	var req struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(input, &req); err != nil || req.ID == "" {
		return failure("id is required")
	}
	planMu.Lock()
	if _, ok := state.Plans[req.ID]; !ok {
		planMu.Unlock()
		return failure("plan not found: " + req.ID)
	}
	state.ActiveID = req.ID
	planMu.Unlock()
	if err := savePlanState(); err != nil {
		return failure("could not persist active plan: " + err.Error())
	}
	return result(map[string]any{"id": req.ID, "active": true})
}

func main() {}
