//go:build wasip1

// Sigil observability extension — real SDK integration.
// Wires generation and tool execution recording to Grafana Cloud via sigil-sdk-go.
//
// Config (env vars):
//
//	AGENTO11Y_ENDPOINT   — required, e.g. https://sigil-prod-eu-west-2.grafana.net
//	AGENTO11Y_PROTOCOL   — http or https (default: http)
//	AGENTO11Y_AUTH_MODE  — "basic" (default)
//	AGENTO11Y_AUTH_TENANT_ID — tenant ID
//	AGENTO11Y_AUTH_TOKEN     — API key
//	DEBUG_LOG              — set to 1 for Sigil lifecycle and HTTP status logs
//
// If AGENTO11Y_ENDPOINT is not set the extension loads but all tool
// calls return {"status":"disabled","reason":"AGENTO11Y_ENDPOINT not set"}.
//
// The SDK owns batching, retries, and generation export. The extension only
// supplies the SDK configuration and forwards recorder lifecycle calls.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	sdk "github.com/grafana/sigil-sdk/go/sigil"
)

// ---------------------------------------------------------------------------
// State
// ---------------------------------------------------------------------------

var (
	client       *sdk.Client
	once         sync.Once
	guidanceOnce sync.Once
	enabled      bool
	agentName    string
	agentVer     string
	debugLog     bool
)

type debugRoundTripper struct {
	base http.RoundTripper
}

func (t debugRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.base.RoundTrip(req)
	if err != nil {
		Logf(3, "HTTP %s %s failed: %v", req.Method, req.URL, err)
		return nil, err
	}
	Logf(1, "HTTP %s %s -> %d", req.Method, req.URL, resp.StatusCode)
	return resp, nil
}

func debugf(format string, args ...any) {
	if debugLog {
		Logf(1, format, args...)
	}
}

// activeGens tracks in-flight generations: conversation_id -> recorder.
var (
	genMu       sync.Mutex
	activeGens  = make(map[string]*sdk.GenerationRecorder)
	activeConvs = make(map[string]string)
)

type automaticGeneration struct {
	id       string
	recorder *sdk.GenerationRecorder
	input    []sdk.Message
	output   strings.Builder
}

var automaticGen struct {
	sync.Mutex
	current *automaticGeneration
}

// activeTools tracks in-flight tool executions: tool_id -> generation_id.
var (
	toolMu      sync.Mutex
	activeTools = make(map[string]*sdk.ToolExecutionRecorder)
)

func initClient() {
	once.Do(func() {
		debugLog = os.Getenv("DEBUG_LOG") == "1"
		endpoint := os.Getenv("AGENTO11Y_ENDPOINT")
		if endpoint == "" {
			debugf("disabled: AGENTO11Y_ENDPOINT is not set")
			enabled = false
			return
		}

		protocol := os.Getenv("AGENTO11Y_PROTOCOL")
		if protocol == "" {
			protocol = "http"
		}

		authMode := os.Getenv("AGENTO11Y_AUTH_MODE")
		if authMode == "" {
			authMode = "basic"
		}

		tenantID := os.Getenv("AGENTO11Y_AUTH_TENANT_ID")
		token := os.Getenv("AGENTO11Y_AUTH_TOKEN")

		agentName = os.Getenv("WLLR_AGENT_NAME")
		if agentName == "" {
			agentName = "wllr"
		}
		agentVer = os.Getenv("WLLR_AGENT_VERSION")
		if agentVer == "" {
			agentVer = "dev"
		}

		cfg := sdk.DefaultConfig()
		cfg.GenerationExport.Endpoint = endpoint
		if protocol == "grpc" {
			cfg.GenerationExport.Protocol = sdk.GenerationExportProtocolGRPC
		} else {
			cfg.GenerationExport.Protocol = sdk.GenerationExportProtocolHTTP
		}
		cfg.GenerationExport.Auth = sdk.AuthConfig{
			TenantID: tenantID,
		}
		switch strings.ToLower(authMode) {
		case "none":
			cfg.GenerationExport.Auth.Mode = sdk.ExportAuthModeNone
		case "bearer":
			cfg.GenerationExport.Auth.Mode = sdk.ExportAuthModeBearer
			cfg.GenerationExport.Auth.BearerToken = token
		default:
			cfg.GenerationExport.Auth.Mode = sdk.ExportAuthModeBasic
			cfg.GenerationExport.Auth.BasicPassword = token
		}
		if protocol != "https" && protocol != "grpc" {
			insecure := true
			cfg.GenerationExport.Insecure = &insecure
		}
		cfg.AgentName = agentName
		cfg.AgentVersion = agentVer
		if debugLog && cfg.GenerationExport.Protocol == sdk.GenerationExportProtocolHTTP &&
			http.DefaultTransport != nil {
			http.DefaultTransport = debugRoundTripper{base: http.DefaultTransport}
		}
		debugf(
			"initializing endpoint=%s protocol=%s auth_mode=%s tenant=%s",
			endpoint,
			cfg.GenerationExport.Protocol,
			authMode,
			tenantID,
		)
		client = sdk.NewClient(cfg)
		enabled = true
		debugf("client ready")
	})
}

