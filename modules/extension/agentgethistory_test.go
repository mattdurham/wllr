package extension

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/mattdurham/wllr/modules/sdk"
)

// TestAgentGetHistory covers the read path that lets a view render an agent's
// transcript: an agent id is forwarded to the bridge and the messages come back
// in the result payload.
func TestAgentGetHistory(t *testing.T) {
	h := NewHost(nil)
	var askedFor string
	h.SetAgentBridge(&testAgentBridge{
		onGetHistory: func(id string) ([]sdk.Message, error) {
			askedFor = id
			return []sdk.Message{
				{Role: sdk.RoleUser, Content: "research this"},
				{Role: sdk.RoleAssistant, Content: "answer"},
			}, nil
		},
	})

	ctx := context.Background()
	defer func() { _ = h.Close(ctx) }()
	path := writeWASM(t, "minimal.wasm", minimalWASM)
	if err := h.Load(ctx, path); err != nil {
		t.Fatalf("Load: %v", err)
	}
	ext := h.extensions[0]

	resp := h.routeHostCall(ctx, ext.module, ext, sdk.HostCallRequest{
		Method: sdk.MethodAgentGetHistory,
		Params: []byte(`{"id":"main/kid"}`),
	})
	if resp.Error != "" {
		t.Fatalf("agent_get_history error: %s", resp.Error)
	}
	if askedFor != "main/kid" {
		t.Errorf("bridge asked for %q, want main/kid", askedFor)
	}
	var got sdk.AgentGetHistoryResult
	if err := json.Unmarshal(resp.Result, &got); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if len(got.Messages) != 2 {
		t.Fatalf("messages = %d, want 2", len(got.Messages))
	}
	if got.Messages[0].Content != "research this" || got.Messages[1].Role != sdk.RoleAssistant {
		t.Errorf("unexpected messages: %+v", got.Messages)
	}
}

// TestAgentGetHistoryEmptyIsNotAnError pins that an agent with no history yet
// returns an empty list rather than an error, so a focused view can open on it.
func TestAgentGetHistoryEmptyIsNotAnError(t *testing.T) {
	h := NewHost(nil)
	h.SetAgentBridge(&testAgentBridge{
		onGetHistory: func(string) ([]sdk.Message, error) { return nil, nil },
	})
	ctx := context.Background()
	defer func() { _ = h.Close(ctx) }()
	path := writeWASM(t, "minimal.wasm", minimalWASM)
	if err := h.Load(ctx, path); err != nil {
		t.Fatalf("Load: %v", err)
	}
	ext := h.extensions[0]
	resp := h.routeHostCall(ctx, ext.module, ext, sdk.HostCallRequest{
		Method: sdk.MethodAgentGetHistory,
	})
	if resp.Error != "" {
		t.Fatalf("empty history should not error, got %s", resp.Error)
	}
	var got sdk.AgentGetHistoryResult
	if err := json.Unmarshal(resp.Result, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.Messages) != 0 {
		t.Errorf("messages = %+v, want none", got.Messages)
	}
}
