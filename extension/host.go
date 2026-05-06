package extension

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"context"
	crand "crypto/rand"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"

	"github.com/mattdurham/wllr/sdk"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

// Extension wraps a loaded WASM module.
type Extension struct {
	name   string
	module api.Module
	store  *Store

	subMu         sync.RWMutex
	subscriptions map[sdk.EventType]bool

	// trusted is true for built-in extensions loaded via LoadBytes with trusted=true.
	// Trusted extensions bypass permission checks.
	trusted bool

	// permissions holds the declared permissions for untrusted extensions.
	// Map entries are set to true for each granted permission.
	permissions map[sdk.Permission]bool
}

// HasPermission reports whether the extension holds permission p.
// Trusted extensions always return true. Untrusted extensions must have p
// explicitly granted in their permissions map.
func (e *Extension) HasPermission(p sdk.Permission) bool {
	if e.trusted {
		return true
	}
	return e.permissions[p]
}

// toolResult holds the result of a tool execution sent back by an extension.
type toolResult struct {
	Result  string
	IsError bool
}

// RegisteredToolInfo pairs a registered tool with the name of the extension
// that registered it.  OwnerName is empty for tools registered outside of an
// extension context.
type RegisteredToolInfo struct {
	Tool      sdk.Tool
	OwnerName string
}

// Host manages a collection of WASM extensions.
type Host struct {
	mu         sync.RWMutex
	runtime    wazero.Runtime
	extensions []*Extension
	logger     *slog.Logger

	// Registered tools keyed by name (for duplicate detection).
	registeredTools map[string]sdk.Tool

	// toolOwners maps tool name to the name of the extension that registered it.
	toolOwners map[string]string

	// pendingTools holds channels waiting for tool_result responses.
	// Keyed by toolCallID.
	pendingMu    sync.Mutex
	pendingTools map[string]chan toolResult

	// Callbacks set by the harness.
	OnSendMessage     func(msg sdk.Message)
	OnSetStatus       func(key, value string)
	OnRegisterTool    func(tool sdk.Tool) error
	OnRegisterCommand func(name string, desc string)
	OnNotify          func(text string)
	OnAbort           func()
	OnToolResult      func(toolCallID, result string, isError bool)
	OnAfterToolCall   func(toolCallID, toolName, result string, isError bool)
	OnSetSystemPrompt func(prompt string)
	OnExec            func(command, dir string) (string, error)
	OnGetEnv          func(name string) (string, error)
	OnConfigRead      func(group string) (json.RawMessage, error)

	// Agent management callbacks. Set by the pool layer.
	// OnAgentSpawn creates a new named agent with the given system prompt and model.
	OnAgentSpawn func(id, name, systemPrompt, modelName string) error
	// OnAgentClose closes and removes a named agent.
	OnAgentClose func(id string) error
	// OnAgentSendMessage queues a plain-text message into a named agent's inbox.
	OnAgentSendMessage func(id, message string) error
	// OnAgentList returns a snapshot of all live agents.
	OnAgentList func() ([]AgentInfo, error)
	// OnAgentTokenCount returns the total token count across all agents.
	OnAgentTokenCount func() int64

	// Team management callbacks. Set by the pool layer.
	// OnTeamCreate creates a new named team.
	OnTeamCreate func(id, name string) error
	// OnTeamClose cancels all member agents and removes the team.
	OnTeamClose func(id string) error
	// OnTeamAddMember adds an agent to an existing team.
	OnTeamAddMember func(teamID, agentID string) error
	// OnTeamRemoveMember removes an agent from a team (does not close the agent).
	OnTeamRemoveMember func(teamID, agentID string) error
}

// AgentInfo describes a running agent.
type AgentInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// NewHost creates a Host and installs the "env" host module into a fresh wazero runtime.
// Pass nil to use slog.Default().
func NewHost(logger *slog.Logger) *Host {
	if logger == nil {
		logger = slog.Default()
	}
	h := &Host{
		logger:          logger,
		registeredTools: make(map[string]sdk.Tool),
		toolOwners:      make(map[string]string),
		pendingTools:    make(map[string]chan toolResult),
	}
	h.runtime = wazero.NewRuntime(context.Background())
	// WASI is required by native Go WASM modules (GOOS=wasip1).
	if _, err := wasi_snapshot_preview1.Instantiate(context.Background(), h.runtime); err != nil {
		h.logger.Error("extension: install wasi module", "err", err)
	}
	if err := h.installEnvModule(); err != nil {
		h.logger.Error("extension: install env module", "err", err)
	}
	return h
}

