package sdk_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/mattdurham/wllr/modules/sdk"
)

func TestEventJSONRoundTrip(t *testing.T) {
	events := []struct {
		name    string
		evtType sdk.EventType
		payload any
	}{
		{
			name:    "session_start",
			evtType: sdk.EventSessionStart,
			payload: sdk.SessionStartPayload{Reason: "new_session"},
		},
		{
			name:    "before_agent_start",
			evtType: sdk.EventBeforeAgentStart,
			payload: sdk.BeforeAgentStartPayload{Prompt: "hello", SystemPrompt: "be helpful"},
		},
		{
			name:    "before_provider_request",
			evtType: sdk.EventBeforeProviderRequest,
			payload: sdk.BeforeProviderRequestPayload{
				Messages: []sdk.Message{{Role: sdk.RoleUser, Content: "hi"}},
				Model:    "claude-sonnet",
			},
		},
		{
			name:    "after_provider_response",
			evtType: sdk.EventAfterProviderResponse,
			payload: sdk.AfterProviderResponsePayload{Usage: sdk.UsageStats{InputTokens: 10, OutputTokens: 20}},
		},
		{
			name:    "on_tool_call",
			evtType: sdk.EventOnToolCall,
			payload: sdk.OnToolCallPayload{
				ToolCallID: "tc-1",
				ToolName:   "search",
				Input:      json.RawMessage(`{"q":"foo"}`),
			},
		},
		{
			name:    "on_tool_result",
			evtType: sdk.EventOnToolResult,
			payload: sdk.OnToolResultPayload{ToolCallID: "tc-1", Result: "bar", IsError: false},
		},
		{
			name:    "message_start",
			evtType: sdk.EventMessageStart,
			payload: sdk.MessageStartPayload{Role: "assistant"},
		},
		{
			name:    "message_end",
			evtType: sdk.EventMessageEnd,
			payload: sdk.MessageEndPayload{Role: "assistant", Content: "hello world"},
		},
		{
			name:    "shutdown",
			evtType: sdk.EventShutdown,
			payload: sdk.ShutdownPayload{Reason: "quit"},
		},
	}

	for _, tc := range events {
		t.Run(tc.name, func(t *testing.T) {
			payloadBytes, err := json.Marshal(tc.payload)
			if err != nil {
				t.Fatalf("marshal payload: %v", err)
			}
			evt := sdk.Event{
				Type:    tc.evtType,
				Payload: json.RawMessage(payloadBytes),
			}
			data, err := json.Marshal(evt)
			if err != nil {
				t.Fatalf("marshal event: %v", err)
			}
			var got sdk.Event
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("unmarshal event: %v", err)
			}
			if got.Type != tc.evtType {
				t.Errorf("type: got %q, want %q", got.Type, tc.evtType)
			}
			if string(got.Payload) != string(payloadBytes) {
				t.Errorf("payload: got %s, want %s", got.Payload, payloadBytes)
			}
		})
	}
}

func TestEventResponseRoundTrip(t *testing.T) {
	cases := []sdk.EventResponse{
		{Cancel: true},
		{Block: true},
		{Error: "something went wrong"},
		{Payload: json.RawMessage(`{"model":"local"}`)},
		{Block: true, Error: "blocked by policy"},
		{},
	}
	for _, c := range cases {
		data, err := json.Marshal(c)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var got sdk.EventResponse
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if got.Cancel != c.Cancel || got.Block != c.Block || got.Error != c.Error {
			t.Errorf("flags: got %+v, want %+v", got, c)
		}
		if string(got.Payload) != string(c.Payload) {
			t.Errorf("payload: got %q, want %q", got.Payload, c.Payload)
		}
	}
}

func TestEventResponseOmitsPayloadWhenEmpty(t *testing.T) {
	data, _ := json.Marshal(sdk.EventResponse{Block: true})
	if strings.Contains(string(data), "payload") {
		t.Fatalf("empty payload must be omitted: %s", data)
	}
	observe, _ := json.Marshal(sdk.EventResponse{})
	if string(observe) != "{}" {
		t.Fatalf("zero EventResponse must marshal to {}, got %s", observe)
	}
}

