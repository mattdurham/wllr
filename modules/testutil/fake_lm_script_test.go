package testutil_test

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"context"
	"encoding/json"
	"testing"

	"charm.land/fantasy"
	"github.com/mattdurham/wllr/modules/testutil"
)

// collectStreamParts drains a fantasy.StreamResponse and returns all parts.
func collectStreamParts(t *testing.T, lm *testutil.FakeLM) []fantasy.StreamPart {
	t.Helper()
	ctx := context.Background()
	stream, err := lm.Stream(ctx, fantasy.Call{
		Prompt: fantasy.Prompt{
			{Role: fantasy.MessageRoleUser, Content: []fantasy.MessagePart{fantasy.TextPart{Text: "go"}}},
		},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	parts := make([]fantasy.StreamPart, 0, 16)
	for p := range stream {
		parts = append(parts, p)
	}
	return parts
}

// TestFakeLMEmitsToolCall verifies that a scripted turn with a tool call emits
// StreamPartTypeToolCall with the correct ToolCallName and ToolCallInput.
func TestFakeLMEmitsToolCall(t *testing.T) {
	lm := testutil.NewFakeLM()
	lm.SetScript([]testutil.ScriptedTurn{
		{
			ToolCalls: []testutil.ScriptedToolCall{
				{ID: "tc1", Name: "my_tool", Input: json.RawMessage(`{"x":1}`)},
			},
		},
	})

	parts := collectStreamParts(t, lm)

	var toolCallParts []fantasy.StreamPart
	for _, p := range parts {
		if p.Type == fantasy.StreamPartTypeToolCall {
			toolCallParts = append(toolCallParts, p)
		}
	}
	if len(toolCallParts) == 0 {
		t.Fatal("expected at least one StreamPartTypeToolCall part")
	}
	found := toolCallParts[0]
	if found.ToolCallName != "my_tool" {
		t.Errorf("ToolCallName: got %q, want %q", found.ToolCallName, "my_tool")
	}
	if found.ToolCallInput != `{"x":1}` {
		t.Errorf("ToolCallInput: got %q, want %q", found.ToolCallInput, `{"x":1}`)
	}
	if found.ID != "tc1" {
		t.Errorf("ID: got %q, want %q", found.ID, "tc1")
	}
}

// TestFakeLMMixedTextAndToolCall verifies that a scripted turn with both text
// and tool calls emits text parts followed by tool call parts, and a finish part.
func TestFakeLMMixedTextAndToolCall(t *testing.T) {
	lm := testutil.NewFakeLM()
	lm.SetScript([]testutil.ScriptedTurn{
		{
			Text: "thinking...",
			ToolCalls: []testutil.ScriptedToolCall{
				{ID: "tc1", Name: "do_thing", Input: json.RawMessage(`{}`)},
			},
		},
	})

	parts := collectStreamParts(t, lm)

	// Find indices of text delta and tool call parts.
	textIdx := -1
	toolIdx := -1
	finishIdx := -1
	for i, p := range parts {
		switch p.Type {
		case fantasy.StreamPartTypeTextDelta:
			if textIdx < 0 {
				textIdx = i
			}
		case fantasy.StreamPartTypeToolCall:
			if toolIdx < 0 {
				toolIdx = i
			}
		case fantasy.StreamPartTypeFinish:
			finishIdx = i
		}
	}

	if textIdx < 0 {
		t.Error("expected text delta parts — none found")
	}
	if toolIdx < 0 {
		t.Error("expected tool call part — none found")
	}
	if finishIdx < 0 {
		t.Error("expected finish part — none found")
	}
	// Text must appear before the tool call.
	if textIdx >= 0 && toolIdx >= 0 && textIdx > toolIdx {
		t.Errorf("text delta (idx %d) must appear before tool call (idx %d)", textIdx, toolIdx)
	}
	// Finish must be last.
	if finishIdx >= 0 && finishIdx != len(parts)-1 {
		t.Errorf("finish part (idx %d) must be last (len %d)", finishIdx, len(parts))
	}
}

// TestFakeLMScriptMultipleTurns verifies that SetScript pops turns in order:
// turn 1 emits tool call A, turn 2 emits tool call B.
func TestFakeLMScriptMultipleTurns(t *testing.T) {
	lm := testutil.NewFakeLM()
	lm.SetScript([]testutil.ScriptedTurn{
		{ToolCalls: []testutil.ScriptedToolCall{{ID: "t1", Name: "tool_a", Input: json.RawMessage(`{"n":1}`)}}},
		{ToolCalls: []testutil.ScriptedToolCall{{ID: "t2", Name: "tool_b", Input: json.RawMessage(`{"n":2}`)}}},
	})

	parts1 := collectStreamParts(t, lm)
	parts2 := collectStreamParts(t, lm)

	nameOf := func(parts []fantasy.StreamPart) string {
		for _, p := range parts {
			if p.Type == fantasy.StreamPartTypeToolCall {
				return p.ToolCallName
			}
		}
		return ""
	}

	if got := nameOf(parts1); got != "tool_a" {
		t.Errorf("turn 1: expected tool_a, got %q", got)
	}
	if got := nameOf(parts2); got != "tool_b" {
		t.Errorf("turn 2: expected tool_b, got %q", got)
	}
}

// TestFakeLMScriptExhausted verifies that once all scripted turns are consumed,
// subsequent calls fall back to the preset text responses.
func TestFakeLMScriptExhausted(t *testing.T) {
	lm := testutil.NewFakeLMWithResponses("fallback text")
	lm.SetScript([]testutil.ScriptedTurn{
		{Text: "scripted"},
	})

	// First call: uses scripted turn.
	parts1 := collectStreamParts(t, lm)
	var text1 string
	for _, p := range parts1 {
		if p.Type == fantasy.StreamPartTypeTextDelta {
			text1 += p.Delta
		}
	}
	if text1 != "scripted" {
		t.Errorf("turn 1: expected %q, got %q", "scripted", text1)
	}

	// Second call: script exhausted, falls back to preset responses.
	parts2 := collectStreamParts(t, lm)
	var text2 string
	for _, p := range parts2 {
		if p.Type == fantasy.StreamPartTypeTextDelta {
			text2 += p.Delta
		}
	}
	if text2 != "fallback text" {
		t.Errorf("turn 2 (fallback): expected %q, got %q", "fallback text", text2)
	}
}

// TestFakeLMScriptMultipleToolCalls verifies that a scripted turn with multiple
// tool calls emits all of them as separate StreamPartTypeToolCall parts.
func TestFakeLMScriptMultipleToolCalls(t *testing.T) {
	lm := testutil.NewFakeLM()
	lm.SetScript([]testutil.ScriptedTurn{
		{
			ToolCalls: []testutil.ScriptedToolCall{
				{ID: "t1", Name: "alpha", Input: json.RawMessage(`{"a":1}`)},
				{ID: "t2", Name: "beta", Input: json.RawMessage(`{"b":2}`)},
				{ID: "t3", Name: "gamma", Input: json.RawMessage(`{"c":3}`)},
			},
		},
	})

	parts := collectStreamParts(t, lm)

	var names []string
	for _, p := range parts {
		if p.Type == fantasy.StreamPartTypeToolCall {
			names = append(names, p.ToolCallName)
		}
	}
	if len(names) != 3 {
		t.Fatalf("expected 3 tool call parts, got %d: %v", len(names), names)
	}
	want := []string{"alpha", "beta", "gamma"}
	for i, n := range names {
		if n != want[i] {
			t.Errorf("tool call %d: got %q, want %q", i, n, want[i])
		}
	}
}
