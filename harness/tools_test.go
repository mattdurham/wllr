package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"context"
	"testing"

	"charm.land/fantasy"
	"github.com/mattdurham/wllr/sdk"
)

func TestParseInputSchema_Empty(t *testing.T) {
	params, required, err := parseInputSchema(nil)
	if err != nil {
		t.Fatalf("parseInputSchema nil: %v", err)
	}
	if len(params) != 0 {
		t.Errorf("expected empty params, got %v", params)
	}
	if len(required) != 0 {
		t.Errorf("expected empty required, got %v", required)
	}
}

func TestParseInputSchema_WithProperties(t *testing.T) {
	schema := []byte(`{
		"type": "object",
		"properties": {
			"path": {"type": "string", "description": "File path"},
			"encoding": {"type": "string"}
		},
		"required": ["path"]
	}`)

	params, required, err := parseInputSchema(schema)
	if err != nil {
		t.Fatalf("parseInputSchema: %v", err)
	}
	if _, ok := params["path"]; !ok {
		t.Error("expected 'path' in params")
	}
	if _, ok := params["encoding"]; !ok {
		t.Error("expected 'encoding' in params")
	}
	if len(required) != 1 || required[0] != "path" {
		t.Errorf("expected required=[path], got %v", required)
	}
}

func TestParseInputSchema_EmptyObject(t *testing.T) {
	schema := []byte(`{}`)
	params, required, err := parseInputSchema(schema)
	if err != nil {
		t.Fatalf("parseInputSchema {}: %v", err)
	}
	if len(params) != 0 {
		t.Errorf("expected empty params, got %v", params)
	}
	if len(required) != 0 {
		t.Errorf("expected empty required, got %v", required)
	}
}

func TestSDKToolAdapter_Info(t *testing.T) {
	tool := sdk.Tool{
		Name:        "read_file",
		Description: "Reads a file from the filesystem",
		InputSchema: []byte(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`),
	}

	adapter, err := newSDKToolAdapter(tool, nil, "test-agent")
	if err != nil {
		t.Fatalf("newSDKToolAdapter: %v", err)
	}

	info := adapter.Info()
	if info.Name != "read_file" {
		t.Errorf("Info.Name: got %q, want %q", info.Name, "read_file")
	}
	if info.Description != "Reads a file from the filesystem" {
		t.Errorf("Info.Description: got %q", info.Description)
	}
	if _, ok := info.Parameters["path"]; !ok {
		t.Error("expected 'path' in parameters")
	}
	if len(info.Required) != 1 || info.Required[0] != "path" {
		t.Errorf("Info.Required: got %v, want [path]", info.Required)
	}
}

func TestSDKToolAdapter_Run_NoHost(t *testing.T) {
	tool := sdk.Tool{Name: "test"}
	adapter, err := newSDKToolAdapter(tool, nil, "test-agent")
	if err != nil {
		t.Fatalf("newSDKToolAdapter: %v", err)
	}

	resp, err := adapter.Run(context.Background(), fantasy.ToolCall{
		ID:    "call-1",
		Name:  "test",
		Input: `{}`,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !resp.IsError {
		t.Error("expected error response when no host configured")
	}
}

func TestSDKToolsToFantasy_SkipsBadSchema(t *testing.T) {
	tools := []sdk.Tool{
		{Name: "good", Description: "works", InputSchema: []byte(`{}`)},
		{Name: "bad", Description: "broken", InputSchema: []byte(`not-json`)},
	}

	var loggedWarning bool
	adapted := sdkToolsToFantasy(tools, nil, "test-agent", func(level int, msg string) {
		if level == 2 {
			loggedWarning = true
		}
	})

	if len(adapted) != 1 {
		t.Errorf("expected 1 adapted tool (bad schema skipped), got %d", len(adapted))
	}
	if adapted[0].Info().Name != "good" {
		t.Errorf("expected good tool, got %q", adapted[0].Info().Name)
	}
	if !loggedWarning {
		t.Error("expected warning to be logged for bad schema tool")
	}
}

func TestSDKToolAdapter_ProviderOptions(t *testing.T) {
	tool := sdk.Tool{Name: "x", InputSchema: []byte(`{}`)}
	adapter, err := newSDKToolAdapter(tool, nil, "test-agent")
	if err != nil {
		t.Fatalf("newSDKToolAdapter: %v", err)
	}

	if adapter.ProviderOptions() != nil {
		t.Error("expected nil provider options initially")
	}

	opts := fantasy.ProviderOptions{}
	adapter.SetProviderOptions(opts)
	got := adapter.ProviderOptions()
	if got == nil {
		t.Error("expected non-nil provider options after SetProviderOptions")
	}
}
