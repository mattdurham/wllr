package testutil_test

import (
	"context"
	"strings"
	"testing"

	"charm.land/fantasy"
	"github.com/mattdurham/wllr/modules/testutil"
)

func TestNewFakeProvider_Name(t *testing.T) {
	p := testutil.NewFakeProvider("hello world")
	if p.Name() != "fake" {
		t.Errorf("Name: got %q, want %q", p.Name(), "fake")
	}
}

func TestNewFakeProvider_LanguageModel(t *testing.T) {
	p := testutil.NewFakeProvider("response text")
	lm, err := p.LanguageModel(context.Background(), "any-model")
	if err != nil {
		t.Fatalf("LanguageModel: %v", err)
	}
	if lm == nil {
		t.Fatal("LanguageModel returned nil")
	}
	if lm.Provider() != "fake" {
		t.Errorf("Provider: got %q, want %q", lm.Provider(), "fake")
	}
}

func TestFakeLM_Stream_EmitsTokensWordByWord(t *testing.T) {
	p := testutil.NewFakeProvider("hello world foo")
	lm := p.LM()

	ctx := context.Background()
	stream, err := lm.Stream(ctx, fantasy.Call{
		Prompt: fantasy.Prompt{
			{Role: fantasy.MessageRoleUser, Content: []fantasy.MessagePart{fantasy.TextPart{Text: "prompt"}}},
		},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	var collected strings.Builder
	for part := range stream {
		if part.Type == fantasy.StreamPartTypeTextDelta {
			collected.WriteString(part.Delta)
		}
	}

	if collected.String() != "hello world foo" {
		t.Errorf("collected: got %q, want %q", collected.String(), "hello world foo")
	}
}

func TestFakeLM_Stream_RecordsCall(t *testing.T) {
	p := testutil.NewFakeProvider("response")
	lm := p.LM()

	ctx := context.Background()
	stream, err := lm.Stream(ctx, fantasy.Call{
		Prompt: fantasy.Prompt{
			{Role: fantasy.MessageRoleSystem, Content: []fantasy.MessagePart{fantasy.TextPart{Text: "sys"}}},
			{Role: fantasy.MessageRoleUser, Content: []fantasy.MessagePart{fantasy.TextPart{Text: "user input"}}},
		},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	// Drain stream.
	for range stream {
	}

	calls := lm.Calls()
	if len(calls) != 1 {
		t.Fatalf("Calls: expected 1, got %d", len(calls))
	}
	if calls[0].SystemPrompt != "sys" {
		t.Errorf("SystemPrompt: got %q, want %q", calls[0].SystemPrompt, "sys")
	}
	if calls[0].Prompt != "user input" {
		t.Errorf("Prompt: got %q, want %q", calls[0].Prompt, "user input")
	}
}

func TestFakeLM_LastCall(t *testing.T) {
	p := testutil.NewFakeProvider("first", "second")
	lm := p.LM()
	ctx := context.Background()

	makeCall := func(text string) {
		stream, _ := lm.Stream(ctx, fantasy.Call{
			Prompt: fantasy.Prompt{
				{Role: fantasy.MessageRoleUser, Content: []fantasy.MessagePart{fantasy.TextPart{Text: text}}},
			},
		})
		for range stream {
		}
	}

	makeCall("call one")
	makeCall("call two")

	last := lm.LastCall()
	if last.Prompt != "call two" {
		t.Errorf("LastCall.Prompt: got %q, want %q", last.Prompt, "call two")
	}
}

func TestFakeLM_Generate_ReturnsText(t *testing.T) {
	p := testutil.NewFakeProvider("generated text")
	lm := p.LM()

	resp, err := lm.Generate(context.Background(), fantasy.Call{
		Prompt: fantasy.Prompt{
			{Role: fantasy.MessageRoleUser, Content: []fantasy.MessagePart{fantasy.TextPart{Text: "q"}}},
		},
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if resp.Content.Text() != "generated text" {
		t.Errorf("Content.Text: got %q, want %q", resp.Content.Text(), "generated text")
	}
}

func TestFakeLM_MultipleResponses_CyclesThroughThem(t *testing.T) {
	p := testutil.NewFakeProvider("first", "second", "third")
	lm := p.LM()
	ctx := context.Background()

	drainStream := func() string {
		stream, _ := lm.Stream(ctx, fantasy.Call{
			Prompt: fantasy.Prompt{
				{Role: fantasy.MessageRoleUser, Content: []fantasy.MessagePart{fantasy.TextPart{Text: "q"}}},
			},
		})
		var sb strings.Builder
		for part := range stream {
			if part.Type == fantasy.StreamPartTypeTextDelta {
				sb.WriteString(part.Delta)
			}
		}
		return sb.String()
	}

	if got := drainStream(); got != "first" {
		t.Errorf("call 1: got %q, want %q", got, "first")
	}
	if got := drainStream(); got != "second" {
		t.Errorf("call 2: got %q, want %q", got, "second")
	}
	if got := drainStream(); got != "third" {
		t.Errorf("call 3: got %q, want %q", got, "third")
	}
	// 4th call: repeats last response.
	if got := drainStream(); got != "third" {
		t.Errorf("call 4 (repeat last): got %q, want %q", got, "third")
	}
}

func TestFakeLM_Stream_ContextCancelled_StopsEarly(t *testing.T) {
	// Build a provider with a long response so we can cancel mid-stream.
	words := strings.Repeat("word ", 100)
	p := testutil.NewFakeProvider(words)
	lm := p.LM()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	stream, err := lm.Stream(ctx, fantasy.Call{
		Prompt: fantasy.Prompt{
			{Role: fantasy.MessageRoleUser, Content: []fantasy.MessagePart{fantasy.TextPart{Text: "q"}}},
		},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	count := 0
	for part := range stream {
		if part.Type == fantasy.StreamPartTypeTextDelta {
			count++
			if count >= 3 {
				cancel()
			}
		}
	}

	// Should have stopped before emitting all 100 words.
	if count >= 100 {
		t.Errorf("expected early cancellation, got %d deltas", count)
	}
}
