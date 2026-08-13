//go:build wasip1

package main

import (
	"fmt"
	"strings"
)

// Widget area constants. The plan extension owns a compact sidebar widget that
// shows the active plan: objective, current step, progress, blockers, and
// completion state. It is re-rendered on a 1s tick so it stays in sync with
// tool-driven plan changes.
const (
	planAreaID  = "plan-widget"
	planRootID  = "plan-widget-root"
	planTitleID = "plan-widget-title"
	planBodyID  = "plan-widget-body"
	planFootID  = "plan-widget-foot"
)

// initPlanWidget registers the /plan slash command (summary via Modal) and the
// compact sidebar widget. Requires the "ui" permission in the plan manifest.
func initPlanWidget() {
	RegisterCommand("plan", "Show the active plan, current step, and progress")

	UICreateArea(planAreaID, "sidebar", 1, "0", "", "", "")

	OnCommand("plan", func(_ []string) {
		planMu.RLock()
		defer planMu.RUnlock()
		if state.ActiveID == "" {
			Modal("No active plan. Use plan_create to start one.")
			return
		}
		plan, ok := state.Plans[state.ActiveID]
		if !ok {
			Modal("Active plan not found.")
			return
		}
		var sb strings.Builder
		sb.WriteString("Plan: " + plan.Title + "\n")
		sb.WriteString("Status: " + plan.Status + "\n")
		sb.WriteString("\n")
		done, total := 0, len(plan.Steps)
		for _, step := range plan.Steps {
			mark := "  "
			switch step.Status {
			case stepCompleted:
				mark = "✓ "
				done++
			case stepInProgress:
				mark = "▶ "
			case stepBlocked:
				mark = "✗ "
			default:
				mark = "○ "
			}
			sb.WriteString(mark + step.Title + "\n")
			if step.Status == stepBlocked && step.Notes != "" {
				sb.WriteString("    blocker: " + step.Notes + "\n")
			}
		}
		sb.WriteString("\n")
		sb.WriteString(fmt.Sprintf("Progress: %d/%d steps\n", done, total))
		if plan.CompletionOverride != "" {
			sb.WriteString("Override: " + plan.CompletionOverride + "\n")
		}
		Modal(sb.String())
	})

	OnTick(func() {
		renderPlanWidget()
	})
}

// renderPlanWidget redraws the sidebar widget from current plan state.
func renderPlanWidget() {
	planMu.RLock()
	if state.ActiveID == "" {
		planMu.RUnlock()
		UIPatch(planAreaID, OpSetRoot(UIVStack(planRootID, UIText(planTitleID, "No active plan"))))
		return
	}
	plan, ok := state.Plans[state.ActiveID]
	if !ok {
		planMu.RUnlock()
		UIPatch(planAreaID, OpSetRoot(UIVStack(planRootID, UIText(planTitleID, "Active plan not found"))))
		return
	}

	var body strings.Builder
	done, total := 0, len(plan.Steps)
	for _, step := range plan.Steps {
		mark := "○ "
		switch step.Status {
		case stepCompleted:
			mark = "✓ "
			done++
		case stepInProgress:
			mark = "▶ "
		case stepBlocked:
			mark = "✗ "
		}
		body.WriteString(mark + step.Title + "\n")
		if step.Assignee != "" {
			body.WriteString("  → " + step.Assignee + "\n")
		}
		if step.Status == stepBlocked && step.Notes != "" {
			body.WriteString("  blocker: " + step.Notes + "\n")
		}
	}
	title := plan.Title
	if title == "" {
		title = "(untitled)"
	}
	status := "status: " + plan.Status
	if plan.Status == planActive && done < total {
		s := nextStepTitle(plan)
		status += " · next: " + s
		if a := nextStepAssignee(plan); a != "" {
			status += " → " + a
		}
	}
	foot := fmt.Sprintf("%d/%d · %s", done, total, status)
	planMu.RUnlock()

	UIPatch(planAreaID,
		OpSetRoot(UIVStack(planRootID,
			UIText(planTitleID, title),
			UIText(planBodyID, body.String()),
			UIText(planFootID, foot),
		)),
	)
}
