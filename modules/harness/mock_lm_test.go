package harness

import (
	"context"

	"charm.land/fantasy"
)

// mockLM is a test double for fantasy.LanguageModel that emits a fixed
// list of text tokens and then finishes.
type mockLM struct {
	tokens    []string
	streamErr error
	provider  string
	modelID   string
	callCount int
}

// compile-time interface check
var _ fantasy.LanguageModel = (*mockLM)(nil)

func newMockLM(tokens ...string) *mockLM {
	return &mockLM{
		tokens:   tokens,
		provider: "mock",
		modelID:  "mock-model-1",
	}
}

func (m *mockLM) Generate(_ context.Context, _ fantasy.Call) (*fantasy.Response, error) {
	return &fantasy.Response{}, nil
}

func (m *mockLM) Stream(ctx context.Context, _ fantasy.Call) (fantasy.StreamResponse, error) {
	m.callCount++
	tokens := m.tokens
	streamErr := m.streamErr
	return func(yield func(fantasy.StreamPart) bool) {
		for _, tok := range tokens {
			select {
			case <-ctx.Done():
				return
			default:
			}
			if !yield(fantasy.StreamPart{
				Type:  fantasy.StreamPartTypeTextDelta,
				Delta: tok,
			}) {
				return
			}
		}
		if streamErr != nil {
			yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeError, Error: streamErr})
			return
		}
		yield(fantasy.StreamPart{
			Type:         fantasy.StreamPartTypeFinish,
			FinishReason: fantasy.FinishReasonStop,
		})
	}, nil
}

func (m *mockLM) GenerateObject(_ context.Context, _ fantasy.ObjectCall) (*fantasy.ObjectResponse, error) {
	return nil, nil
}

func (m *mockLM) StreamObject(_ context.Context, _ fantasy.ObjectCall) (fantasy.ObjectStreamResponse, error) {
	return nil, nil
}

func (m *mockLM) Provider() string { return m.provider }
func (m *mockLM) Model() string    { return m.modelID }

// usageMockLM is a test double that emits fixed tokens plus a finish part with usage.
type usageMockLM struct {
	tokens       []string
	inputTokens  int64
	outputTokens int64
}

var _ fantasy.LanguageModel = (*usageMockLM)(nil)

func newUsageMockLM(inputTokens, outputTokens int64, tokens ...string) *usageMockLM {
	return &usageMockLM{
		tokens:       tokens,
		inputTokens:  inputTokens,
		outputTokens: outputTokens,
	}
}

func (u *usageMockLM) Generate(_ context.Context, _ fantasy.Call) (*fantasy.Response, error) {
	return &fantasy.Response{}, nil
}

func (u *usageMockLM) Stream(ctx context.Context, _ fantasy.Call) (fantasy.StreamResponse, error) {
	toks := u.tokens
	usage := fantasy.Usage{
		InputTokens:  u.inputTokens,
		OutputTokens: u.outputTokens,
		TotalTokens:  u.inputTokens + u.outputTokens,
	}
	return func(yield func(fantasy.StreamPart) bool) {
		for _, tok := range toks {
			select {
			case <-ctx.Done():
				return
			default:
			}
			if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextDelta, Delta: tok}) {
				return
			}
		}
		yield(fantasy.StreamPart{
			Type:         fantasy.StreamPartTypeFinish,
			FinishReason: fantasy.FinishReasonStop,
			Usage:        usage,
		})
	}, nil
}

func (u *usageMockLM) GenerateObject(_ context.Context, _ fantasy.ObjectCall) (*fantasy.ObjectResponse, error) {
	return nil, nil
}

func (u *usageMockLM) StreamObject(_ context.Context, _ fantasy.ObjectCall) (fantasy.ObjectStreamResponse, error) {
	return nil, nil
}

func (u *usageMockLM) Provider() string { return "usage-mock" }
func (u *usageMockLM) Model() string    { return "usage-mock-model" }
