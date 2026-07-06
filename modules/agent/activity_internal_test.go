package agent

import "testing"

func TestAgentActivity_ToolCompletionClearsActiveTool(t *testing.T) {
	a := &Agent{}
	a.markTurnStart()
	a.markToolCall("call-1", "read_file")

	if got := a.Activity().ActiveToolName; got != "read_file" {
		t.Fatalf("ActiveToolName before completion = %q, want read_file", got)
	}

	a.MarkToolCallDone("call-1", "read_file")

	activity := a.Activity()
	if activity.ActiveToolName != "" {
		t.Fatalf("ActiveToolName after completion = %q, want empty", activity.ActiveToolName)
	}
	if activity.ActiveToolCallID != "" {
		t.Fatalf("ActiveToolCallID after completion = %q, want empty", activity.ActiveToolCallID)
	}
	if activity.LastToolDoneAt.IsZero() {
		t.Fatal("LastToolDoneAt should be set")
	}
	if activity.LastToolName != "read_file" {
		t.Fatalf("LastToolName = %q, want read_file", activity.LastToolName)
	}
}