// installEnvModule registers the "env" host module that extensions import.
func (h *Host) installEnvModule() error {
	ctx := context.Background()
	_, err := h.runtime.NewHostModuleBuilder("env").
		NewFunctionBuilder().
		WithFunc(h.hostLogImpl).
		Export("host_log").
		NewFunctionBuilder().
		WithFunc(h.hostAllocImpl).
		Export("host_alloc").
		NewFunctionBuilder().
		WithFunc(h.hostFreeImpl).
		Export("host_free").
		NewFunctionBuilder().
		WithFunc(h.hostCallImpl).
		Export("host_call").
		Instantiate(ctx)
	return err
}

// slogLevel converts the extension ABI level (0=debug,1=info,2=warn,3=error) to slog.Level.
func slogLevel(level uint32) slog.Level {
	switch level {
	case 0:
		return slog.LevelDebug
	case 1:
		return slog.LevelInfo
	case 2:
		return slog.LevelWarn
	default:
		return slog.LevelError
	}
}

// hostLogImpl is the host_log import: host_log(level, ptr, length).
// Logs are emitted via slog with "extension" attribute set to the module name.
func (h *Host) hostLogImpl(ctx context.Context, m api.Module, level, ptr, length uint32) {
	mem := m.Memory()
	if mem == nil {
		return
	}
	bs, ok := mem.Read(ptr, length)
	if !ok {
		h.logger.Error("extension: host_log: invalid memory read", "extension", m.Name())
		return
	}
	h.logger.Log(ctx, slogLevel(level), string(bs), "extension", m.Name())
}

// hostAllocImpl is the host_alloc import: unused in v1, always returns 0.
func (h *Host) hostAllocImpl(_ context.Context, _ api.Module, _ uint32) uint32 {
	return 0
}

// hostFreeImpl is the host_free import: no-op in v1.
func (h *Host) hostFreeImpl(_ context.Context, _ api.Module, _ uint32) {}

// hostCallImpl is the host_call import.
// host_call(req_ptr, req_len, resp_ptr_ptr, resp_len_ptr) -> status
func (h *Host) hostCallImpl(ctx context.Context, m api.Module, reqPtr, reqLen, respPtrPtr, respLenPtr uint32) uint32 {
	mem := m.Memory()
	if mem == nil {
		return uint32(sdk.ErrGeneral)
	}

	// Read request JSON from WASM memory.
	reqBytes, ok := mem.Read(reqPtr, reqLen)
	if !ok {
		h.logger.Error("extension: host_call: invalid request memory read")
		return uint32(sdk.ErrGeneral)
	}

	var req sdk.HostCallRequest
	if err := json.Unmarshal(reqBytes, &req); err != nil {
		h.logger.Error("extension: host_call: unmarshal request", "err", err)
		return uint32(sdk.ErrGeneral)
	}

	// Find the calling extension.
	ext := h.findExtensionByModule(m)

	// Dispatch to the router.
	resp := h.routeHostCall(ctx, m, ext, req)

	// Write response back into WASM memory if caller wants it.
	if respPtrPtr == 0 && respLenPtr == 0 {
		return uint32(sdk.ErrOK)
	}

	respBytes, err := json.Marshal(resp)
	if err != nil {
		h.logger.Error("extension: host_call: marshal response", "err", err)
		return uint32(sdk.ErrGeneral)
	}

	// Allocate memory in the extension's WASM module for the response.
	allocFn := m.ExportedFunction("_alloc")
	if allocFn == nil {
		return uint32(sdk.ErrGeneral)
	}
	allocResult, err := allocFn.Call(ctx, uint64(len(respBytes)))
	if err != nil || len(allocResult) == 0 {
		h.logger.Error("extension: host_call: _alloc failed", "err", err)
		return uint32(sdk.ErrGeneral)
	}
	respPtr := uint32(allocResult[0])
	if respPtr == 0 {
		// Extension's _alloc returned 0 — can't write response.
		return uint32(sdk.ErrOK)
	}

	if !mem.Write(respPtr, respBytes) {
		h.logger.Error("extension: host_call: write response to WASM memory failed")
		return uint32(sdk.ErrGeneral)
	}

	// Write respPtr and respLen into the caller-supplied pointer slots.
	if !mem.WriteUint32Le(respPtrPtr, respPtr) {
		return uint32(sdk.ErrGeneral)
	}
	if !mem.WriteUint32Le(respLenPtr, uint32(len(respBytes))) {
		return uint32(sdk.ErrGeneral)
	}

	return uint32(sdk.ErrOK)
}