func ensureClient() bool {
	initClient()
	return enabled && client != nil
}

func startAutomaticGeneration(messages []ProviderMessage, model string) {
	if !ensureClient() {
		return
	}
	provider, modelName := splitModel(model)
	genID := fmt.Sprintf("wllr-auto-%d", time.Now().UnixNano())
	_, recorder := client.StartStreamingGeneration(context.Background(), sdk.GenerationStart{
		ID:             genID,
		ConversationID: "wllr",
		Model:          sdk.ModelRef{Provider: provider, Name: modelName},
	})
	automaticGen.Lock()
	automaticGen.current = &automaticGeneration{id: genID, recorder: recorder, input: providerMessages(messages)}
	automaticGen.Unlock()
	debugf("generation started id=%s model=%s input_messages=%d", genID, model, len(messages))
}

func finishAutomaticGeneration(inputTokens, outputTokens int) {
	automaticGen.Lock()
	current := automaticGen.current
	automaticGen.current = nil
	automaticGen.Unlock()
	if current == nil {
		return
	}
	current.recorder.SetResult(sdk.Generation{
		Input:  current.input,
		Output: []sdk.Message{sdk.AssistantTextMessage(current.output.String())},
		Usage:  sdk.TokenUsage{InputTokens: int64(inputTokens), OutputTokens: int64(outputTokens)},
	}, nil)
	current.recorder.End()
	debugf(
		"generation ended id=%s input_tokens=%d output_tokens=%d output_bytes=%d",
		current.id,
		inputTokens,
		outputTokens,
		current.output.Len(),
	)
}

