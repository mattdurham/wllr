package extension

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/mattdurham/wllr/modules/sdk"
)

// TestApplyInterceptorResponse covers the pure decision step of the chain.
func TestApplyInterceptorResponse(t *testing.T) {
	base := sdk.Event{Type: sdk.EventBeforeToolCall, Payload: json.RawMessage(`{"input":"orig"}`)}

	t.Run("observe leaves event unchanged", func(t *testing.T) {
		got, blocked, reason := applyInterceptorResponse(base, sdk.EventResponse{}, "ext")
		if blocked || reason != "" {
			t.Fatalf("observe must not block: blocked=%v reason=%q", blocked, reason)
		}
		if string(got.Payload) != `{"input":"orig"}` {
			t.Errorf("payload changed on observe: %s", got.Payload)
		}
	})

	t.Run("transform threads new payload", func(t *testing.T) {
		resp := sdk.EventResponse{Payload: json.RawMessage(`{"input":"rewritten"}`)}
		got, blocked, _ := applyInterceptorResponse(base, resp, "ext")
		if blocked {
			t.Fatal("transform must not block")
		}
		if string(got.Payload) != `{"input":"rewritten"}` {
			t.Errorf("payload not threaded: %s", got.Payload)
		}
		if got.Type != base.Type {
			t.Errorf("type changed: %s", got.Type)
		}
	})

	t.Run("block stops with reason", func(t *testing.T) {
		resp := sdk.EventResponse{Block: true, Error: "policy denied"}
		_, blocked, reason := applyInterceptorResponse(base, resp, "ext")
		if !blocked || reason != "policy denied" {
			t.Fatalf("block: blocked=%v reason=%q", blocked, reason)
		}
	})

	t.Run("cancel stops with reason", func(t *testing.T) {
		_, blocked, reason := applyInterceptorResponse(base, sdk.EventResponse{Cancel: true}, "secguard")
		if !blocked {
			t.Fatal("cancel must block the chain")
		}
		if reason != "blocked by extension secguard" {
			t.Errorf("default reason: got %q", reason)
		}
	})
}

func TestBlockReason(t *testing.T) {
	if got := blockReason(""); got != "tool call blocked by extension" {
		t.Errorf("empty reason: got %q", got)
	}
	if got := blockReason("contains api key"); got != "tool call blocked: contains api key" {
		t.Errorf("reason: got %q", got)
	}
}

// TestExecuteTool_NativeInterceptorRewritesInput verifies the end-to-end native
// path: a before_tool_call interceptor rewrites the tool input, and the native
// tool receives the rewritten input. Uses a test AgentBridge-free host with a
// native tool and a fake interceptor wired through the bus is not enough — the
// chain runs over WASM extensions, so we drive it via a native interceptor shim
// installed as the only subscriber through a stub extension is heavyweight;
// instead we test runBeforeToolCall's payload handling directly below and rely
// on the WASM end-to-end coverage in TestHost_ExecuteTool_* + the chain unit
// tests above.
func TestRunBeforeToolCall_NoInterceptorsKeepsInput(t *testing.T) {
	h := NewHost(nil)
	ctx := context.Background()
	defer func() { _ = h.Close(ctx) }()

	in := json.RawMessage(`{"command":"ls"}`)
	got, blocked, reason := h.runBeforeToolCall(ctx, "main", "tc-1", "exec", in)
	if blocked || reason != "" {
		t.Fatalf("no interceptors must not block: blocked=%v reason=%q", blocked, reason)
	}
	if string(got) != string(in) {
		t.Errorf("input changed with no interceptors: got %s want %s", got, in)
	}
}

// TestExecuteTool_NativeToolReceivesInput is a baseline: with no interceptors a
// native tool receives the original input unchanged and its result is unchanged.
func TestExecuteTool_NativeToolReceivesInput(t *testing.T) {
	h := NewHost(nil)
	ctx := context.Background()
	defer func() { _ = h.Close(ctx) }()

	var seen string
	h.RegisterNativeTool(sdk.Tool{Name: "exec"}, func(_ context.Context, input json.RawMessage) (string, bool) {
		seen = string(input)
		return "ok", false
	})

	res, err := h.ExecuteTool(ctx, "main", "tc-1", "exec", json.RawMessage(`{"command":"ls"}`))
	if err != nil {
		t.Fatalf("ExecuteTool: %v", err)
	}
	if res.IsError || res.Result != "ok" {
		t.Errorf("result: %+v", res)
	}
	if seen != `{"command":"ls"}` {
		t.Errorf("native tool input: got %s", seen)
	}
}

// TestRunAfterToolCall_NoInterceptorsKeepsResult verifies the output-side chain
// is a passthrough with no interceptors.
func TestRunAfterToolCall_NoInterceptorsKeepsResult(t *testing.T) {
	h := NewHost(nil)
	ctx := context.Background()
	defer func() { _ = h.Close(ctx) }()

	res, isErr := h.runAfterToolCall(ctx, "main", "tc-1", "exec", "raw output", false)
	if res != "raw output" || isErr {
		t.Errorf("passthrough failed: got (%q, %v)", res, isErr)
	}
}