// routeHostCall dispatches req to the appropriate handler and returns a response.
func (h *Host) routeHostCall(ctx context.Context, _ api.Module, ext *Extension, req sdk.HostCallRequest) sdk.HostCallResponse {
	switch req.Method {
	case sdk.MethodSubscribe:
		return h.handleSubscribe(ext, req)

	case sdk.MethodRegisterTool:
		return h.handleRegisterTool(ext, req)

	case sdk.MethodRegisterCommand:
		return h.handleRegisterCommand(req)

	case sdk.MethodSendMessage:
		return h.handleSendMessage(req)

	case sdk.MethodSetStatus:
		return h.handleSetStatus(req)

	case sdk.MethodNotify:
		return h.handleNotify(req)

	case sdk.MethodToolResult:
		return h.handleToolResult(req)

	case sdk.MethodStoreSet:
		return h.handleStoreSet(ext, req)

	case sdk.MethodStoreGet:
		return h.handleStoreGet(ext, req)

	case sdk.MethodAbort:
		if h.OnAbort != nil {
			h.OnAbort()
		}
		return sdk.HostCallResponse{}

	case sdk.MethodRequestPermission:
		return h.handleRequestPermission(ext, req)

	case sdk.MethodSetSystemPrompt:
		return h.handleSetSystemPrompt(req)

	case sdk.MethodExec:
		return h.handleExec(ctx, ext, req)

	case sdk.MethodGetEnv:
		return h.handleGetEnv(ext, req)

	case sdk.MethodConfigRead:
		return h.handleConfigRead(ext)

	// Agent management methods.
	case sdk.MethodAgentSpawn:
		return h.handleAgentSpawn(req)

	case sdk.MethodAgentClose:
		return h.handleAgentClose(req)

	case sdk.MethodAgentSendMessage:
		return h.handleAgentSendMessage(req)

	case sdk.MethodAgentList:
		return h.handleAgentList()

	case sdk.MethodAgentTokenCount:
		return h.handleAgentTokenCount()

	// Team management methods.
	case sdk.MethodTeamCreate:
		return h.handleTeamCreate(req)

	case sdk.MethodTeamClose:
		return h.handleTeamClose(req)

	case sdk.MethodTeamAddMember:
		return h.handleTeamAddMember(req)

	case sdk.MethodTeamRemoveMember:
		return h.handleTeamRemoveMember(req)

	default:
		return sdk.HostCallResponse{Error: fmt.Sprintf("unknown method: %s", req.Method)}
	}
}

func (h *Host) handleSubscribe(ext *Extension, req sdk.HostCallRequest) sdk.HostCallResponse {
	if ext == nil {
		return sdk.HostCallResponse{Error: "subscribe: unknown extension"}
	}
	var params struct {
		Event sdk.EventType `json:"event"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return sdk.HostCallResponse{Error: fmt.Sprintf("subscribe: %v", err)}
	}
	ext.subMu.Lock()
	ext.subscriptions[params.Event] = true
	ext.subMu.Unlock()
	return sdk.HostCallResponse{}
}

func (h *Host) handleRegisterTool(ext *Extension, req sdk.HostCallRequest) sdk.HostCallResponse {
	var tool sdk.Tool
	if err := json.Unmarshal(req.Params, &tool); err != nil {
		return sdk.HostCallResponse{Error: fmt.Sprintf("register_tool: %v", err)}
	}
	h.mu.Lock()
	_, exists := h.registeredTools[tool.Name]
	if !exists || tool.Override {
		h.registeredTools[tool.Name] = tool
		if ext != nil {
			h.toolOwners[tool.Name] = ext.name
		}
	}
	h.mu.Unlock()
	if exists && !tool.Override {
		return sdk.HostCallResponse{Error: fmt.Sprintf("tool already registered: %s", tool.Name)}
	}
	if h.OnRegisterTool != nil {
		if err := h.OnRegisterTool(tool); err != nil {
			return sdk.HostCallResponse{Error: err.Error()}
		}
	}
	return sdk.HostCallResponse{}
}

func (h *Host) handleRegisterCommand(req sdk.HostCallRequest) sdk.HostCallResponse {
	var params struct {
		Name string `json:"name"`
		Desc string `json:"description"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return sdk.HostCallResponse{Error: fmt.Sprintf("register_command: %v", err)}
	}
	if h.OnRegisterCommand != nil {
		h.OnRegisterCommand(params.Name, params.Desc)
	}
	return sdk.HostCallResponse{}
}

func (h *Host) handleSendMessage(req sdk.HostCallRequest) sdk.HostCallResponse {
	var msg sdk.Message
	if err := json.Unmarshal(req.Params, &msg); err != nil {
		return sdk.HostCallResponse{Error: fmt.Sprintf("send_message: %v", err)}
	}
	if h.OnSendMessage != nil {
		h.OnSendMessage(msg)
	}
	return sdk.HostCallResponse{}
}

