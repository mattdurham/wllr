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

func TestContextUsageFromResultEqualsOrExceedsEveryStepTotal(t *testing.T) {
	res := &fantasy.AgentResult{
		Steps: []fantasy.StepResult{
			{Response: fantasy.Response{Usage: fantasy.Usage{InputTokens: 1_000, CacheCreationTokens: 15_000}}},
			{
				Response: fantasy.Response{
					Usage: fantasy.Usage{InputTokens: 200, CacheReadTokens: 19_000, CacheCreationTokens: 100},
				},
			},
			{Response: fantasy.Response{Usage: fantasy.Usage{InputTokens: 150, CacheReadTokens: 500}}},
		},
		TotalUsage: fantasy.Usage{InputTokens: 1_350, CacheReadTokens: 19_500, CacheCreationTokens: 15_100},
	}

	got := contextUsageFromResult(res)
	gotTotal := promptTokenTotal(got)
	for i, step := range res.Steps {
		if want := promptTokenTotal(step.Usage); gotTotal < want {
			t.Fatalf("selected total %d is below step %d total %d; turn-end ctx would tick down", gotTotal, i, want)
		}
	}
}

func TestContextUsageFromResultFallsBackToTotalForEmptyResult(t *testing.T) {
	res := &fantasy.AgentResult{TotalUsage: fantasy.Usage{InputTokens: 120, OutputTokens: 15}}

	got := contextUsageFromResult(res)
	if got != res.TotalUsage {
		t.Fatalf("context usage = %+v, want total usage %+v", got, res.TotalUsage)
	}
}

// A cache-warm step reports a tiny raw InputTokens while its true prompt size is
// the largest of the turn. Selecting on raw InputTokens picked the cache-cold
// first step and made ctx undercount at turn end.
func TestContextUsageFromResultPrefersLargestPromptTotalOverRawInput(t *testing.T) {
	res := &fantasy.AgentResult{
		Steps: []fantasy.StepResult{
			{Response: fantasy.Response{Usage: fantasy.Usage{InputTokens: 1_000, CacheCreationTokens: 15_000}}},
			{
				Response: fantasy.Response{
					Usage: fantasy.Usage{InputTokens: 200, CacheReadTokens: 19_000, CacheCreationTokens: 100},
				},
			},
		},
		TotalUsage: fantasy.Usage{InputTokens: 1_200, CacheReadTokens: 19_000, CacheCreationTokens: 15_100},
	}

	got := contextUsageFromResult(res)
	if got.InputTokens != 200 || got.CacheReadTokens != 19_000 {
		t.Fatalf("context usage = %+v, want cache-warm step {InputTokens:200 CacheReadTokens:19000}", got)
	}
}
