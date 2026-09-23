package agent

import (
	"context"
	"strings"
	"testing"

	"charm.land/fantasy"
)

// textPartTestLM streams two assistant text parts separated by a tool call:
// call 1 emits a text part then a tool call (finish reason tool-calls so the
// loop continues), call 2 emits a second text part and finishes.
type textPartTestLM struct {
	calls int
}

func (m *textPartTestLM) Stream(_ context.Context, _ fantasy.Call) (fantasy.StreamResponse, error) {
	m.calls++
	if m.calls == 1 {
		return func(yield func(fantasy.StreamPart) bool) {
			yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextStart, ID: "0"})
			yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextDelta, ID: "0", Delta: "First narration segment."})
			yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextEnd, ID: "0"})
			yield(fantasy.StreamPart{
				Type: fantasy.StreamPartTypeToolCall, ID: "call-1",
				ToolCallName: "read_file", ToolCallInput: `{}`,
			})
			yield(fantasy.StreamPart{
				Type: fantasy.StreamPartTypeFinish, FinishReason: fantasy.FinishReasonToolCalls,
				Usage: fantasy.Usage{InputTokens: 10, OutputTokens: 10},
			})
		}, nil
	}
	return func(yield func(fantasy.StreamPart) bool) {
		yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextStart, ID: "1"})
		yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextDelta, ID: "1", Delta: "Second narration segment."})
		yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextEnd, ID: "1"})
		yield(fantasy.StreamPart{
			Type: fantasy.StreamPartTypeFinish, FinishReason: fantasy.FinishReasonStop,
			Usage: fantasy.Usage{InputTokens: 10, OutputTokens: 10},
		})
	}, nil
}

func (m *textPartTestLM) Generate(context.Context, fantasy.Call) (*fantasy.Response, error) {
	return nil, nil
}

func (m *textPartTestLM) GenerateObject(context.Context, fantasy.ObjectCall) (*fantasy.ObjectResponse, error) {
	return nil, nil
}

func (m *textPartTestLM) StreamObject(context.Context, fantasy.ObjectCall) (fantasy.ObjectStreamResponse, error) {
	return func(yield func(fantasy.ObjectStreamPart) bool) {}, nil
}

func (m *textPartTestLM) Provider() string { return "test" }
func (m *textPartTestLM) Model() string    { return "textpart-test" }

// TestStreamTurn_SeparatesTextPartsWithParagraphBreak guards against narration
// segments around tool calls being rendered as one fused blob. Text parts carry
// no separating whitespace of their own, so raw concatenation yields
// "...segment.Second narration..." — both in the live token stream and in the
// collected response persisted to history. The agent must join parts with a
// paragraph break instead.
func TestStreamTurn_SeparatesTextPartsWithParagraphBreak(t *testing.T) {
	lm := &textPartTestLM{}
	tool := fantasy.NewAgentTool("read_file", "reads a file",
		func(context.Context, map[string]any, fantasy.ToolCall) (fantasy.ToolResponse, error) {
			return fantasy.NewTextResponse("file contents"), nil
		})
	fa := fantasy.NewAgent(lm, fantasy.WithTools(tool))

	const want = "First narration segment.\n\nSecond narration segment."

	var streamed strings.Builder
	a := &Agent{}
	got, _, err := a.streamTurn(context.Background(), fa, lm, nil, "inspect", nil,
		func(text string) { streamed.WriteString(text) }, nil,
		262_144, CompactConfig{}, nil)
	if err != nil {
		t.Fatalf("streamTurn: %v", err)
	}
	if lm.calls != 2 {
		t.Fatalf("LM calls = %d, want 2 (both turns must stream)", lm.calls)
	}
	if got != want {
		t.Errorf("collected response fused text parts or lost the break:\n got: %q\nwant: %q", got, want)
	}
	if live := streamed.String(); live != want {
		t.Errorf("live token stream fused text parts or lost the break:\n got: %q\nwant: %q", live, want)
	}
}