func (h *Host) handleSetStatus(req sdk.HostCallRequest) sdk.HostCallResponse {
	var params struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return sdk.HostCallResponse{Error: fmt.Sprintf("set_status: %v", err)}
	}
	if h.OnSetStatus != nil {
		h.OnSetStatus(params.Key, params.Value)
	}
	return sdk.HostCallResponse{}
}

func (h *Host) handleNotify(req sdk.HostCallRequest) sdk.HostCallResponse {
	var params struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return sdk.HostCallResponse{Error: fmt.Sprintf("notify: %v", err)}
	}
	if h.OnNotify != nil {
		h.OnNotify(params.Text)
	}
	return sdk.HostCallResponse{}
}

func (h *Host) handleSetSystemPrompt(req sdk.HostCallRequest) sdk.HostCallResponse {
	if h.OnSetSystemPrompt == nil {
		return sdk.HostCallResponse{Error: "set_system_prompt: not supported by host"}
	}
	var params struct {
		Prompt string `json:"prompt"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return sdk.HostCallResponse{Error: fmt.Sprintf("set_system_prompt: %v", err)}
	}
	h.OnSetSystemPrompt(params.Prompt)
	return sdk.HostCallResponse{}
}

func (h *Host) handleExec(ctx context.Context, ext *Extension, req sdk.HostCallRequest) sdk.HostCallResponse {
	if ext == nil || !ext.HasPermission(sdk.PermExec) {
		return sdk.HostCallResponse{Error: "exec: permission denied: requires exec"}
	}
	if h.OnExec == nil {
		return sdk.HostCallResponse{Error: "exec: not supported by host"}
	}
	var params struct {
		Command string `json:"command"`
		Dir     string `json:"dir"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return sdk.HostCallResponse{Error: fmt.Sprintf("exec: %v", err)}
	}
	output, err := h.OnExec(params.Command, params.Dir)
	if err != nil {
		result, _ := json.Marshal(map[string]string{"output": output, "error": err.Error()})
		return sdk.HostCallResponse{Result: result}
	}
	result, _ := json.Marshal(map[string]string{"output": output})
	return sdk.HostCallResponse{Result: result}
}

func (h *Host) handleGetEnv(ext *Extension, req sdk.HostCallRequest) sdk.HostCallResponse {
	if h.OnGetEnv == nil {
		return sdk.HostCallResponse{Error: "get_env: not supported by host"}
	}
	var params struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return sdk.HostCallResponse{Error: fmt.Sprintf("get_env: %v", err)}
	}
	output, err := h.OnGetEnv(params.Name)
	if err != nil {
		return sdk.HostCallResponse{Error: err.Error()}
	}
	result, _ := json.Marshal(map[string]string{"value": output})
	return sdk.HostCallResponse{Result: result}
}

func (h *Host) handleConfigRead(ext *Extension) sdk.HostCallResponse {
	if h.OnConfigRead == nil {
		return sdk.HostCallResponse{Error: "config_read: not supported by host"}
	}
	group := ""
	if ext != nil {
		group = ext.name
	}
	data, err := h.OnConfigRead(group)
	if err != nil {
		return sdk.HostCallResponse{Error: fmt.Sprintf("config_read: %v", err)}
	}
	if data == nil {
		data = json.RawMessage("{}")
	}
	return sdk.HostCallResponse{Result: data}
}

func (h *Host) handleRequestPermission(ext *Extension, req sdk.HostCallRequest) sdk.HostCallResponse {
	if ext == nil {
		return sdk.HostCallResponse{Error: "request_permission: unknown extension"}
	}
	var params struct {
		Permission sdk.Permission `json:"permission"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return sdk.HostCallResponse{Error: fmt.Sprintf("request_permission: %v", err)}
	}
	if !ext.HasPermission(params.Permission) {
		return sdk.HostCallResponse{Error: fmt.Sprintf("permission denied: %s", params.Permission)}
	}
	return sdk.HostCallResponse{}
}

func (h *Host) handleToolResult(req sdk.HostCallRequest) sdk.HostCallResponse {
	var params struct {
		ToolCallID string `json:"tool_call_id"`
		Result     string `json:"result"`
		IsError    bool   `json:"is_error"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return sdk.HostCallResponse{Error: fmt.Sprintf("tool_result: %v", err)}
	}

	// Signal any goroutine waiting in ExecuteTool.
	h.pendingMu.Lock()
	ch, hasPending := h.pendingTools[params.ToolCallID]
	if hasPending {
		delete(h.pendingTools, params.ToolCallID)
	}
	h.pendingMu.Unlock()
	if hasPending {
		ch <- toolResult{Result: params.Result, IsError: params.IsError}
	}

	if h.OnToolResult != nil {
		h.OnToolResult(params.ToolCallID, params.Result, params.IsError)
	}
	return sdk.HostCallResponse{}
}

