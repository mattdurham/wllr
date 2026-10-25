package tools

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"reflect"
	"sync"
	"time"

	"charm.land/fantasy"

	"github.com/mattdurham/wllr/modules/extension"
	"github.com/mattdurham/wllr/modules/sdk"
)

// ToolExecutor is the slice of the extension host the adapter needs. A
// concrete *extension.Host satisfies it; tests substitute fakes.
type ToolExecutor interface {
	ExecuteTool(ctx context.Context, agentID, toolCallID, toolName string, input json.RawMessage) (extension.ToolResult, error)
	RegisteredTools() []extension.RegisteredToolInfo
}

// DefaultToolCallTimeout bounds every extension tool call. A wedged tool (the
// Oct 2026 tempo incident: a search extension exec deadlocked on a full pipe
// for 16+ minutes) must never freeze an agent's turn — on breach the adapter
// returns a timeout error to the model and the turn continues. Settable via
// SetToolCallTimeout, wired from config at startup.
const DefaultToolCallTimeout = 10 * time.Minute

var toolCallTimeout = DefaultToolCallTimeout

// ToolCallTimeout reports the active watchdog duration (test/config visibility).
func ToolCallTimeout() time.Duration { return toolCallTimeout }

// TestBackdateBreaker backdates the last recorded failure for the given
// (agent, tool, input) so tests can exercise cooldown expiry without waiting.
// Exported for tests only.
func TestBackdateBreaker(agentID, toolName string, input json.RawMessage, lastFailure time.Time) {
	key := breakerKey(agentID, toolName, input)
	toolBreaker.mu.Lock()
	defer toolBreaker.mu.Unlock()
	if st, ok := toolBreaker.fails[key]; ok {
		st.lastFailure = lastFailure
		toolBreaker.fails[key] = st
	}
}

// SetToolCallTimeout overrides the per-tool-call watchdog duration. Values
// <= 0 restore the default. Safe to call before agents are spawned (startup
// config load); not synchronized for concurrent adjustment at runtime.
func SetToolCallTimeout(d time.Duration) {
	if d <= 0 {
		toolCallTimeout = DefaultToolCallTimeout
		return
	}
	toolCallTimeout = d
}

// Circuit-breaker defaults. After BreakerThreshold consecutive watchdog
// timeouts of the same (agent, tool, input) the breaker opens and the adapter
// fails that call fast instead of executing it, breaking the model's
// timeout-retry loops (the list_files stall pattern). The breaker half-opens
// after BreakerCooldown, and any successful execution of the tool resets it.
const (
	BreakerThreshold = 3
	BreakerCooldown  = 5 * time.Minute
)

// toolBreaker tracks consecutive watchdog timeouts per (agent, tool, input
// hash). Package-level: one breaker is shared by all adapters in the process.
var toolBreaker = &breaker{fails: map[string]breakerState{}}

type breakerState struct {
	lastFailure time.Time
	consecutive int
}

type breaker struct {
	fails map[string]breakerState
	mu    sync.Mutex
}

// breakerKey identifies a call site: agent + tool + hash of the raw input.
func breakerKey(agentID, toolName string, input json.RawMessage) string {
	sum := sha256.Sum256(input)
	return agentID + "\x00" + toolName + "\x00" + hex.EncodeToString(sum[:8])
}

// tripped reports whether the breaker is open for the given call and, if so,
// when it can be retried. An open breaker half-opens (returns false) once the
// cooldown has elapsed.
func (b *breaker) tripped(key string, now time.Time) (bool, time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	st, ok := b.fails[key]
	if !ok || st.consecutive < BreakerThreshold {
		return false, time.Time{}
	}
	retryAt := st.lastFailure.Add(BreakerCooldown)
	if now.After(retryAt) {
		// Half-open: allow one attempt through; the next timeout re-trips it.
		st.consecutive = BreakerThreshold - 1
		b.fails[key] = st
		return false, time.Time{}
	}
	return true, retryAt
}

// recordTimeout records a watchdog timeout for the call.
func (b *breaker) recordTimeout(key string, now time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	st := b.fails[key]
	st.consecutive++
	st.lastFailure = now
	b.fails[key] = st
}