func splitModel(model string) (string, string) {
	parts := strings.SplitN(model, "/", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return "unknown", model
}

func providerMessages(messages []ProviderMessage) []sdk.Message {
	out := make([]sdk.Message, 0, len(messages))
	for _, message := range messages {
		role := sdk.RoleUser
		if message.Role == "assistant" {
			role = sdk.RoleAssistant
		}
		out = append(out, sdk.Message{Role: role, Parts: []sdk.Part{sdk.TextPart(message.Content)}})
	}
	return out
}

func startAutomaticTool(payload json.RawMessage) {
	var request struct {
		ToolCallID string          `json:"tool_call_id"`
		ToolName   string          `json:"tool_name"`
		Input      json.RawMessage `json:"input"`
	}
	if json.Unmarshal(payload, &request) != nil || request.ToolCallID == "" || request.ToolName == "" ||
		strings.HasPrefix(request.ToolName, "sigil_") {
		return
	}
	if !ensureClient() {
		return
	}
	_, recorder := client.StartToolExecution(context.Background(), sdk.ToolExecutionStart{
		ToolName:       request.ToolName,
		ToolCallID:     request.ToolCallID,
		ConversationID: "wllr",
	})
	toolMu.Lock()
	activeTools[request.ToolCallID] = recorder
	toolMu.Unlock()
	debugf("tool started id=%s name=%s", request.ToolCallID, request.ToolName)
}

func finishAutomaticTool(callID, toolName, result string, isError bool) {
	if strings.HasPrefix(toolName, "sigil_") {
		return
	}
	toolMu.Lock()
	recorder := activeTools[callID]
	delete(activeTools, callID)
	toolMu.Unlock()
	if recorder == nil {
		return
	}
	if isError {
		recorder.SetExecError(fmt.Errorf("%s", result))
	}
	recorder.SetResult(sdk.ToolExecutionEnd{Result: result})
	recorder.End()
	debugf("tool ended id=%s name=%s error=%t", callID, toolName, isError)
}

// ---------------------------------------------------------------------------
// Tool handlers
// ---------------------------------------------------------------------------

func handleSigilStartGeneration(callID, toolName string, input json.RawMessage) (string, bool) {
	if !ensureClient() {
		return `{"status":"disabled","reason":"AGENTO11Y_ENDPOINT not set"}`, false
	}

	var req struct {
		ConversationID string `json:"conversation_id"`
		ModelProvider  string `json:"model_provider"`
		ModelName      string `json:"model_name"`
		AgentName      string `json:"agent_name,omitempty"`
		AgentVersion   string `json:"agent_version,omitempty"`
		SystemPrompt   string `json:"system_prompt,omitempty"`
	}
	if err := json.Unmarshal(input, &req); err != nil {
		return fmt.Sprintf(`{"error":"invalid args: %v"}`, err), true
	}

	if req.ConversationID == "" || req.ModelProvider == "" || req.ModelName == "" {
		return `{"error":"conversation_id, model_provider, and model_name are required"}`, true
	}

	genID := fmt.Sprintf("wllr-%d", time.Now().UnixNano())
	_, recorder := client.StartGeneration(context.Background(), sdk.GenerationStart{
		ID:             genID,
		ConversationID: req.ConversationID,
		Model:          sdk.ModelRef{Provider: req.ModelProvider, Name: req.ModelName},
		AgentName:      req.AgentName,
		AgentVersion:   req.AgentVersion,
		SystemPrompt:   req.SystemPrompt,
	})

	genMu.Lock()
	activeGens[genID] = recorder
	activeConvs[req.ConversationID] = genID
	genMu.Unlock()

	return fmt.Sprintf(`{"generation_id":"%s"}`, genID), false
}

func handleSigilEndGeneration(callID, toolName string, input json.RawMessage) (string, bool) {
	if !ensureClient() {
		return `{"status":"disabled","reason":"AGENTO11Y_ENDPOINT not set"}`, false
	}

	var req struct {
		GenerationID string `json:"generation_id"`
		Output       string `json:"output,omitempty"`
	}
	if err := json.Unmarshal(input, &req); err != nil {
		return fmt.Sprintf(`{"error":"invalid args: %v"}`, err), true
	}

	if req.GenerationID == "" {
		return `{"error":"generation_id is required"}`, true
	}

	// Find the recorder by generation_id
	genMu.Lock()
	var recorder *sdk.GenerationRecorder
	recorder = activeGens[req.GenerationID]
	genMu.Unlock()

	if recorder == nil {
		return fmt.Sprintf(`{"error":"no active generation for id %s"}`, req.GenerationID), true
	}

	// Set result if provided
	if req.Output != "" {
		recorder.SetResult(sdk.Generation{
			Output: []sdk.Message{sdk.AssistantTextMessage(req.Output)},
		}, nil)
	}

	recorder.End()

	// Clean up generation
	genMu.Lock()
	delete(activeGens, req.GenerationID)
	for conversationID, generationID := range activeConvs {
		if generationID == req.GenerationID {
			delete(activeConvs, conversationID)
		}
	}
	genMu.Unlock()

	// Clean up any tools associated with this generation
	toolMu.Lock()
	for tid, tool := range activeTools {
		if tool == nil {
			delete(activeTools, tid)
		}
	}
	toolMu.Unlock()

	return `{"status":"ended"}`, false
}

func handleSigilSetResult(callID, toolName string, input json.RawMessage) (string, bool) {
	if !ensureClient() {
		return `{"status":"disabled","reason":"AGENTO11Y_ENDPOINT not set"}`, false
	}

	var req struct {
		GenerationID string `json:"generation_id"`
		Output       string `json:"output"`
	}
	if err := json.Unmarshal(input, &req); err != nil {
		return fmt.Sprintf(`{"error":"invalid args: %v"}`, err), true
	}

	if req.GenerationID == "" {
		return `{"error":"generation_id is required"}`, true
	}

	genMu.Lock()
	var recorder *sdk.GenerationRecorder
	recorder = activeGens[req.GenerationID]
	genMu.Unlock()

	if recorder == nil {
		return fmt.Sprintf(`{"error":"no active generation for id %s"}`, req.GenerationID), true
	}

	recorder.SetResult(sdk.Generation{
		Output: []sdk.Message{sdk.AssistantTextMessage(req.Output)},
	}, nil)

	return `{"status":"set"}`, false
}

func handleSigilStartToolExecution(callID, toolName string, input json.RawMessage) (string, bool) {
	if !ensureClient() {
		return `{"status":"disabled","reason":"AGENTO11Y_ENDPOINT not set"}`, false
	}

	var req struct {
		ToolName       string `json:"tool_name"`
		ConversationID string `json:"conversation_id,omitempty"`
	}
	if err := json.Unmarshal(input, &req); err != nil {
		return fmt.Sprintf(`{"error":"invalid args: %v"}`, err), true
	}

	if req.ToolName == "" {
		return `{"error":"tool_name is required"}`, true
	}

	// Find the active generation
	genMu.Lock()
	var genRecorder *sdk.GenerationRecorder
	if req.ConversationID != "" {
		if generationID := activeConvs[req.ConversationID]; generationID != "" {
			genRecorder = activeGens[generationID]
		}
	} else {
		// Use the most recent generation (any one)
		for _, r := range activeGens {
			genRecorder = r
			break
		}
	}
	genMu.Unlock()

	if genRecorder == nil {
		return `{"error":"no active generation to nest tool execution under"}`, true
	}

	_, recorder := client.StartToolExecution(context.Background(), sdk.ToolExecutionStart{
		ToolName:       req.ToolName,
		ConversationID: req.ConversationID,
	})
	toolID := fmt.Sprintf("%p", recorder)

	toolMu.Lock()
	activeTools[toolID] = recorder
	toolMu.Unlock()

	return fmt.Sprintf(`{"tool_id":"%s"}`, toolID), false
}

func handleSigilEndToolExecution(callID, toolName string, input json.RawMessage) (string, bool) {
	if !ensureClient() {
		return `{"status":"disabled","reason":"AGENTO11Y_ENDPOINT not set"}`, false
	}

	var req struct {
		ToolID    string          `json:"tool_id"`
		Arguments json.RawMessage `json:"arguments,omitempty"`
		Result    json.RawMessage `json:"result,omitempty"`
		Error     string          `json:"error,omitempty"`
	}
	if err := json.Unmarshal(input, &req); err != nil {
		return fmt.Sprintf(`{"error":"invalid args: %v"}`, err), true
	}

	if req.ToolID == "" {
		return `{"error":"tool_id is required"}`, true
	}

	toolMu.Lock()
	recorder, ok := activeTools[req.ToolID]
	if !ok {
		toolMu.Unlock()
		return fmt.Sprintf(`{"error":"no active tool execution for id %s"}`, req.ToolID), true
	}
	delete(activeTools, req.ToolID)
	toolMu.Unlock()
	if req.Error != "" {
		recorder.SetExecError(fmt.Errorf("%s", req.Error))
	}
	recorder.SetResult(sdk.ToolExecutionEnd{Arguments: req.Arguments, Result: req.Result})
	recorder.End()

	return `{"status":"ended"}`, false
}

// ---------------------------------------------------------------------------
// Extension entry
// ---------------------------------------------------------------------------

func init() {
	// Register tools
	RegisterTool("sigil_start_generation",
		"Start a generation recording for Grafana AI observability. Returns a generation_id used to end the recording.",
		json.RawMessage(`{
			"type": "object",
			"required": ["conversation_id", "model_provider", "model_name"],
			"properties": {
				"conversation_id": {"type": "string", "description": "Conversation ID"},
				"model_provider": {"type": "string", "description": "Model provider name"},
				"model_name": {"type": "string", "description": "Model name"},
				"agent_name": {"type": "string", "description": "Agent name (optional)"},
				"agent_version": {"type": "string", "description": "Agent version (optional)"},
				"system_prompt": {"type": "string", "description": "System prompt (optional)"}
			}
		}`))

	RegisterTool("sigil_end_generation",
		"End a generation recording and finalize the span.",
		json.RawMessage(`{
			"type": "object",
			"required": ["generation_id"],
			"properties": {
				"generation_id": {"type": "string", "description": "Generation ID to end"},
				"output": {"type": "string", "description": "Generation output as JSON array (optional)"}
			}
		}`))

	RegisterTool("sigil_set_result",
		"Set the generation result for Grafana AI observability.",
		json.RawMessage(`{
			"type": "object",
			"required": ["generation_id", "output"],
			"properties": {
				"generation_id": {"type": "string", "description": "Generation ID"},
				"output": {"type": "string", "description": "Output as JSON array"}
			}
		}`))

	RegisterTool("sigil_start_tool_execution",
		"Start a tool execution recording nested under the current generation.",
		json.RawMessage(`{
			"type": "object",
			"required": ["tool_name"],
			"properties": {
				"tool_name": {"type": "string", "description": "Tool name"},
				"conversation_id": {"type": "string", "description": "Conversation ID (optional)"}
			}
		}`))

	RegisterTool("sigil_end_tool_execution",
		"End a tool execution recording.",
		json.RawMessage(`{
			"type": "object",
			"required": ["tool_id"],
			"properties": {
				"tool_id": {"type": "string", "description": "Tool execution ID"}
			}
		}`))

	// Register /sigil command
	RegisterCommand("sigil", "Sigil observability status")
	OnInterceptProviderRequest(
		func(messages []ProviderMessage, model string) ([]ProviderMessage, string, bool, string) {
			startAutomaticGeneration(messages, model)
			return nil, "", false, ""
		},
	)
	OnToken(func(_, text string) {
		automaticGen.Lock()
		if automaticGen.current != nil {
			automaticGen.current.output.WriteString(text)
		}
		automaticGen.Unlock()
	})
	OnAfterProviderResponse(finishAutomaticGeneration)
	OnBeforeToolCall(startAutomaticTool)
	OnAfterToolCall(finishAutomaticTool)
	OnShutdown(func(reason string) {
		debugf("shutdown started reason=%s", reason)
		finishAutomaticGeneration(0, 0)
		toolMu.Lock()
		for callID, recorder := range activeTools {
			recorder.SetExecError(fmt.Errorf("wllr shutting down before tool completion: %s", callID))
			recorder.End()
		}
		activeTools = make(map[string]*sdk.ToolExecutionRecorder)
		toolMu.Unlock()
		if client != nil {
			if err := client.Shutdown(context.Background()); err != nil {
				Logf(3, "shutdown flush failed: %v", err)
			}
		}
		debugf("shutdown complete")
	})
	OnBeforeAgentStart(func(string) {
		guidanceOnce.Do(func() {
			AppendSystemPrompt(`## Sigil observability
When observability is enabled, use the sigil tools to instrument important LLM generations and tool executions. Start a generation with sigil_start_generation, set its output with sigil_set_result, and finish it with sigil_end_generation. For tools, use sigil_start_tool_execution before execution and sigil_end_tool_execution afterward. Always pass the returned IDs and report errors in the end call.`)
		})
	})

	// Handle tool calls
	OnToolCall(func(callID, name string, input json.RawMessage) (string, bool) {
		switch name {
		case "sigil_start_generation":
			return handleSigilStartGeneration(callID, name, input)
		case "sigil_end_generation":
			return handleSigilEndGeneration(callID, name, input)
		case "sigil_set_result":
			return handleSigilSetResult(callID, name, input)
		case "sigil_start_tool_execution":
			return handleSigilStartToolExecution(callID, name, input)
		case "sigil_end_tool_execution":
			return handleSigilEndToolExecution(callID, name, input)
		default:
			return "", false
		}
	})

	// Handle /sigil command
	OnCommand("sigil", func(args []string) {
		initClient()
		if !enabled {
			Notify("⚠ Sigil observability is disabled (set AGENTO11Y_ENDPOINT to enable)")
			return
		}

		genMu.Lock()
		genCount := len(activeGens)
		genMu.Unlock()
		toolMu.Lock()
		toolCount := len(activeTools)
		toolMu.Unlock()

		msg := fmt.Sprintf("Sigil observability enabled. "+
			"Endpoint: %s | Active generations: %d | Active tools: %d",
			os.Getenv("AGENTO11Y_ENDPOINT"), genCount, toolCount)
		Notify(msg)
	})
}

func main() {
	Log(1, "sigil extension loaded")
}
