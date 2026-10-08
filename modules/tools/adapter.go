package tools

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"reflect"
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
// turn proceeds. The underlying ExecuteTool goroutine is abandoned on breach;
// it cannot be forcibly interrupted from here, so a truly wedged host leaks
// that goroutine for the process lifetime (bounded by agent hard-kill).
func (a *sdkToolAdapter) Run(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
	if a.host == nil {
		return fantasy.NewTextErrorResponse("no extension host configured"), nil
	}

	type execResult struct {
		result extension.ToolResult
		err    error
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
		return fantasy.NewTextErrorResponse(fmt.Sprintf(
			"tool %q timed out after %s and was abandoned; it may still be running in the background",
			call.Name, toolCallTimeout)), nil
	}
	defer cancelExec()

	if r.err != nil {
		return fantasy.NewTextErrorResponse(fmt.Sprintf("tool execution failed: %v", r.err)), nil
	}

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