// recordSuccess clears the breaker for the tool: any successful execution —
// even with different arguments — proves the tool is healthy again.
func (b *breaker) recordSuccess(agentID, toolName string) {
	prefix := agentID + "\x00" + toolName + "\x00"
	b.mu.Lock()
	defer b.mu.Unlock()
	for k := range b.fails {
		if len(k) >= len(prefix) && k[:len(prefix)] == prefix {
			delete(b.fails, k)
		}
	}
}

// sdkToolAdapter adapts an sdk.Tool to the fantasy.AgentTool interface.
// When Run is called by the fantasy agent, it dispatches the tool call
// to the extension host via ExecuteTool and waits for the result.
type sdkToolAdapter struct {
	host            ToolExecutor
	params          map[string]any
	providerOptions fantasy.ProviderOptions
	agentID         string
	required        []string
	tool            sdk.Tool
}

// ParseInputSchema parses a JSON Schema object and extracts the properties
// map and required array. A nil or empty schema returns empty values.
// Exported for use in tests and session package.
func ParseInputSchema(schema json.RawMessage) (map[string]any, []string, error) {
	if len(schema) == 0 {
		return map[string]any{}, []string{}, nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(schema, &obj); err != nil {
		return nil, nil, err
	}

	var params map[string]any
	if raw, ok := obj["properties"]; ok {
		if err := json.Unmarshal(raw, &params); err != nil {
			return nil, nil, fmt.Errorf("parse properties: %w", err)
		}
	}
	if params == nil {
		params = map[string]any{}
	}

	var required []string
	if raw, ok := obj["required"]; ok {
		if err := json.Unmarshal(raw, &required); err != nil {
			return nil, nil, fmt.Errorf("parse required: %w", err)
		}
	}
	if required == nil {
		required = []string{}
	}

	return params, required, nil
}

// NewSDKToolAdapter builds an sdkToolAdapter from an sdk.Tool.
// It parses the InputSchema JSON to extract properties and required fields.
// Exported for use in tests.
func NewSDKToolAdapter(tool sdk.Tool, host ToolExecutor, agentID string) (*sdkToolAdapter, error) {
	params, required, err := ParseInputSchema(tool.InputSchema)
	if err != nil {
		return nil, fmt.Errorf("parse input_schema for %q: %w", tool.Name, err)
	}
	return &sdkToolAdapter{
		tool:     tool,
		host:     host,
		agentID:  agentID,
		params:   params,
		required: required,
	}, nil
}

// sdkToolsToFantasy converts a slice of sdk.Tool values into []fantasy.AgentTool.
// Tools that cannot be parsed are skipped with a warning logged via logFn.
func sdkToolsToFantasy(
	tools []sdk.Tool,
	host ToolExecutor,
	agentID string,
	logFn func(int, string),
) []fantasy.AgentTool {
	result := make([]fantasy.AgentTool, 0, len(tools))
	for _, t := range tools {
		adapted, err := NewSDKToolAdapter(t, host, agentID)
		if err != nil {
			if logFn != nil {
				logFn(2, fmt.Sprintf("tools: skip tool %q: %v", t.Name, err))
			}
			continue
		}
		result = append(result, adapted)
	}
	return result
}

// Info implements fantasy.AgentTool.
func (a *sdkToolAdapter) Info() fantasy.ToolInfo {
	return fantasy.ToolInfo{
		Name:        a.tool.Name,
		Description: a.tool.Description,
		Parameters:  a.params,
		Required:    a.required,
	}
}

// Run implements fantasy.AgentTool. Execution is bounded by the tool-call
// watchdog (toolCallTimeout): if the host does not answer in time — a hung
// exec, a wedged WASM module — the model receives a timeout error and the
// turn proceeds. Repeated watchdog timeouts of the same (agent, tool, input)
// trip a circuit breaker (BreakerThreshold) that fails the call fast with
// guidance to narrow the request, breaking timeout-retry loops. The breaker
// resets on any successful execution of the tool and half-opens after
// BreakerCooldown. The underlying ExecuteTool goroutine is abandoned on
// breach; it cannot be forcibly interrupted from here, so a truly wedged
// host leaks that goroutine for the process lifetime (bounded by agent
// hard-kill).
func (a *sdkToolAdapter) Run(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
	if a.host == nil {
		return fantasy.NewTextErrorResponse("no extension host configured"), nil
	}

	key := breakerKey(a.agentID, call.Name, json.RawMessage(call.Input))
	if open, retryAt := toolBreaker.tripped(key, time.Now()); open {
		slog.Warn("tools: circuit breaker open, failing call fast",
			"agent", a.agentID, "tool", call.Name, "call_id", call.ID,
			"retry_at", retryAt)
		return fantasy.NewTextErrorResponse(fmt.Sprintf(
			"tool %q with these arguments has timed out %d times in a row and the circuit breaker is open "+
				"until %s. Do not retry the same call unchanged — narrow the request (e.g. a smaller path or page size), "+
				"use a different tool, or move on without this result.",
			call.Name, BreakerThreshold, retryAt.Format(time.TimeOnly))), nil
	}

	type execResult struct {
		err    error
		result extension.ToolResult
	}
	done := make(chan execResult, 1) // buffered: abandoned goroutine never blocks
	execCtx, cancelExec := context.WithTimeout(ctx, toolCallTimeout)
	go func() {
		result, err := a.host.ExecuteTool(execCtx, a.agentID, call.ID, call.Name, json.RawMessage(call.Input))
		done <- execResult{result: result, err: err}
	}()

	var r execResult
	select {
	case r = <-done:
	case <-execCtx.Done():
		cancelExec()
		if ctx.Err() != nil {
			// The turn itself is ending (user cancel / hard kill) — report that,
			// not a tool timeout.
			return fantasy.NewTextErrorResponse("tool call cancelled"), nil
		}
		slog.Warn("tools: tool call exceeded watchdog, abandoning",
			"agent", a.agentID, "tool", call.Name, "call_id", call.ID,
			"timeout", toolCallTimeout)
		toolBreaker.recordTimeout(key, time.Now())
		return fantasy.NewTextErrorResponse(fmt.Sprintf(
			"tool %q timed out after %s and was abandoned; it may still be running in the background",
			call.Name, toolCallTimeout)), nil
	}
	defer cancelExec()

	if r.err != nil {
		return fantasy.NewTextErrorResponse(fmt.Sprintf("tool execution failed: %v", r.err)), nil
	}

	toolBreaker.recordSuccess(a.agentID, call.Name)

	if r.result.IsError {
		return fantasy.NewTextErrorResponse(r.result.Result), nil
	}
	return fantasy.NewTextResponse(r.result.Result), nil
}

// ProviderOptions implements fantasy.AgentTool.
func (a *sdkToolAdapter) ProviderOptions() fantasy.ProviderOptions {
	return a.providerOptions
}

// SetProviderOptions implements fantasy.AgentTool.
func (a *sdkToolAdapter) SetProviderOptions(opts fantasy.ProviderOptions) {
	a.providerOptions = opts
}

// BuildFantasyTools returns the current set of registered tools as []fantasy.AgentTool.
// Returns nil if extHost is nil.
func BuildFantasyTools(extHost ToolExecutor, agentID string, logFn func(int, string)) []fantasy.AgentTool {
	if isNilExecutor(extHost) {
		return nil
	}
	infos := extHost.RegisteredTools()
	if len(infos) == 0 {
		return nil
	}
	sdkTools := make([]sdk.Tool, len(infos))
	for i, info := range infos {
		sdkTools[i] = info.Tool
	}
	return sdkToolsToFantasy(sdkTools, extHost, agentID, logFn)
}

// isNilExecutor catches both a nil interface and a typed-nil (*extension.Host)(nil)
// stored in the interface — the latter panics on method call. Callers in the
// harness legitimately hold a nil *Host before extensions load.
func isNilExecutor(e ToolExecutor) bool {
	if e == nil {
		return true
	}
	v := reflect.ValueOf(e)
	return v.Kind() == reflect.Pointer && v.IsNil()
}
