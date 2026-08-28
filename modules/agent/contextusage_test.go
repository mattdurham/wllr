package agent

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"testing"

	"charm.land/fantasy"
)

func TestContextUsageFromResultUsesPeakStep(t *testing.T) {
	res := &fantasy.AgentResult{
		Steps: []fantasy.StepResult{
			{Response: fantasy.Response{Usage: fantasy.Usage{InputTokens: 90, OutputTokens: 10}}},
			{Response: fantasy.Response{Usage: fantasy.Usage{InputTokens: 240, OutputTokens: 20}}},
			{Response: fantasy.Response{Usage: fantasy.Usage{InputTokens: 180, OutputTokens: 30}}},
		},
		TotalUsage: fantasy.Usage{InputTokens: 510, OutputTokens: 60},
	}

	got := contextUsageFromResult(res)
	if got.InputTokens != 240 || got.OutputTokens != 20 {
		t.Fatalf("context usage = %+v, want peak step usage {InputTokens:240 OutputTokens:20}", got)
	}
}

func TestContextUsageFromResultFallsBackToTotalForEmptyResult(t *testing.T) {
	res := &fantasy.AgentResult{TotalUsage: fantasy.Usage{InputTokens: 120, OutputTokens: 15}}

	got := contextUsageFromResult(res)
	if got != res.TotalUsage {
		t.Fatalf("context usage = %+v, want total usage %+v", got, res.TotalUsage)
	}
}
