package tools_test

// watchdog_test.go — the tool-call watchdog pins the Oct 2026 tempo incident:
// a wedged extension tool call (a search exec deadlocked on a full pipe) must
// never freeze an agent's turn. The adapter bounds every ExecuteTool with a
// timeout, returns a timeout error to the model, and lets the turn continue.

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"charm.land/fantasy"
	"github.com/mattdurham/wllr/modules/extension"
	"github.com/mattdurham/wllr/modules/sdk"
	"github.com/mattdurham/wllr/modules/tools"
)

// blockingHost simulates a wedged extension host: ExecuteTool never returns
// until its context is cancelled (exactly the unreachable-WASM wedge shape).
type blockingHost struct {
	started chan struct{}
}

func (b *blockingHost) ExecuteTool(ctx context.Context, _, _, _ string, _ json.RawMessage) (extension.ToolResult, error) {
	close(b.started)
	<-ctx.Done()
	return extension.ToolResult{}, ctx.Err()
}

func (b *blockingHost) RegisteredTools() []extension.RegisteredToolInfo { return nil }

// okHost returns immediately with a successful result.
type okHost struct{}

func (okHost) ExecuteTool(context.Context, string, string, string, json.RawMessage) (extension.ToolResult, error) {
	return extension.ToolResult{Result: "done"}, nil
}

func (okHost) RegisteredTools() []extension.RegisteredToolInfo { return nil }

func newAdapter(t *testing.T, host tools.ToolExecutor) fantasy.AgentTool {
	t.Helper()
	adapter, err := tools.NewSDKToolAdapter(
		sdk.Tool{Name: "search", Description: "search"},
		host,
		"agent1",
	)
	if err != nil {
		t.Fatalf("NewSDKToolAdapter: %v", err)
	}
	return adapter
}

// TestWatchdog_TimeoutReturnsErrorToModel — a tool call that outlives the
// watchdog must return a timeout error promptly (the turn proceeds), not hang.
func TestWatchdog_TimeoutReturnsErrorToModel(t *testing.T) {
	tools.SetToolCallTimeout(50 * time.Millisecond)
	t.Cleanup(func() { tools.SetToolCallTimeout(0) })

	host := &blockingHost{started: make(chan struct{})}
	adapter := newAdapter(t, host)

	start := time.Now()
	resp, err := adapter.Run(context.Background(), fantasy.ToolCall{ID: "c1", Name: "search", Input: `{}`})
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Run returned Go error (should be a tool error response): %v", err)
	}
	if !resp.IsError {
		t.Fatal("expected error response on watchdog breach")
	}
	if !strings.Contains(resp.Content, "timed out") {
		t.Fatalf("expected timeout message, got: %s", resp.Content)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("watchdog returned after %v — too slow", elapsed)
	}
}

// TestWatchdog_ParentCancelNotMisreported — when the TURN is cancelled (parent
// ctx) while a tool call is in flight, the error must say cancelled, not
// "timed out" (the distinction matters for what the model is told).
func TestWatchdog_ParentCancelNotMisreported(t *testing.T) {
	tools.SetToolCallTimeout(10 * time.Second)
	t.Cleanup(func() { tools.SetToolCallTimeout(0) })

	host := &blockingHost{started: make(chan struct{})}
	adapter := newAdapter(t, host)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	var resp fantasy.ToolResponse
	go func() {
		defer close(done)
		resp, _ = adapter.Run(ctx, fantasy.ToolCall{ID: "c1", Name: "search", Input: `{}`})
	}()
	<-host.started // wait until ExecuteTool is in flight
	cancel()
	<-done

	if !resp.IsError || !strings.Contains(resp.Content, "cancelled") {
		t.Fatalf("expected cancelled error, got: %s", resp.Content)
	}
}

// TestWatchdog_SuccessPathUnaffected — a fast tool call returns normally with
// the watchdog installed (no behavioral change on the happy path).
func TestWatchdog_SuccessPathUnaffected(t *testing.T) {
	tools.SetToolCallTimeout(10 * time.Second)
	t.Cleanup(func() { tools.SetToolCallTimeout(0) })

	adapter := newAdapter(t, okHost{})
	resp, err := adapter.Run(context.Background(), fantasy.ToolCall{ID: "c1", Name: "search", Input: `{}`})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if resp.IsError {
		t.Fatalf("unexpected error response: %s", resp.Content)
	}
	if !strings.Contains(resp.Content, "done") {
		t.Fatalf("expected tool result, got: %s", resp.Content)
	}
}

// TestWatchdog_SetToolCallTimeout contract: <=0 restores the default.
func TestWatchdog_SetToolCallTimeout(t *testing.T) {
	tools.SetToolCallTimeout(5 * time.Second)
	if got := tools.ToolCallTimeout(); got != 5*time.Second {
		t.Fatalf("ToolCallTimeout() = %v, want 5s", got)
	}
	tools.SetToolCallTimeout(-1)
	if got := tools.ToolCallTimeout(); got != tools.DefaultToolCallTimeout {
		t.Fatalf("after <=0 reset: got %v, want default %v", got, tools.DefaultToolCallTimeout)
	}
}

// TestWatchdog_HostErrorSurfaces — a Go error from ExecuteTool still maps to
// a tool error response (existing contract, pinned alongside the watchdog).
func TestWatchdog_HostErrorSurfaces(t *testing.T) {
	host := errHost{}
	adapter := newAdapter(t, host)
	resp, err := adapter.Run(context.Background(), fantasy.ToolCall{ID: "c1", Name: "search", Input: `{}`})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !resp.IsError || !strings.Contains(resp.Content, "boom") {
		t.Fatalf("expected boom error response, got: %s", resp.Content)
	}
}

type errHost struct{}

func (errHost) ExecuteTool(context.Context, string, string, string, json.RawMessage) (extension.ToolResult, error) {
	return extension.ToolResult{}, errors.New("boom")
}

func (errHost) RegisteredTools() []extension.RegisteredToolInfo { return nil }