func TestMessageRoundTrip(t *testing.T) {
	msg := sdk.Message{Role: sdk.RoleUser, Content: "hello"}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got sdk.Message
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Role != sdk.RoleUser || got.Content != "hello" {
		t.Errorf("got %+v", got)
	}
}

func TestMessageType_ZeroValueIsNormal(t *testing.T) {
	// A Message with no Type set should marshal without a "type" field (omitempty),
	// and unmarshal back to Type == "".
	msg := sdk.Message{Role: sdk.RoleUser, Content: "hello"}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// Verify no "type" key in JSON output.
	raw := string(data)
	if contains(raw, `"type"`) {
		t.Errorf("zero-value MessageType must not appear in JSON, got: %s", raw)
	}
	var got sdk.Message
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Type != "" {
		t.Errorf("Type: got %q, want empty string", got.Type)
	}
}

func TestMessageType_SystemRoundtrip(t *testing.T) {
	msg := sdk.Message{Role: sdk.RoleUser, Content: `{"event":"test"}`, Type: sdk.MessageTypeSystem}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got sdk.Message
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Type != sdk.MessageTypeSystem {
		t.Errorf("Type: got %q, want %q", got.Type, sdk.MessageTypeSystem)
	}
}

func TestMessageType_NormalRoundtrip(t *testing.T) {
	msg := sdk.Message{Role: sdk.RoleUser, Content: "hello", Type: sdk.MessageTypeNormal}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got sdk.Message
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Type != sdk.MessageTypeNormal {
		t.Errorf("Type: got %q, want %q", got.Type, sdk.MessageTypeNormal)
	}
}

func TestMessageType_SteeringRoundtrip(t *testing.T) {
	msg := sdk.Message{Role: sdk.RoleUser, Content: "steer me", Type: sdk.MessageTypeSteering}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got sdk.Message
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Type != sdk.MessageTypeSteering {
		t.Errorf("Type: got %q, want %q", got.Type, sdk.MessageTypeSteering)
	}
}

func TestMessageTypeConstants(t *testing.T) {
	if sdk.MessageTypeNormal != "normal" {
		t.Errorf("MessageTypeNormal = %q, want \"normal\"", sdk.MessageTypeNormal)
	}
	if sdk.MessageTypeSteering != "steering" {
		t.Errorf("MessageTypeSteering = %q, want \"steering\"", sdk.MessageTypeSteering)
	}
	if sdk.MessageTypeSystem != "system" {
		t.Errorf("MessageTypeSystem = %q, want \"system\"", sdk.MessageTypeSystem)
	}
}

// contains is a local helper to avoid importing strings in the test file.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsAt(s, substr))
}

func containsAt(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestToolSchemasPreserveRawJSON(t *testing.T) {
	inputRaw := `{"type":"object","properties":{"q":{"type":"string"}}}`
	outputRaw := `{"type":"object","properties":{"result":{"type":"string"}}}`
	tool := sdk.Tool{
		Name:         "search",
		Description:  "search the web",
		InputSchema:  json.RawMessage(inputRaw),
		OutputSchema: json.RawMessage(outputRaw),
	}
	data, err := json.Marshal(tool)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got sdk.Tool
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if string(got.InputSchema) != inputRaw {
		t.Errorf("input_schema: got %s, want %s", got.InputSchema, inputRaw)
	}
	if string(got.OutputSchema) != outputRaw {
		t.Errorf("output_schema: got %s, want %s", got.OutputSchema, outputRaw)
	}
}

func TestHostCallRoundTrip(t *testing.T) {
	req := sdk.HostCallRequest{
		Method: sdk.MethodSubscribe,
		Params: json.RawMessage(`{"event":"session_start"}`),
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal req: %v", err)
	}
	var gotReq sdk.HostCallRequest
	if err := json.Unmarshal(data, &gotReq); err != nil {
		t.Fatalf("unmarshal req: %v", err)
	}
	if gotReq.Method != sdk.MethodSubscribe {
		t.Errorf("method: got %q, want %q", gotReq.Method, sdk.MethodSubscribe)
	}
	if string(gotReq.Params) != `{"event":"session_start"}` {
		t.Errorf("params: got %s", gotReq.Params)
	}

	resp := sdk.HostCallResponse{
		Result: json.RawMessage(`{"ok":true}`),
	}
	data, err = json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal resp: %v", err)
	}
	var gotResp sdk.HostCallResponse
	if err := json.Unmarshal(data, &gotResp); err != nil {
		t.Fatalf("unmarshal resp: %v", err)
	}
	if string(gotResp.Result) != `{"ok":true}` {
		t.Errorf("result: got %s", gotResp.Result)
	}
}

