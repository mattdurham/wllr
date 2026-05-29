package agent_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"charm.land/fantasy"
	"github.com/mattdurham/wllr/modules/agent"
	"github.com/mattdurham/wllr/modules/sdk"
)

// captureHistoryLM is a mock LM that records the Prompt slice passed to Stream.
// It immediately returns a text delta so the turn completes with a response.
type captureHistoryLM struct {
	mu       sync.Mutex
	captured fantasy.Prompt // last call.Prompt
}

func (c *captureHistoryLM) Model() string    { return "capture-model" }
func (c *captureHistoryLM) Provider() string { return "capture" }

func (c *captureHistoryLM) Stream(_ context.Context, call fantasy.Call) (fantasy.StreamResponse, error) {
	c.mu.Lock()
	c.captured = make(fantasy.Prompt, len(call.Prompt))
	copy(c.captured, call.Prompt)
	c.mu.Unlock()
	return func(yield func(fantasy.StreamPart) bool) {
		yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextDelta, Delta: "ok"})
		yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeFinish, FinishReason: fantasy.FinishReasonStop})
	}, nil
}

func (c *captureHistoryLM) Generate(_ context.Context, _ fantasy.Call) (*fantasy.Response, error) {
	return nil, nil
}

func (c *captureHistoryLM) GenerateObject(_ context.Context, _ fantasy.ObjectCall) (*fantasy.ObjectResponse, error) {
	return nil, nil
}

func (c *captureHistoryLM) StreamObject(_ context.Context, _ fantasy.ObjectCall) (fantasy.ObjectStreamResponse, error) {
	return nil, nil
}

// lastPrompt returns the Prompt passed to the most recent Stream call.
func (c *captureHistoryLM) lastPrompt() fantasy.Prompt {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(fantasy.Prompt, len(c.captured))
	copy(out, c.captured)
	return out
}

// textContent extracts the concatenated text content from a fantasy.Message.
func textContent(m fantasy.Message) string {
	var s string
	for _, p := range m.Content {
		if tp, ok := p.(fantasy.TextPart); ok {
			s += tp.Text
		}
	}
	return s
}

// TestInboxMessages_AppendedAfterPriorHistory verifies that inbox messages appear
// AFTER prior history in the Prompt passed to the LM.
// This is the key ordering invariant: inbox messages are more recent than prior
// history and must be the last messages seen by the LLM.
func TestInboxMessages_AppendedAfterPriorHistory(t *testing.T) {
	pool := agent.NewPool()
	lm := &captureHistoryLM{}
	a, _ := pool.Spawn("ordering-test", lm, agent.SpawnOpts{})

	done := make(chan error, 1)
	a.SetOnDone(func(err error) { done <- err })

	// First turn: establishes history with user+assistant pair.
	a.Submit(context.Background(), "first message")
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("first turn error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("first turn timeout")
	}

	// History now ends with an assistant message.
	// Append an inbox message (user role) — this should appear LAST in the prompt.
	a.AppendInbox(sdk.Message{Role: sdk.RoleUser, Content: "inbox message"})

	// Second turn with empty content (relies on inbox).
	a.Submit(context.Background(), "")
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("second turn error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("second turn timeout")
	}

	prompt := lm.lastPrompt()
	if len(prompt) == 0 {
		t.Fatal("no messages in captured prompt")
	}
	last := prompt[len(prompt)-1]
	if last.Role != fantasy.MessageRoleUser {
		t.Errorf("expected last message role to be user, got %q", last.Role)
	}
	if got := textContent(last); got != "inbox message" {
		t.Errorf("expected last message content to be %q, got %q", "inbox message", got)
	}
}

// TestInboxMessages_EmptyPromptValidAfterAppend verifies that calling Submit with
// content="" is valid when there are inbox messages in the queue.
// This is the direct regression test for Bug #1.
func TestInboxMessages_EmptyPromptValidAfterAppend(t *testing.T) {
	pool := agent.NewPool()
	lm := &captureHistoryLM{}
	a, _ := pool.Spawn("empty-prompt-test", lm, agent.SpawnOpts{})

	done := make(chan error, 1)
	a.SetOnDone(func(err error) { done <- err })

	// Run one turn to establish assistant history (history ends with assistant).
	a.Submit(context.Background(), "setup message")
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("setup turn error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("setup turn timeout")
	}

	// History now ends with an assistant message.
	// Append an inbox message — this makes empty content valid.
	a.AppendInbox(sdk.Message{Role: sdk.RoleUser, Content: "from another agent"})

	// Submit with empty content — must NOT return "prompt can't be empty" error.
	a.Submit(context.Background(), "")
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("expected nil error with inbox message present, got: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("empty-prompt turn timeout")
	}
}