func (h *Host) handleStoreSet(ext *Extension, req sdk.HostCallRequest) sdk.HostCallResponse {
	if ext == nil {
		return sdk.HostCallResponse{Error: "store_set: unknown extension"}
	}
	var params struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return sdk.HostCallResponse{Error: fmt.Sprintf("store_set: %v", err)}
	}
	ext.store.Set(params.Key, params.Value)
	return sdk.HostCallResponse{}
}

func (h *Host) handleStoreGet(ext *Extension, req sdk.HostCallRequest) sdk.HostCallResponse {
	if ext == nil {
		return sdk.HostCallResponse{Error: "store_get: unknown extension"}
	}
	var params struct {
		Key string `json:"key"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return sdk.HostCallResponse{Error: fmt.Sprintf("store_get: %v", err)}
	}
	v, ok := ext.store.Get(params.Key)
	if !ok {
		return sdk.HostCallResponse{Error: "not found"}
	}
	result, _ := json.Marshal(map[string]string{"value": v})
	return sdk.HostCallResponse{Result: json.RawMessage(result)}
}

func (h *Host) handleAgentSpawn(req sdk.HostCallRequest) sdk.HostCallResponse {
	if h.OnAgentSpawn == nil {
		return sdk.HostCallResponse{Error: "agent_spawn: not supported by host"}
	}
	var params struct {
		ID           string `json:"id"`
		Name         string `json:"name"`
		SystemPrompt string `json:"system_prompt"`
		ModelName    string `json:"model_name"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return sdk.HostCallResponse{Error: fmt.Sprintf("agent_spawn: %v", err)}
	}
	if err := h.OnAgentSpawn(params.ID, params.Name, params.SystemPrompt, params.ModelName); err != nil {
		return sdk.HostCallResponse{Error: err.Error()}
	}
	return sdk.HostCallResponse{}
}

func (h *Host) handleAgentClose(req sdk.HostCallRequest) sdk.HostCallResponse {
	if h.OnAgentClose == nil {
		return sdk.HostCallResponse{Error: "agent_close: not supported by host"}
	}
	var params struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return sdk.HostCallResponse{Error: fmt.Sprintf("agent_close: %v", err)}
	}
	if err := h.OnAgentClose(params.ID); err != nil {
		return sdk.HostCallResponse{Error: err.Error()}
	}
	return sdk.HostCallResponse{}
}

func (h *Host) handleAgentSendMessage(req sdk.HostCallRequest) sdk.HostCallResponse {
	if h.OnAgentSendMessage == nil {
		return sdk.HostCallResponse{Error: "agent_send_message: not supported by host"}
	}
	var params struct {
		ID      string `json:"id"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return sdk.HostCallResponse{Error: fmt.Sprintf("agent_send_message: %v", err)}
	}
	if err := h.OnAgentSendMessage(params.ID, params.Message); err != nil {
		return sdk.HostCallResponse{Error: err.Error()}
	}
	return sdk.HostCallResponse{}
}

func (h *Host) handleAgentList() sdk.HostCallResponse {
	if h.OnAgentList == nil {
		return sdk.HostCallResponse{Error: "agent_list: not supported by host"}
	}
	agents, err := h.OnAgentList()
	if err != nil {
		return sdk.HostCallResponse{Error: fmt.Sprintf("agent_list: %v", err)}
	}
	result, _ := json.Marshal(map[string][]AgentInfo{"agents": agents})
	return sdk.HostCallResponse{Result: result}
}

func (h *Host) handleAgentTokenCount() sdk.HostCallResponse {
	if h.OnAgentTokenCount == nil {
		return sdk.HostCallResponse{Error: "agent_token_count: not supported by host"}
	}
	count := h.OnAgentTokenCount()
	result, _ := json.Marshal(map[string]int64{"count": count})
	return sdk.HostCallResponse{Result: result}
}

func (h *Host) handleTeamCreate(req sdk.HostCallRequest) sdk.HostCallResponse {
	if h.OnTeamCreate == nil {
		return sdk.HostCallResponse{Error: "team_create: not supported by host"}
	}
	var params struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return sdk.HostCallResponse{Error: fmt.Sprintf("team_create: %v", err)}
	}
	if err := h.OnTeamCreate(params.ID, params.Name); err != nil {
		return sdk.HostCallResponse{Error: err.Error()}
	}
	return sdk.HostCallResponse{}
}

func (h *Host) handleTeamClose(req sdk.HostCallRequest) sdk.HostCallResponse {
	if h.OnTeamClose == nil {
		return sdk.HostCallResponse{Error: "team_close: not supported by host"}
	}
	var params struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return sdk.HostCallResponse{Error: fmt.Sprintf("team_close: %v", err)}
	}
	if err := h.OnTeamClose(params.ID); err != nil {
		return sdk.HostCallResponse{Error: err.Error()}
	}
	return sdk.HostCallResponse{}
}

func (h *Host) handleTeamAddMember(req sdk.HostCallRequest) sdk.HostCallResponse {
	if h.OnTeamAddMember == nil {
		return sdk.HostCallResponse{Error: "team_add_member: not supported by host"}
	}
	var params struct {
		TeamID  string `json:"team_id"`
		AgentID string `json:"agent_id"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return sdk.HostCallResponse{Error: fmt.Sprintf("team_add_member: %v", err)}
	}
	if err := h.OnTeamAddMember(params.TeamID, params.AgentID); err != nil {
		return sdk.HostCallResponse{Error: err.Error()}
	}
	return sdk.HostCallResponse{}
}

