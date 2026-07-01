package tools_test

import (
	"context"
	"encoding/json"
	"testing"

	"charm.land/fantasy"
	"github.com/mattdurham/wllr/modules/sdk"
	"github.com/mattdurham/wllr/modules/tools"
)

func TestParseInputSchema_Empty(t *testing.T) {
	params, required, err := tools.ParseInputSchema(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(params) != 0 {
		t.Errorf("expected empty params, got %v", params)
	}
	if len(required) != 0 {
		t.Errorf("expected empty required, got %v", required)
	}
	if required == nil {
		t.Error("expected required to be a non-nil empty slice")
	}
}

func TestParseInputSchema_MissingRequiredNonNil(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{}}`)
	_, required, err := tools.ParseInputSchema(schema)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(required) != 0 {
		t.Errorf("expected empty required, got %v", required)
	}
	if required == nil {
		t.Fatal("expected required to be a non-nil empty slice")
	}
	data, err := json.Marshal(required)
	if err != nil {
		t.Fatalf("marshal required: %v", err)
	}
	if string(data) != "[]" {
		t.Fatalf("required marshaled as %s, want []", data)
	}
}

func TestParseInputSchema_WithProperties(t *testing.T) {
	schema := json.RawMessage(`{
		"properties": {"foo": {"type": "string"}},
		"required": ["foo"]
	}`)
	params, required, err := tools.ParseInputSchema(schema)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := params["foo"]; !ok {
		t.Error("expected 'foo' in params")
	}
	if len(required) != 1 || required[0] != "foo" {
		t.Errorf("expected required=[foo], got %v", required)
	}
}

func TestParseInputSchema_InvalidJSON(t *testing.T) {
	_, _, err := tools.ParseInputSchema(json.RawMessage(`{not json`))
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestBuildFantasyTools_NilHost(t *testing.T) {
	result := tools.BuildFantasyTools(nil, "agent1", nil)
	if result != nil {
		t.Errorf("expected nil for nil host, got %v", result)
	}
}

// TestSDKToolAdapter_Info verifies tool info passthrough.
func TestSDKToolAdapter_Info(t *testing.T) {
	schema := json.RawMessage(`{"properties":{"cmd":{"type":"string"}},"required":["cmd"]}`)
	tool := sdk.Tool{Name: "exec", Description: "run a command", InputSchema: schema}

	ft, err := tools.NewSDKToolAdapter(tool, nil, "agent1")
	if err != nil {
		t.Fatalf("NewSDKToolAdapter: %v", err)
	}
	info := ft.Info()
	if info.Name != "exec" {
		t.Errorf("expected name 'exec', got %q", info.Name)
	}
	if info.Description != "run a command" {
		t.Errorf("expected desc 'run a command', got %q", info.Description)
	}
	if _, ok := info.Parameters["cmd"]; !ok {
		t.Error("expected 'cmd' in parameters")
	}
}

func TestSDKToolAdapter_Run_NilHost(t *testing.T) {
	tool := sdk.Tool{Name: "test", Description: "test tool"}
	ft, err := tools.NewSDKToolAdapter(tool, nil, "agent1")
	if err != nil {
		t.Fatalf("NewSDKToolAdapter: %v", err)
	}
	resp, err := ft.Run(context.Background(), fantasy.ToolCall{ID: "c1", Name: "test", Input: `{}`})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// With nil host, should return an error response.
	if !resp.IsError {
		t.Error("expected error response for nil host")
	}
}