func TestRoleConstants(t *testing.T) {
	if sdk.RoleUser != "user" {
		t.Errorf("RoleUser = %q, want \"user\"", sdk.RoleUser)
	}
	if sdk.RoleAssistant != "assistant" {
		t.Errorf("RoleAssistant = %q, want \"assistant\"", sdk.RoleAssistant)
	}
}

func TestEventTypeConstants(t *testing.T) {
	types := []sdk.EventType{
		sdk.EventSessionStart,
		sdk.EventBeforeAgentStart,
		sdk.EventBeforeProviderRequest,
		sdk.EventAfterProviderResponse,
		sdk.EventOnToolCall,
		sdk.EventOnToolResult,
		sdk.EventMessageStart,
		sdk.EventMessageEnd,
		sdk.EventShutdown,
		sdk.EventBeforeToolCall,
		sdk.EventAfterToolCall,
		sdk.EventOnCommand,
		sdk.EventTick,
		sdk.EventContextUsage,
		sdk.EventToken,
		sdk.EventNotify,
		sdk.EventLog,
		sdk.EventModelChanged,
	}
	if len(types) != 18 {
		t.Errorf("expected 18 event types, got %d", len(types))
	}
	for _, et := range types {
		if et == "" {
			t.Errorf("event type constant is empty string")
		}
	}
}

func TestMethodConstants(t *testing.T) {
	methods := []string{
		sdk.MethodSubscribe,
		sdk.MethodRegisterTool,
		sdk.MethodRegisterCommand,
		sdk.MethodSendMessage,
		sdk.MethodSetStatus,
		sdk.MethodNotify,
		sdk.MethodToolResult,
		sdk.MethodStoreSet,
		sdk.MethodStoreGet,
		sdk.MethodAbort,
		sdk.MethodRequestPermission,
		sdk.MethodBeforeToolCall,
		sdk.MethodAfterToolCall,
	}
	if len(methods) != 13 {
		t.Errorf("expected 13 method constants, got %d", len(methods))
	}
	for _, m := range methods {
		if m == "" {
			t.Errorf("method constant is empty string")
		}
	}
}

func TestMethodAgentTeamConstants(t *testing.T) {
	for _, tc := range []struct{ name, want string }{
		{sdk.MethodAgentSpawn, "agent_spawn"},
		{sdk.MethodAgentClose, "agent_close"},
		{sdk.MethodAgentSendMessage, "agent_send_message"},
		{sdk.MethodAgentList, "agent_list"},
		{sdk.MethodAgentTokenCount, "agent_token_count"},
		{sdk.MethodTeamCreate, "team_create"},
		{sdk.MethodTeamClose, "team_close"},
		{sdk.MethodTeamAddMember, "team_add_member"},
		{sdk.MethodTeamRemoveMember, "team_remove_member"},
		{sdk.MethodTeamGetInfo, "team_get_info"},
		{sdk.MethodTeamList, "team_list"},
	} {
		if tc.name != tc.want {
			t.Errorf("constant value mismatch: got %q, want %q", tc.name, tc.want)
		}
	}
}
