// Package testutil provides test helpers for wllr tests, including fake
// fantasy.Provider and fantasy.LanguageModel implementations that stream
// preset responses without hitting any real API.
package testutil

import (
	"context"
	"strings"
	"sync"

	"charm.land/fantasy"
)

// RecordedCall holds the inputs captured from a single Stream or Generate call.
type RecordedCall struct {
	// SystemPrompt is the system message content, if any.
	SystemPrompt string
	// Messages contains "role: content" strings for each non-system prompt message.
	Messages []string
	// Prompt is the final user-turn text (last user message in the prompt).
	Prompt string
}

// FakeLM is a fantasy.LanguageModel that streams preset text responses
// one word at a time and records every call it receives.
type FakeLM struct {
	mu        sync.Mutex
	responses []string // one response string per call
	callIdx   int
	calls     []RecordedCall
	modelID   string
	provider  string
}

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
// It emits the next preset response word-by-word as text deltas.
func (lm *FakeLM) Stream(ctx context.Context, call fantasy.Call) (fantasy.StreamResponse, error) {
	lm.mu.Lock()
	rc := recordCall(call)
	lm.calls = append(lm.calls, rc)
	text := lm.nextResponse()
	lm.mu.Unlock()

	words := splitWords(text)

	return func(yield func(fantasy.StreamPart) bool) {
		// text_start
		if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextStart, ID: "0"}) {
			return
		}
		// emit each word as a delta
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
		// text_end
		if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextEnd, ID: "0"}) {
			return
		}
		// finish
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

// FakeProvider is a fantasy.Provider that returns a single FakeLM for any model.
type FakeProvider struct {
	lm *FakeLM
}

// compile-time interface assertion
var _ fantasy.Provider = (*FakeProvider)(nil)

// NewFakeProvider creates a FakeProvider whose FakeLM will emit the given
// response strings in order (one per call). After responses are exhausted the
// last response is repeated indefinitely.
func NewFakeProvider(responses ...string) *FakeProvider {
	return &FakeProvider{
		lm: &FakeLM{
			responses: responses,
			modelID:   "fake-model",
			provider:  "fake",
		},
	}
}

// LM returns the underlying FakeLM so tests can configure tool call behaviour
// or inspect recorded calls.
func (p *FakeProvider) LM() *FakeLM { return p.lm }

// Name implements fantasy.Provider.
func (p *FakeProvider) Name() string { return "fake" }

// LanguageModel implements fantasy.Provider. Returns the same FakeLM for any model ID.
func (p *FakeProvider) LanguageModel(_ context.Context, _ string) (fantasy.LanguageModel, error) {
	return p.lm, nil
}