func (h *Host) handleTeamRemoveMember(req sdk.HostCallRequest) sdk.HostCallResponse {
	if h.OnTeamRemoveMember == nil {
		return sdk.HostCallResponse{Error: "team_remove_member: not supported by host"}
	}
	var params struct {
		TeamID  string `json:"team_id"`
		AgentID string `json:"agent_id"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return sdk.HostCallResponse{Error: fmt.Sprintf("team_remove_member: %v", err)}
	}
	if err := h.OnTeamRemoveMember(params.TeamID, params.AgentID); err != nil {
		return sdk.HostCallResponse{Error: err.Error()}
	}
	return sdk.HostCallResponse{}
}

// findExtensionByModule returns the Extension whose module has the given name.
func (h *Host) findExtensionByModule(m api.Module) *Extension {
	h.mu.RLock()
	defer h.mu.RUnlock()
	name := m.Name()
	for _, ext := range h.extensions {
		if ext.module.Name() == name {
			return ext
		}
	}
	return nil
}

// Load reads the WASM file at path, loads a companion manifest if present,
// validates exports, calls _init, and registers the extension.
// User extensions loaded via Load are not trusted; they receive only the
// permissions declared in their companion <basename>.json manifest.
func (h *Host) Load(ctx context.Context, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("load extension %s: %w", path, err)
	}

	// Load optional manifest for permission declarations.
	perms := loadManifestPermissions(path, h.logger)

	return h.loadExtension(ctx, path, data, false, perms)
}

// LoadBytes instantiates a WASM module from in-memory bytes.
// Use this for embedded built-in extensions.
// When trusted is true the extension is granted all permissions without a
// manifest; when trusted is false the caller must supply the permissions slice.
func (h *Host) LoadBytes(ctx context.Context, name string, data []byte, trusted bool) error {
	return h.loadExtension(ctx, name, data, trusted, nil)
}

// loadExtension is the common implementation for Load and LoadBytes.
func (h *Host) loadExtension(ctx context.Context, name string, data []byte, trusted bool, perms []sdk.Permission) error {
	modName := moduleNameFromPath(name)

	mod, err := h.runtime.InstantiateWithConfig(ctx, data,
		wazero.NewModuleConfig().WithName(modName).WithStartFunctions().WithRandSource(crand.Reader))
	if err != nil {
		return fmt.Errorf("instantiate extension %s: %w", name, err)
	}

	if err := validateExports(mod); err != nil {
		_ = mod.Close(ctx)
		return fmt.Errorf("validate extension %s: %w", name, err)
	}

	permMap := make(map[sdk.Permission]bool, len(perms))
	for _, p := range perms {
		permMap[p] = true
	}

	ext := &Extension{
		name:          modName,
		module:        mod,
		subscriptions: make(map[sdk.EventType]bool),
		store:         NewStore(),
		trusted:       trusted,
		permissions:   permMap,
	}

	// Register ext before calling _init so host_call works.
	h.mu.Lock()
	h.extensions = append(h.extensions, ext)
	h.mu.Unlock()

	if err := callInit(ctx, mod); err != nil {
		// Remove on _init failure.
		h.mu.Lock()
		h.removeExtension(ext)
		h.mu.Unlock()
		_ = mod.Close(ctx)
		return fmt.Errorf("init extension %s: %w", name, err)
	}

	return nil
}

// loadManifestPermissions reads a companion JSON manifest at
// "<basename>.json" alongside the WASM file and returns the declared
// permissions. Missing or invalid manifest files are silently ignored.
func loadManifestPermissions(wasmPath string, logger *slog.Logger) []sdk.Permission {
	manifestPath := strings.TrimSuffix(wasmPath, ".wasm") + ".json"
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil
	}
	var manifest sdk.ExtensionManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		logger.Warn("extension: manifest parse error", "path", manifestPath, "err", err)
		return nil
	}
	return manifest.Permissions
}

// ExecuteTool dispatches a tool call to subscribed extensions and blocks until
// an extension returns the result via the tool_result host_call method.
// It dispatches EventBeforeToolCall so extensions may intercept the call.
// The call is cancelled and an error is returned if ctx is cancelled.
func (h *Host) ExecuteTool(ctx context.Context, toolCallID, toolName string, input json.RawMessage) (toolResult, error) {
	ch := make(chan toolResult, 1)

	h.pendingMu.Lock()
	h.pendingTools[toolCallID] = ch
	h.pendingMu.Unlock()

	// Dispatch before_tool_call event. An extension may call tool_result
	// synchronously within _on_event, which will write to ch.
	payload, _ := json.Marshal(sdk.BeforeToolCallPayload{
		ToolCallID: toolCallID,
		ToolName:   toolName,
		Input:      input,
	})
	evt := sdk.Event{Type: sdk.EventBeforeToolCall, Payload: payload}
	_, _ = h.DispatchEvent(ctx, evt)

	// Block until the extension returns the result or the context is cancelled.
	select {
	case result := <-ch:
		// Dispatch after_tool_call event to all subscribed extensions.
		afterPayload, _ := json.Marshal(sdk.AfterToolCallPayload{
			ToolCallID: toolCallID,
			ToolName:   toolName,
			Result:     result.Result,
			IsError:    result.IsError,
		})
		afterEvt := sdk.Event{Type: sdk.EventAfterToolCall, Payload: afterPayload}
		_, _ = h.DispatchEvent(ctx, afterEvt)

		// Invoke the harness callback if set.
		if h.OnAfterToolCall != nil {
			h.OnAfterToolCall(toolCallID, toolName, result.Result, result.IsError)
		}
		return result, nil
	case <-ctx.Done():
		// Clean up the pending entry on cancellation.
		h.pendingMu.Lock()
		delete(h.pendingTools, toolCallID)
		h.pendingMu.Unlock()
		return toolResult{}, ctx.Err()
	}
}

// GetRegisteredTools returns a snapshot of all currently registered tools.
func (h *Host) GetRegisteredTools() []sdk.Tool {
	h.mu.RLock()
	tools := make([]sdk.Tool, 0, len(h.registeredTools))
	for _, t := range h.registeredTools {
		tools = append(tools, t)
	}
	h.mu.RUnlock()
	return tools
}

// RegisteredTools returns a snapshot of all currently registered tools with
// their owner extension names.  OwnerName is empty for tools registered
// outside an extension context.
func (h *Host) RegisteredTools() []RegisteredToolInfo {
	h.mu.RLock()
	infos := make([]RegisteredToolInfo, 0, len(h.registeredTools))
	for name, t := range h.registeredTools {
		infos = append(infos, RegisteredToolInfo{
			Tool:      t,
			OwnerName: h.toolOwners[name],
		})
	}
	h.mu.RUnlock()
	return infos
}

// DispatchEvent dispatches evt to all subscribed extensions and returns their responses.
// A WASM trap (error from _on_event) is logged and does not stop dispatch to other extensions.
func (h *Host) DispatchEvent(ctx context.Context, evt sdk.Event) ([]sdk.EventResponse, error) {
	evtJSON, err := json.Marshal(evt)
	if err != nil {
		return nil, fmt.Errorf("marshal event: %w", err)
	}

	h.mu.RLock()
	exts := make([]*Extension, len(h.extensions))
	copy(exts, h.extensions)
	h.mu.RUnlock()

	var responses []sdk.EventResponse
	for _, ext := range exts {
		ext.subMu.RLock()
		subscribed := ext.subscriptions[evt.Type]
		ext.subMu.RUnlock()
		if !subscribed {
			continue
		}
		resp, dispErr := h.dispatchToExtension(ctx, ext, evtJSON)
		if dispErr != nil {
			h.logger.Warn("extension: dispatch error", "extension", ext.name, "err", dispErr)
			continue
		}
		responses = append(responses, resp)
	}
	return responses, nil
}

// dispatchToExtension calls _on_event on a single extension.
func (h *Host) dispatchToExtension(ctx context.Context, ext *Extension, evtJSON []byte) (sdk.EventResponse, error) {
	mod := ext.module
	mem := mod.Memory()
	if mem == nil {
		return sdk.EventResponse{}, fmt.Errorf("extension %s has no memory", ext.name)
	}

	// Allocate memory in the extension for the event JSON.
	allocFn := mod.ExportedFunction("_alloc")
	if allocFn == nil {
		return sdk.EventResponse{}, fmt.Errorf("extension %s missing _alloc", ext.name)
	}
	allocResult, err := allocFn.Call(ctx, uint64(len(evtJSON)))
	if err != nil {
		return sdk.EventResponse{}, fmt.Errorf("extension %s _alloc: %w", ext.name, err)
	}
	if len(allocResult) == 0 {
		return sdk.EventResponse{}, fmt.Errorf("extension %s _alloc returned no results", ext.name)
	}
	evtPtr := uint32(allocResult[0])

	if evtPtr == 0 {
		// Extension's _alloc returned 0 — can't safely deliver event.
		return sdk.EventResponse{}, nil
	}

	if !mem.Write(evtPtr, evtJSON) {
		return sdk.EventResponse{}, fmt.Errorf("extension %s: write event to memory failed", ext.name)
	}

	// Call _on_event(ptr, len) → resp_ptr.
	onEvent := mod.ExportedFunction("_on_event")
	if onEvent == nil {
		return sdk.EventResponse{}, fmt.Errorf("extension %s missing _on_event", ext.name)
	}
	results, err := onEvent.Call(ctx, uint64(evtPtr), uint64(len(evtJSON)))
	if err != nil {
		return sdk.EventResponse{}, fmt.Errorf("extension %s _on_event trap: %w", ext.name, err)
	}

	// Free the event memory.
	if freeFn := mod.ExportedFunction("_free"); freeFn != nil {
		_, _ = freeFn.Call(ctx, uint64(evtPtr))
	}

	if len(results) == 0 || results[0] == 0 {
		return sdk.EventResponse{}, nil
	}

	// Read response JSON from WASM memory.
	respPtr := uint32(results[0])

	// Read length: we don't know the length directly, so read until null byte or
	// use a size prefix. Per the ABI, the extension must ensure the response is
	// valid JSON. We read up to 64KB and find the JSON boundary.
	// Simpler: the extension stores resp JSON starting at respPtr; scan for end.
	// We read a reasonable max (64KB) and try to unmarshal.
	respBytes := readNullTerminatedOrJSON(mem, respPtr)

	freeFn := mod.ExportedFunction("_free")
	if freeFn != nil {
		_, _ = freeFn.Call(ctx, uint64(respPtr))
	}

	var resp sdk.EventResponse
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		h.logger.Warn("extension: unmarshal _on_event response", "extension", ext.name, "err", err)
		return sdk.EventResponse{}, nil
	}
	return resp, nil
}

// readNullTerminatedOrJSON reads bytes from WASM memory starting at ptr,
// trying to find a complete JSON object. Returns at most 64KB.
// It correctly handles braces inside JSON string values.
func readNullTerminatedOrJSON(mem api.Memory, ptr uint32) []byte {
	const maxLen = 65536
	// Read max available.
	avail, ok := mem.Read(ptr, maxLen)
	if !ok {
		// Try smaller reads if at end of memory.
		for size := uint32(maxLen / 2); size >= 1; size /= 2 {
			avail, ok = mem.Read(ptr, size)
			if ok {
				break
			}
		}
	}
	if len(avail) == 0 {
		return nil
	}
	// Find the end of the JSON object, skipping characters inside string literals.
	var depth int
	inString := false
	escaped := false
	for i, b := range avail {
		if escaped {
			escaped = false
			continue
		}
		if b == '\\' && inString {
			escaped = true
			continue
		}
		if b == '"' {
			inString = !inString
			continue
		}
		if inString {
			continue
		}
		switch b {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return avail[:i+1]
			}
		}
	}
	return avail
}

// Reload closes all extensions and reloads them from the given paths.
func (h *Host) Reload(ctx context.Context, paths []string) error {
	h.mu.Lock()
	old := h.extensions
	h.extensions = nil
	h.registeredTools = make(map[string]sdk.Tool)
	h.toolOwners = make(map[string]string)
	h.mu.Unlock()

	for _, ext := range old {
		if err := ext.module.Close(ctx); err != nil {
			h.logger.Warn("extension: close error", "extension", ext.name, "err", err)
		}
	}

	for _, path := range paths {
		if err := h.Load(ctx, path); err != nil {
			h.logger.Error("extension: reload error", "err", err)
		}
	}
	return nil
}

// Close shuts down all extensions and the wazero runtime.
func (h *Host) Close(ctx context.Context) error {
	return h.runtime.Close(ctx)
}

// removeExtension removes ext from h.extensions. Caller must hold h.mu.Lock().
func (h *Host) removeExtension(target *Extension) {
	filtered := make([]*Extension, 0, len(h.extensions))
	for _, ext := range h.extensions {
		if ext != target {
			filtered = append(filtered, ext)
		}
	}
	h.extensions = filtered
}

// moduleNameFromPath derives a unique module name from a file path.
func moduleNameFromPath(path string) string {
	// Use the full path to avoid name collisions.
	return path
}
