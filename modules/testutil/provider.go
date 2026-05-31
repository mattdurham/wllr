// Package testutil provides test helpers for wllr tests, including fake
// fantasy.Provider and fantasy.LanguageModel implementations that stream
// preset responses without hitting any real API.
package testutil

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"context"
	"strings"

	"charm.land/fantasy"
)

// FakeLM is a fantasy.LanguageModel that streams preset text responses
// one word at a time and records every call it receives.

// one response string per call

// compile-time interface assertion
var _ fantasy.LanguageModel = (*FakeLM)(nil)

// Calls returns a snapshot of all recorded calls.
func (lm *FakeLM) Calls() []RecordedCall {
	lm.mu.Lock()
	defer lm.mu.Unlock()
	out := make([]RecordedCall, len(lm.calls))
	copy(out, lm.calls)
	return out
}

// LastCall returns the most recent recorded call.
// Panics if no calls have been made.
func (lm *FakeLM) LastCall() RecordedCall {
	lm.mu.Lock()
	defer lm.mu.Unlock()
	if len(lm.calls) == 0 {
		panic("testutil.FakeLM: LastCall called with no recorded calls")
	}
	return lm.calls[len(lm.calls)-1]
}

// nextResponse returns the response for the current call index, cycling back
// to the last response when all are exhausted.
func (lm *FakeLM) nextResponse() string {
	if len(lm.responses) == 0 {
		return ""
	}
	idx := lm.callIdx
	if idx >= len(lm.responses) {
		idx = len(lm.responses) - 1
	}
	lm.callIdx++
	return lm.responses[idx]
}

// recordCall extracts and stores a RecordedCall from a fantasy.Call.
func recordCall(call fantasy.Call) RecordedCall {
	rc := RecordedCall{}
	for _, msg := range call.Prompt {
		switch msg.Role {
		case fantasy.MessageRoleSystem:
			// Extract text from system message parts.
			for _, part := range msg.Content {
				if tp, ok := part.(fantasy.TextPart); ok {
					if rc.SystemPrompt != "" {
						rc.SystemPrompt += "\n"
					}
					rc.SystemPrompt += tp.Text
				}
			}
		case fantasy.MessageRoleUser, fantasy.MessageRoleAssistant, fantasy.MessageRoleTool:
			var sb strings.Builder
			for _, part := range msg.Content {
				if tp, ok := part.(fantasy.TextPart); ok {
					sb.WriteString(tp.Text)
				}
			}
			rc.Messages = append(rc.Messages, string(msg.Role)+": "+sb.String())
			if msg.Role == fantasy.MessageRoleUser {
				rc.Prompt = sb.String()
			}
		}
	}
	return rc
}

// Generate implements fantasy.LanguageModel (non-streaming).
// Returns the next preset response as a text content block.
func (lm *FakeLM) Generate(_ context.Context, call fantasy.Call) (*fantasy.Response, error) {
	lm.mu.Lock()
	rc := recordCall(call)
	lm.calls = append(lm.calls, rc)
	text := lm.nextResponse()
	lm.mu.Unlock()

	return &fantasy.Response{
		Content:      fantasy.ResponseContent{fantasy.TextContent{Text: text}},
		FinishReason: fantasy.FinishReasonStop,
		Usage: fantasy.Usage{
			InputTokens:  int64(len(rc.Prompt) / 4),
			OutputTokens: int64(len(text) / 4),
		},
	}, nil
}

// Stream implements fantasy.LanguageModel.
// If a scripted turn is available (added via SetScript), it is popped and
// emitted — text parts first, then tool call parts, then a finish part.
// When the script is exhausted, the next preset text response is emitted
// word-by-word as text deltas.
func (lm *FakeLM) Stream(ctx context.Context, call fantasy.Call) (fantasy.StreamResponse, error) {
	lm.mu.Lock()
	rc := recordCall(call)
	lm.calls = append(lm.calls, rc)

	// If a scripted turn is queued, pop it.
	var scripted *ScriptedTurn
	if len(lm.script) > 0 {
		turn := lm.script[0]
		lm.script = lm.script[1:]
		scripted = &turn
	}

	text := ""
	if scripted == nil {
		text = lm.nextResponse()
	} else if scripted.Text != "" {
		text = scripted.Text
	}
	lm.mu.Unlock()

	// Capture tool calls snapshot (nil-safe).
	var toolCalls []ScriptedToolCall
	if scripted != nil {
		toolCalls = scripted.ToolCalls
	}

	words := splitWords(text)

	return func(yield func(fantasy.StreamPart) bool) {
		// Emit text parts if there is text content.
		if len(words) > 0 {
			if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextStart, ID: "0"}) {
				return
			}
			for _, word := range words {
				select {
				case <-ctx.Done():
					return
				default:
				}
				if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextDelta, ID: "0", Delta: word}) {
					return
				}
			}
			if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextEnd, ID: "0"}) {
				return
			}
		}

		// Emit tool call parts (each as a single complete StreamPartTypeToolCall).
		for _, tc := range toolCalls {
			input := ""
			if len(tc.Input) > 0 {
				input = string(tc.Input)
			}
			if !yield(fantasy.StreamPart{
				Type:          fantasy.StreamPartTypeToolCall,
				ID:            tc.ID,
				ToolCallName:  tc.Name,
				ToolCallInput: input,
			}) {
				return
			}
		}

		// Finish part.
		yield(fantasy.StreamPart{
			Type:         fantasy.StreamPartTypeFinish,
			FinishReason: fantasy.FinishReasonStop,
			Usage: fantasy.Usage{
				InputTokens:  int64(len(rc.Prompt) / 4),
				OutputTokens: int64(len(text) / 4),
			},
		})
	}, nil
}

// GenerateObject implements fantasy.LanguageModel (stub, not used in tests).
func (lm *FakeLM) GenerateObject(_ context.Context, _ fantasy.ObjectCall) (*fantasy.ObjectResponse, error) {
	return &fantasy.ObjectResponse{}, nil
}

// StreamObject implements fantasy.LanguageModel (stub, not used in tests).
func (lm *FakeLM) StreamObject(_ context.Context, _ fantasy.ObjectCall) (fantasy.ObjectStreamResponse, error) {
	return func(yield func(fantasy.ObjectStreamPart) bool) {}, nil
}

// Provider implements fantasy.LanguageModel.
func (lm *FakeLM) Provider() string { return lm.provider }

// Model implements fantasy.LanguageModel.
func (lm *FakeLM) Model() string { return lm.modelID }

// splitWords splits text into tokens (words with their trailing space attached).
// "hello world" -> ["hello ", "world"]
// Empty string returns nil.
func splitWords(text string) []string {
	if text == "" {
		return nil
	}
	var parts []string
	remaining := text
	for {
		idx := strings.IndexByte(remaining, ' ')
		if idx < 0 {
			parts = append(parts, remaining)
			break
		}
		parts = append(parts, remaining[:idx+1])
		remaining = remaining[idx+1:]
		if remaining == "" {
			break
		}
	}
	return parts
}
