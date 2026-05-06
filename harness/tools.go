package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"context"
	"encoding/json"
	"fmt"

	"charm.land/fantasy"
	"github.com/mattdurham/wllr/extension"
	"github.com/mattdurham/wllr/sdk"
)

// sdkToolAdapter adapts an sdk.Tool to the fantasy.AgentTool interface.
// When Run is called by the fantasy agent, it dispatches the tool call
// to the extension host via ExecuteTool and waits for the result.
type sdkToolAdapter struct {
	tool            sdk.Tool
	host            *extension.Host
	params          map[string]any
	required        []string
	providerOptions fantasy.ProviderOptions
}

// sdkToolsToFantasy converts a slice of sdk.Tool values into []fantasy.AgentTool.
// Tools that cannot be parsed are skipped with a warning logged via logFn.
func sdkToolsToFantasy(tools []sdk.Tool, host *extension.Host, logFn func(int, string)) []fantasy.AgentTool {
	result := make([]fantasy.AgentTool, 0, len(tools))
	for _, t := range tools {
		adapted, err := newSDKToolAdapter(t, host)
		if err != nil {
			if logFn != nil {
				logFn(2, fmt.Sprintf("harness: skip tool %q: %v", t.Name, err))
			}
			continue
		}
		result = append(result, adapted)
	}
	return result
}

// newSDKToolAdapter builds an sdkToolAdapter from an sdk.Tool.
// It parses the InputSchema JSON to extract properties and required fields.
func newSDKToolAdapter(tool sdk.Tool, host *extension.Host) (*sdkToolAdapter, error) {
	params, required, err := parseInputSchema(tool.InputSchema)
	if err != nil {
		return nil, fmt.Errorf("parse input_schema for %q: %w", tool.Name, err)
	}
	return &sdkToolAdapter{
		tool:     tool,
		host:     host,
		params:   params,
		required: required,
	}, nil
}

// parseInputSchema parses a JSON Schema object and extracts the properties
// map and required array.  A nil or empty schema returns empty values.
func parseInputSchema(schema json.RawMessage) (map[string]any, []string, error) {
	if len(schema) == 0 {
		return map[string]any{}, nil, nil
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

	return params, required, nil
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

// Run implements fantasy.AgentTool.
// It calls ExecuteTool on the extension host and blocks until the extension
// returns a result via the tool_result host_call method.
func (a *sdkToolAdapter) Run(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
	if a.host == nil {
		return fantasy.NewTextErrorResponse("no extension host configured"), nil
	}

	result, err := a.host.ExecuteTool(ctx, call.ID, call.Name, json.RawMessage(call.Input))
	if err != nil {
		return fantasy.NewTextErrorResponse(fmt.Sprintf("tool execution failed: %v", err)), nil
	}

	if result.IsError {
		return fantasy.NewTextErrorResponse(result.Result), nil
	}
	return fantasy.NewTextResponse(result.Result), nil
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
func BuildFantasyTools(extHost *extension.Host, logFn func(int, string)) []fantasy.AgentTool {
	if extHost == nil {
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
	return sdkToolsToFantasy(sdkTools, extHost, logFn)
}
