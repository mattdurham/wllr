package tools_test

// breaker_test.go — the tool circuit breaker pins the list_files stall
// pattern: a model that retries the same timed-out call verbatim must be
// stopped after BreakerThreshold consecutive watchdog timeouts. The breaker
// fails the call fast with guidance, resets on any tool success, half-opens
// after BreakerCooldown, and does not trip on distinct arguments.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"charm.land/fantasy"
	"github.com/mattdurham/wllr/modules/extension"
	"github.com/mattdurham/wllr/modules/sdk"
	"github.com/mattdurham/wllr/modules/tools"
)

// timeoutThenOKHost wedges only for the given input; other inputs succeed.
type timeoutThenOKHost struct {
	wedgeInput string
}

func (h *timeoutThenOKHost) ExecuteTool(
	ctx context.Context,
	_, _, _ string,
	input json.RawMessage,
) (extension.ToolResult, error) {
	if string(input) != h.wedgeInput {
		return extension.ToolResult{Result: "done"}, nil
	}
	<-ctx.Done()
	return extension.ToolResult{}, ctx.Err()
}

func (h *timeoutThenOKHost) RegisteredTools() []extension.RegisteredToolInfo { return nil }

func breakerAdapter(t *testing.T, host tools.ToolExecutor, agentID string) fantasy.AgentTool {
	t.Helper()
	adapter, err := tools.NewSDKToolAdapter(
		sdk.Tool{Name: "search", Description: "search"},
		host, agentID,
	)
	if err != nil {
		t.Fatalf("NewSDKToolAdapter: %v", err)
	}
	return adapter
}

// TestBreaker_TripsAfterConsecutiveTimeouts — the same (agent, tool, input)
// timing out BreakerThreshold times in a row must fail fast on the next
// identical attempt, with guidance to narrow the request.
func TestBreaker_TripsAfterConsecutiveTimeouts(t *testing.T) {
	tools.SetToolCallTimeout(20 * time.Millisecond)
	t.Cleanup(func() { tools.SetToolCallTimeout(0) })

	host := &timeoutThenOKHost{wedgeInput: `{"path":"../wllr"}`}
	adapter := breakerAdapter(t, host, "agent-a")
	input := `{"path":"../wllr"}`

	// Burn through the threshold with identical timeouts.
	for i := 0; i < tools.BreakerThreshold; i++ {
		resp, err := adapter.Run(
			context.Background(),
			fantasy.ToolCall{ID: fmt.Sprintf("c%d", i), Name: "search", Input: input},
		)
		if err != nil || !resp.IsError || !strings.Contains(resp.Content, "timed out") {
			t.Fatalf("attempt %d: expected timeout error, got err=%v resp=%+v", i, err, resp.Content)
		}
	}

	// Next identical call must fail fast: no timeout wait, breaker message.
	start := time.Now()
	resp, err := adapter.Run(context.Background(), fantasy.ToolCall{ID: "cx", Name: "search", Input: input})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !resp.IsError || !strings.Contains(resp.Content, "circuit breaker is open") {
		t.Fatalf("expected breaker error, got: %s", resp.Content)
	}
	if elapsed > 5*time.Millisecond {
		t.Fatalf("breaker returned after %v — should fail fast", elapsed)
	}
	if strings.Contains(resp.Content, "timed out after") {
		t.Fatal("breaker response must not look like a fresh timeout")
	}
}

// TestBreaker_DistinctInputNotBlocked — different arguments are a different
// key: the model may retry with a narrowed request even while the original
// call's breaker is open.
func TestBreaker_DistinctInputNotBlocked(t *testing.T) {
	tools.SetToolCallTimeout(20 * time.Millisecond)
	t.Cleanup(func() { tools.SetToolCallTimeout(0) })

	host := &timeoutThenOKHost{wedgeInput: `{"path":"../wllr"}`}
	adapter := breakerAdapter(t, host, "agent-b")
	wedged := `{"path":"../wllr"}`
	for i := 0; i < tools.BreakerThreshold; i++ {
		adapter.Run(context.Background(), fantasy.ToolCall{ID: fmt.Sprintf("c%d", i), Name: "search", Input: wedged})
	}

	// Same tool, different args: must go through to the host and succeed.
	resp, err := adapter.Run(context.Background(), fantasy.ToolCall{ID: "cN", Name: "search", Input: `{"path":"src"}`})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if resp.IsError {
		t.Fatalf("narrowed request must not be blocked, got: %s", resp.Content)
	}
}

// TestBreaker_SuccessResets — a successful execution of the tool clears its
// breaker state, so subsequent identical calls execute again.
func TestBreaker_SuccessResets(t *testing.T) {
	tools.SetToolCallTimeout(20 * time.Millisecond)
	t.Cleanup(func() { tools.SetToolCallTimeout(0) })

	host := &timeoutThenOKHost{wedgeInput: `{"path":"../wllr"}`}
	adapter := breakerAdapter(t, host, "agent-c")
	wedged := `{"path":"../wllr"}`

	// Trip it for one input...
	for i := 0; i < tools.BreakerThreshold; i++ {
		adapter.Run(context.Background(), fantasy.ToolCall{ID: fmt.Sprintf("c%d", i), Name: "search", Input: wedged})
	}
	// ...then succeed with different args for the same tool.
	okAdapter := breakerAdapter(t, &timeoutThenOKHost{}, "agent-c")
	if resp, _ := okAdapter.Run(context.Background(), fantasy.ToolCall{ID: "ok", Name: "search", Input: `{"path":"src"}`}); resp.IsError {
		t.Fatalf("success call failed: %s", resp.Content)
	}

	// The previously-tripped identical call must now execute again (and
	// time out freshly, not fail fast with the breaker message).
	resp, err := adapter.Run(context.Background(), fantasy.ToolCall{ID: "again", Name: "search", Input: wedged})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !resp.IsError || !strings.Contains(resp.Content, "timed out after") {
		t.Fatalf("expected fresh timeout after reset, got: %s", resp.Content)
	}
}

// TestBreaker_HalfOpensAfterCooldown — after BreakerCooldown elapses, the
// breaker lets one attempt through again.
func TestBreaker_HalfOpensAfterCooldown(t *testing.T) {
	tools.SetToolCallTimeout(20 * time.Millisecond)
	t.Cleanup(func() { tools.SetToolCallTimeout(0) })

	host := &timeoutThenOKHost{wedgeInput: `{"path":"../wllr"}`}
	adapter := breakerAdapter(t, host, "agent-d")
	wedged := `{"path":"../wllr"}`

	for i := 0; i < tools.BreakerThreshold; i++ {
		adapter.Run(context.Background(), fantasy.ToolCall{ID: fmt.Sprintf("c%d", i), Name: "search", Input: wedged})
	}
	if resp, _ := adapter.Run(context.Background(), fantasy.ToolCall{ID: "fast", Name: "search", Input: wedged}); !strings.Contains(
		resp.Content,
		"circuit breaker",
	) {
		t.Fatalf("expected breaker open, got: %s", resp.Content)
	}

	// Age the failure past the cooldown by backdating the last failure.
	tools.TestBackdateBreaker(
		"agent-d",
		"search",
		json.RawMessage(wedged),
		time.Now().Add(-tools.BreakerCooldown-time.Second),
	)

	resp, err := adapter.Run(context.Background(), fantasy.ToolCall{ID: "retry", Name: "search", Input: wedged})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if strings.Contains(resp.Content, "circuit breaker") {
		t.Fatalf("breaker should half-open after cooldown, got: %s", resp.Content)
	}
}
