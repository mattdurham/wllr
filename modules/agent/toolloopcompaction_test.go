package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"charm.land/fantasy"
)

type toolLoopTestLM struct {
	*compactTestLM
	providerCalls int
	summaryCalls  int
	finalPrompt   []fantasy.Message
}

func (m *toolLoopTestLM) Stream(_ context.Context, call fantasy.Call) (fantasy.StreamResponse, error) {
	if len(call.Tools) == 0 {
		m.summaryCalls++
		return m.compactTestLM.Stream(context.Background(), call)
	}
	m.providerCalls++
	if m.providerCalls == 1 {
		return func(yield func(fantasy.StreamPart) bool) {
			if !yield(fantasy.StreamPart{
				Type: fantasy.StreamPartTypeToolCall,
				ID:   "call-1", ToolCallName: "large_tool", ToolCallInput: `{}`,
			}) {
				return
			}
			yield(fantasy.StreamPart{
				Type:         fantasy.StreamPartTypeFinish,
				FinishReason: fantasy.FinishReasonToolCalls,
				Usage:        fantasy.Usage{InputTokens: 190_000, OutputTokens: 100},
			})
		}, nil
	}
	m.finalPrompt = call.Prompt
	return func(yield func(fantasy.StreamPart) bool) {
		if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextDelta, Delta: "done"}) {
			return
		}
		yield(fantasy.StreamPart{
			Type:         fantasy.StreamPartTypeFinish,
			FinishReason: fantasy.FinishReasonStop,
			Usage:        fantasy.Usage{InputTokens: 1_000, OutputTokens: 1},
		})
	}, nil
}

func TestStreamTurnCompactsGrowingToolTranscript(t *testing.T) {
	lm := &toolLoopTestLM{compactTestLM: &compactTestLM{response: "task state summary"}}
	tool := fantasy.NewAgentTool("large_tool", "returns a large result",
		func(context.Context, map[string]any, fantasy.ToolCall) (fantasy.ToolResponse, error) {
			return fantasy.NewTextResponse(strings.Repeat("tool data ", 6_000)), nil
		})
	a := &Agent{}
	fa := fantasy.NewAgent(lm, fantasy.WithTools(tool))
	var observed []CompactionResult
	got, _, err := a.streamTurn(context.Background(), fa, lm, nil, "do the task", nil, nil, nil,
		262_144, CompactConfig{Enabled: true, ThresholdPct: 0.80}, func(result CompactionResult) {
			observed = append(observed, result)
		})
	if err != nil {
		t.Fatalf("streamTurn: %v", err)
	}
	if got != "done" || lm.providerCalls != 2 || lm.summaryCalls != 1 || len(observed) != 1 {
		t.Fatalf(
			"response=%q providerCalls=%d summaryCalls=%d compactions=%d",
			got,
			lm.providerCalls,
			lm.summaryCalls,
			len(observed),
		)
	}
	encoded, err := json.Marshal(lm.finalPrompt)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), "task state summary") || len(encoded) > 10_000 {
		t.Fatalf("final prompt was not compacted: %d bytes", len(encoded))
	}
}

func TestToolLoopCompactorSummarizesBeforeNextStep(t *testing.T) {
	var observed []CompactionResult
	c := newToolLoopCompactor(&compactTestLM{response: "task and tool result summary"}, 262_144,
		CompactConfig{Enabled: true, ThresholdPct: 0.80}, func(result CompactionResult) {
			observed = append(observed, result)
		})
	first := fantasy.StepResult{
		Response: fantasy.Response{Usage: fantasy.Usage{InputTokens: 190_000}},
		Messages: []fantasy.Message{
			fantasy.NewUserMessage(strings.Repeat("tool result", 5_000)),
		},
	}
	original := []fantasy.Message{fantasy.NewUserMessage("original task"), first.Messages[0]}
	_, prepared, err := c.prepare(context.Background(), fantasy.PrepareStepFunctionOptions{
		Steps: []fantasy.StepResult{first}, Messages: original,
	})
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if len(prepared.Messages) != 1 || len(observed) != 1 {
		t.Fatalf("messages = %d, compactions = %d; want one each", len(prepared.Messages), len(observed))
	}
	if observed[0].Trigger != CompactionTriggerToolLoop {
		t.Fatalf("trigger = %q", observed[0].Trigger)
	}
	if got := prepared.Messages[0].Content[0].(fantasy.TextPart).Text; !strings.Contains(
		got,
		"task and tool result summary",
	) {
		t.Fatalf("prepared message omits summary: %q", got)
	}

	second := fantasy.StepResult{
		Response: fantasy.Response{Usage: fantasy.Usage{InputTokens: 1_000}},
		Messages: []fantasy.Message{fantasy.NewUserMessage("new tool result")},
	}
	_, prepared, err = c.prepare(context.Background(), fantasy.PrepareStepFunctionOptions{
		Steps: []fantasy.StepResult{first, second}, Messages: original,
	})
	if err != nil {
		t.Fatalf("prepare next step: %v", err)
	}
	if len(prepared.Messages) != 2 || len(observed) != 1 {
		t.Fatalf(
			"messages = %d, compactions = %d; want summary plus new result and one compaction",
			len(prepared.Messages),
			len(observed),
		)
	}
	if got := prepared.Messages[1].Content[0].(fantasy.TextPart).Text; got != "new tool result" {
		t.Fatalf("new result = %q", got)
	}
}

func TestToolLoopCompactorStopsOnSummaryFailure(t *testing.T) {
	c := newToolLoopCompactor(&compactTestLM{}, 262_144,
		CompactConfig{Enabled: true, ThresholdPct: 0.80}, nil)
	_, _, err := c.prepare(context.Background(), fantasy.PrepareStepFunctionOptions{
		Steps:    []fantasy.StepResult{{Response: fantasy.Response{Usage: fantasy.Usage{InputTokens: 220_000}}}},
		Messages: []fantasy.Message{fantasy.NewUserMessage("task")},
	})
	if err == nil || !strings.Contains(err.Error(), "tool-loop context compaction failed") {
		t.Fatalf("error = %v, want explicit compaction failure", err)
	}
}

func TestToolLoopCompactorUsesMessageSizeWhenProviderOmitsUsage(t *testing.T) {
	c := newToolLoopCompactor(&compactTestLM{response: "summary"}, 100_000,
		CompactConfig{Enabled: true, ThresholdPct: 0.80}, nil)
	_, prepared, err := c.prepare(context.Background(), fantasy.PrepareStepFunctionOptions{
		Steps:    []fantasy.StepResult{{}},
		Messages: []fantasy.Message{fantasy.NewUserMessage(strings.Repeat("x", 170_000))},
	})
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if len(prepared.Messages) != 1 || c.summary != "summary" {
		t.Fatalf("usage-less endpoint did not compact: messages=%d summary=%q", len(prepared.Messages), c.summary)
	}
}
