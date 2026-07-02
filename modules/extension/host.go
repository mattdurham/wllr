package extension

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"context"
	crand "crypto/rand"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/mattdurham/wllr/modules/sdk"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

// Extension wraps a loaded WASM module.

// permissions holds the declared permissions for untrusted extensions.
// Map entries are set to true for each granted permission.

// callMu serializes calls into the WASM module. WASM linear memory is
// shared within a module instance, so concurrent _on_event or host_call
// invocations race on the module's globals (pinned map, handler maps).

// trusted is true for built-in extensions loaded via LoadBytes with trusted=true.
// Trusted extensions bypass permission checks.

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

// RegisteredToolInfo pairs a registered tool with the name of the extension
// that registered it.  OwnerName is empty for tools registered outside of an
// extension context.

// Host manages a collection of WASM extensions.
type Host struct {
	runtime wazero.Runtime

	logger   *slog.Logger
	dispatch map[string]func(ctx context.Context, ext *Extension, req sdk.HostCallRequest) sdk.HostCallResponse

	// Bus is the shared event stream. All DispatchEvent calls publish here
	// in addition to dispatching to WASM extensions.
	Bus *EventBus

	// Registered tools keyed by name (for duplicate detection).
	registeredTools map[string]sdk.Tool

	// toolOwners maps tool name to the name of the extension that registered it.
	toolOwners map[string]string

	pendingTools map[string]chan toolResult

	// nativeTools holds Go-native tool handlers registered via RegisterNativeTool.
	// These are checked before WASM dispatch in ExecuteTool.
	nativeTools map[string]func(ctx context.Context, input json.RawMessage) (string, bool)

	// Interface bridges — set once at startup via Set* methods.
	agents       AgentBridge
	teams        TeamBridge
	capabilities CapabilityProvider
	ui           UIBridge
	mcp          MCPBridge

	extensions    []*Extension
	nativeToolsMu sync.RWMutex

	mu sync.RWMutex

	// pendingTools holds channels waiting for tool_result responses.
	// Keyed by toolCallID.
	pendingMu sync.Mutex
}

// SetLogger replaces the logger used for host-internal diagnostics (dispatch
// errors, etc.). Useful when logging is configured after NewHost (e.g. the log
// handler needs the host to dispatch EventLog). Not safe to call concurrently
// with dispatch; call once during startup.
func (h *Host) SetLogger(logger *slog.Logger) {
	if logger != nil {
		h.logger = logger
	}
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
		nativeTools:     make(map[string]func(ctx context.Context, input json.RawMessage) (string, bool)),
		Bus:             NewEventBus(),
	}
	h.dispatch = h.buildDispatch()
	cacheDir := filepath.Join(os.TempDir(), "wllr-wasm-cache")
	rCfg := wazero.NewRuntimeConfig()
	if cache, cacheErr := wazero.NewCompilationCacheWithDir(cacheDir); cacheErr == nil {
		rCfg = rCfg.WithCompilationCache(cache)
	}
	h.runtime = wazero.NewRuntimeWithConfig(context.Background(), rCfg)
	// WASI is required by native Go WASM modules (GOOS=wasip1).
	if _, err := wasi_snapshot_preview1.Instantiate(context.Background(), h.runtime); err != nil {
		h.logger.Error("extension: install wasi module", "err", err)
	}
	if err := h.installEnvModule(); err != nil {
		h.logger.Error("extension: install env module", "err", err)
	}
	return h
}

// SetAgentBridge installs the agent management bridge.
// Replaces the individual OnAgentSpawn, OnAgentClose, OnAgentSendMessage,
// OnAgentRun, OnAgentList, OnAgentTokenCount callback fields.
// Must be called before loading extensions.
func (h *Host) SetAgentBridge(b AgentBridge) {
	h.mu.Lock()
	h.agents = b
	h.mu.Unlock()
}

// SetTeamBridge installs the team management bridge.
// Replaces the individual OnTeamCreate, OnTeamClose, OnTeamAddMember,
// OnTeamRemoveMember, OnTeamGetInfo, OnTeamList callback fields.
// Must be called before loading extensions.
func (h *Host) SetTeamBridge(b TeamBridge) {
	h.mu.Lock()
	h.teams = b
	h.mu.Unlock()
}

// SetUIBridge installs the UI bridge.
// Replaces the UI-related On* callback fields.
// Must be called before loading extensions.
func (h *Host) SetUIBridge(b UIBridge) {
	h.mu.Lock()
	h.ui = b
	h.mu.Unlock()
}

// SetCapabilities installs the capability provider.
// Replaces OnExec, OnGetEnv, OnReadFile, OnWriteFile, OnHTTPPost, OnConfigRead.
// Must be called before loading extensions.
func (h *Host) SetCapabilities(c CapabilityProvider) {
	h.mu.Lock()
	h.capabilities = c
	h.mu.Unlock()
}

// SetMCPBridge installs the MCP bridge.
// Replaces OnMCPSpawn, OnMCPClose, OnMCPSend, OnMCPRead.
// Must be called before loading extensions.
func (h *Host) SetMCPBridge(m MCPBridge) {
	h.mu.Lock()
	h.mcp = m
	h.mu.Unlock()
}

// AgentBridgeSet reports whether an AgentBridge has been installed.
// Used in tests to verify wiring.
func (h *Host) AgentBridgeSet() bool {
	return h.agentBridge() != nil
}

// agentBridge snapshots the current AgentBridge under h.mu.RLock.
func (h *Host) agentBridge() AgentBridge {
	h.mu.RLock()
	b := h.agents
	h.mu.RUnlock()
	return b
}

// teamBridge snapshots the current TeamBridge under h.mu.RLock.
func (h *Host) teamBridge() TeamBridge {
	h.mu.RLock()
	b := h.teams
	h.mu.RUnlock()
	return b
}

// uiBridge snapshots the current UIBridge under h.mu.RLock.
func (h *Host) uiBridge() UIBridge {
	h.mu.RLock()
	b := h.ui
	h.mu.RUnlock()
	return b
}

// capabilityProvider snapshots the current CapabilityProvider under h.mu.RLock.
func (h *Host) capabilityProvider() CapabilityProvider {
	h.mu.RLock()
	c := h.capabilities
	h.mu.RUnlock()
	return c
}

// mcpBridge snapshots the current MCPBridge under h.mu.RLock.
func (h *Host) mcpBridge() MCPBridge {
	h.mu.RLock()
	m := h.mcp
	h.mu.RUnlock()
	return m
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
	// Use the friendly short name if available.
	extName := m.Name()
	if ext := h.findExtensionByModule(m); ext != nil {
		extName = ext.name
	}
	h.logger.Log(ctx, slogLevel(level), string(bs), "extension", extName)
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

// routeHostCall dispatches req to the appropriate handler via the dispatch map.
func (h *Host) routeHostCall(ctx context.Context, _ api.Module, ext *Extension, req sdk.HostCallRequest) sdk.HostCallResponse {
	if fn, ok := h.dispatch[req.Method]; ok {
		return fn(ctx, ext, req)
	}
	return sdk.HostCallResponse{Error: fmt.Sprintf("unknown method: %s", req.Method)}
}

// buildDispatch constructs the method-to-handler dispatch map used by routeHostCall.
func (h *Host) buildDispatch() map[string]func(ctx context.Context, ext *Extension, req sdk.HostCallRequest) sdk.HostCallResponse {
	return map[string]func(ctx context.Context, ext *Extension, req sdk.HostCallRequest) sdk.HostCallResponse{
		sdk.MethodSubscribe: func(_ context.Context, ext *Extension, req sdk.HostCallRequest) sdk.HostCallResponse {
			return h.handleSubscribe(ext, req)
		},
		sdk.MethodRegisterTool: func(_ context.Context, ext *Extension, req sdk.HostCallRequest) sdk.HostCallResponse {
			return h.handleRegisterTool(ext, req)
		},
		sdk.MethodRegisterCommand: func(_ context.Context, _ *Extension, req sdk.HostCallRequest) sdk.HostCallResponse {
			return h.handleRegisterCommand(req)
		},
		sdk.MethodSendMessage: func(_ context.Context, _ *Extension, req sdk.HostCallRequest) sdk.HostCallResponse {
			return h.handleSendMessage(req)
		},
		sdk.MethodSetStatus: func(_ context.Context, _ *Extension, req sdk.HostCallRequest) sdk.HostCallResponse {
			return h.handleSetStatus(req)
		},
		sdk.MethodNotify: func(_ context.Context, _ *Extension, req sdk.HostCallRequest) sdk.HostCallResponse {
			return h.handleNotify(req)
		},
		sdk.MethodToolResult: func(_ context.Context, _ *Extension, req sdk.HostCallRequest) sdk.HostCallResponse {
			return h.handleToolResult(req)
		},
		sdk.MethodStoreSet: func(_ context.Context, ext *Extension, req sdk.HostCallRequest) sdk.HostCallResponse {
			return h.handleStoreSet(ext, req)
		},
		sdk.MethodStoreGet: func(_ context.Context, ext *Extension, req sdk.HostCallRequest) sdk.HostCallResponse {
			return h.handleStoreGet(ext, req)
		},
		sdk.MethodAbort: func(_ context.Context, _ *Extension, _ sdk.HostCallRequest) sdk.HostCallResponse {
			if h.uiBridge() != nil {
				h.uiBridge().Abort()
			}
			return sdk.HostCallResponse{}
		},
		sdk.MethodRequestPermission: func(_ context.Context, ext *Extension, req sdk.HostCallRequest) sdk.HostCallResponse {
			return h.handleRequestPermission(ext, req)
		},
		sdk.MethodModal: func(_ context.Context, _ *Extension, req sdk.HostCallRequest) sdk.HostCallResponse {
			return h.handleModal(req)
		},
		sdk.MethodSetSystemPrompt: func(_ context.Context, _ *Extension, req sdk.HostCallRequest) sdk.HostCallResponse {
			return h.handleSetSystemPrompt(req)
		},
		sdk.MethodAppendSystemPrompt: func(_ context.Context, _ *Extension, req sdk.HostCallRequest) sdk.HostCallResponse {
			return h.handleAppendSystemPrompt(req)
		},
		sdk.MethodExec: func(ctx context.Context, ext *Extension, req sdk.HostCallRequest) sdk.HostCallResponse {
			return h.handleExec(ctx, ext, req)
		},
		sdk.MethodGetEnv: func(_ context.Context, ext *Extension, req sdk.HostCallRequest) sdk.HostCallResponse {
			return h.handleGetEnv(ext, req)
		},
		sdk.MethodReadFile: func(_ context.Context, ext *Extension, req sdk.HostCallRequest) sdk.HostCallResponse {
			return h.handleReadFile(ext, req)
		},
		sdk.MethodWriteFile: func(_ context.Context, ext *Extension, req sdk.HostCallRequest) sdk.HostCallResponse {
			return h.handleWriteFile(ext, req)
		},
		sdk.MethodAppendFile: func(_ context.Context, ext *Extension, req sdk.HostCallRequest) sdk.HostCallResponse {
			return h.handleAppendFile(ext, req)
		},
		sdk.MethodHTTPPost: func(_ context.Context, ext *Extension, req sdk.HostCallRequest) sdk.HostCallResponse {
			return h.handleHTTPPost(ext, req)
		},
		sdk.MethodConfigRead: func(_ context.Context, ext *Extension, _ sdk.HostCallRequest) sdk.HostCallResponse {
			return h.handleConfigRead(ext)
		},
		sdk.MethodAgentSpawn: func(_ context.Context, _ *Extension, req sdk.HostCallRequest) sdk.HostCallResponse {
			return h.handleAgentSpawn(req)
		},
		sdk.MethodAgentClose: func(_ context.Context, _ *Extension, req sdk.HostCallRequest) sdk.HostCallResponse {
			return h.handleAgentClose(req)
		},
		sdk.MethodAgentSendMessage: func(_ context.Context, _ *Extension, req sdk.HostCallRequest) sdk.HostCallResponse {
			return h.handleAgentSendMessage(req)
		},
		sdk.MethodAgentDeliver: func(_ context.Context, _ *Extension, req sdk.HostCallRequest) sdk.HostCallResponse {
			return h.handleAgentDeliver(req)
		},
		sdk.MethodAgentRun: func(_ context.Context, _ *Extension, req sdk.HostCallRequest) sdk.HostCallResponse {
			return h.handleAgentRun(req)
		},
		sdk.MethodAgentList: func(_ context.Context, _ *Extension, _ sdk.HostCallRequest) sdk.HostCallResponse {
			return h.handleAgentList()
		},
		sdk.MethodAgentTokenCount: func(_ context.Context, _ *Extension, _ sdk.HostCallRequest) sdk.HostCallResponse {
			return h.handleAgentTokenCount()
		},
		sdk.MethodTeamCreate: func(_ context.Context, _ *Extension, req sdk.HostCallRequest) sdk.HostCallResponse {
			return h.handleTeamCreate(req)
		},
		sdk.MethodTeamClose: func(_ context.Context, _ *Extension, req sdk.HostCallRequest) sdk.HostCallResponse {
			return h.handleTeamClose(req)
		},
		sdk.MethodTeamAddMember: func(_ context.Context, _ *Extension, req sdk.HostCallRequest) sdk.HostCallResponse {
			return h.handleTeamAddMember(req)
		},
		sdk.MethodTeamRemoveMember: func(_ context.Context, _ *Extension, req sdk.HostCallRequest) sdk.HostCallResponse {
			return h.handleTeamRemoveMember(req)
		},
		sdk.MethodTeamGetInfo: func(_ context.Context, _ *Extension, req sdk.HostCallRequest) sdk.HostCallResponse {
			return h.handleTeamGetInfo(req)
		},
		sdk.MethodTeamList: func(_ context.Context, _ *Extension, _ sdk.HostCallRequest) sdk.HostCallResponse {
			return h.handleTeamList()
		},
		sdk.MethodShowPicker: func(_ context.Context, _ *Extension, req sdk.HostCallRequest) sdk.HostCallResponse {
			return h.handleShowPicker(req)
		},
		sdk.MethodAgentResetHistory: func(_ context.Context, _ *Extension, req sdk.HostCallRequest) sdk.HostCallResponse {
			return h.handleAgentResetHistory(req)
		},
		sdk.MethodMCPSpawn: func(_ context.Context, ext *Extension, req sdk.HostCallRequest) sdk.HostCallResponse {
			return h.handleMCPSpawn(ext, req)
		},
		sdk.MethodMCPClose: func(_ context.Context, _ *Extension, req sdk.HostCallRequest) sdk.HostCallResponse {
			return h.handleMCPClose(req)
		},
		sdk.MethodMCPSend: func(_ context.Context, _ *Extension, req sdk.HostCallRequest) sdk.HostCallResponse {
			return h.handleMCPSend(req)
		},
		sdk.MethodMCPRead: func(_ context.Context, _ *Extension, req sdk.HostCallRequest) sdk.HostCallResponse {
			return h.handleMCPRead(req)
		},
		sdk.MethodGetOS: func(_ context.Context, _ *Extension, _ sdk.HostCallRequest) sdk.HostCallResponse {
			return h.handleGetOS()
		},
		sdk.MethodGetStatusInfo: func(_ context.Context, _ *Extension, _ sdk.HostCallRequest) sdk.HostCallResponse {
			return h.handleGetStatusInfo()
		},
		sdk.MethodSetStatusLine: func(_ context.Context, _ *Extension, req sdk.HostCallRequest) sdk.HostCallResponse {
			return h.handleSetStatusLine(req)
		},
		sdk.MethodGetContextUsage: func(_ context.Context, _ *Extension, _ sdk.HostCallRequest) sdk.HostCallResponse {
			return h.handleGetContextUsage()
		},
		sdk.MethodUICreateArea: func(_ context.Context, ext *Extension, req sdk.HostCallRequest) sdk.HostCallResponse {
			return h.handleUICreateArea(ext, req)
		},
		sdk.MethodUIPatch: func(_ context.Context, ext *Extension, req sdk.HostCallRequest) sdk.HostCallResponse {
			return h.handleUIPatch(ext, req)
		},
		sdk.MethodUIRemoveArea: func(_ context.Context, ext *Extension, req sdk.HostCallRequest) sdk.HostCallResponse {
			return h.handleUIRemoveArea(ext, req)
		},
		sdk.MethodUIUpdateArea: func(_ context.Context, ext *Extension, req sdk.HostCallRequest) sdk.HostCallResponse {
			return h.handleUIUpdateArea(ext, req)
		},
	}
}

func (h *Host) handleUICreateArea(ext *Extension, req sdk.HostCallRequest) sdk.HostCallResponse {
	if ext == nil || !ext.HasPermission(sdk.PermUI) {
		return sdk.HostCallResponse{Error: "ui_create_area: permission denied: requires ui"}
	}
	if h.uiBridge() == nil {
		return sdk.HostCallResponse{Error: "ui_create_area: not supported by host"}
	}
	var params sdk.UICreateAreaParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return sdk.HostCallResponse{Error: fmt.Sprintf("ui_create_area: %v", err)}
	}
	if err := h.uiBridge().CreateArea(params.Area); err != nil {
		return sdk.HostCallResponse{Error: err.Error()}
	}
	return sdk.HostCallResponse{}
}

func (h *Host) handleUIPatch(ext *Extension, req sdk.HostCallRequest) sdk.HostCallResponse {
	if ext == nil || !ext.HasPermission(sdk.PermUI) {
		return sdk.HostCallResponse{Error: "ui_patch: permission denied: requires ui"}
	}
	if h.uiBridge() == nil {
		return sdk.HostCallResponse{Error: "ui_patch: not supported by host"}
	}
	var params sdk.UIPatchParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return sdk.HostCallResponse{Error: fmt.Sprintf("ui_patch: %v", err)}
	}
	if err := h.uiBridge().PatchUI(params); err != nil {
		return sdk.HostCallResponse{Error: err.Error()}
	}
	return sdk.HostCallResponse{}
}

func (h *Host) handleUIUpdateArea(ext *Extension, req sdk.HostCallRequest) sdk.HostCallResponse {
	if ext == nil || !ext.HasPermission(sdk.PermUI) {
		return sdk.HostCallResponse{Error: "ui_update_area: permission denied: requires ui"}
	}
	if h.uiBridge() == nil {
		return sdk.HostCallResponse{Error: "ui_update_area: not supported by host"}
	}
	var params sdk.UIUpdateAreaParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return sdk.HostCallResponse{Error: fmt.Sprintf("ui_update_area: %v", err)}
	}
	if err := h.uiBridge().UpdateArea(params); err != nil {
		return sdk.HostCallResponse{Error: err.Error()}
	}
	return sdk.HostCallResponse{}
}

func (h *Host) handleUIRemoveArea(ext *Extension, req sdk.HostCallRequest) sdk.HostCallResponse {
	if ext == nil || !ext.HasPermission(sdk.PermUI) {
		return sdk.HostCallResponse{Error: "ui_remove_area: permission denied: requires ui"}
	}
	if h.uiBridge() == nil {
		return sdk.HostCallResponse{Error: "ui_remove_area: not supported by host"}
	}
	var params struct {
		Area string `json:"area"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return sdk.HostCallResponse{Error: fmt.Sprintf("ui_remove_area: %v", err)}
	}
	h.uiBridge().RemoveArea(params.Area)
	return sdk.HostCallResponse{}
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
	if h.uiBridge() != nil {
		if err := h.uiBridge().RegisterTool(tool); err != nil {
			return sdk.HostCallResponse{Error: err.Error()}
		}
	}
	return sdk.HostCallResponse{}
}

func (h *Host) handleRegisterCommand(req sdk.HostCallRequest) sdk.HostCallResponse {
	var params struct {
		Name    string `json:"name"`
		Desc    string `json:"description"`
		Instant bool   `json:"instant"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return sdk.HostCallResponse{Error: fmt.Sprintf("register_command: %v", err)}
	}
	if h.uiBridge() != nil {
		_ = h.uiBridge().RegisterCommand(params.Name, params.Desc, params.Instant)
	}
	return sdk.HostCallResponse{}
}

func (h *Host) handleSendMessage(req sdk.HostCallRequest) sdk.HostCallResponse {
	var msg sdk.Message
	if err := json.Unmarshal(req.Params, &msg); err != nil {
		return sdk.HostCallResponse{Error: fmt.Sprintf("send_message: %v", err)}
	}
	if h.uiBridge() != nil {
		h.uiBridge().SendMessage(msg)
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
	if h.uiBridge() != nil {
		h.uiBridge().SetStatus(params.Key, params.Value)
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
	if h.uiBridge() != nil {
		h.uiBridge().Notify(params.Text)
	}
	return sdk.HostCallResponse{}
}

func (h *Host) handleModal(req sdk.HostCallRequest) sdk.HostCallResponse {
	if h.uiBridge() == nil {
		return sdk.HostCallResponse{Error: "modal: not supported by host"}
	}
	var params struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return sdk.HostCallResponse{Error: fmt.Sprintf("modal: %v", err)}
	}
	h.uiBridge().ShowModal(params.Text)
	return sdk.HostCallResponse{}
}

func (h *Host) handleAppendSystemPrompt(req sdk.HostCallRequest) sdk.HostCallResponse {
	if h.uiBridge() == nil {
		return sdk.HostCallResponse{Error: "append_system_prompt: not supported by host"}
	}
	var params struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return sdk.HostCallResponse{Error: fmt.Sprintf("append_system_prompt: %v", err)}
	}
	h.uiBridge().AppendSystemPrompt(params.Text)
	return sdk.HostCallResponse{}
}

func (h *Host) handleSetSystemPrompt(req sdk.HostCallRequest) sdk.HostCallResponse {
	if h.uiBridge() == nil {
		return sdk.HostCallResponse{Error: "set_system_prompt: not supported by host"}
	}
	var params struct {
		Prompt string `json:"prompt"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return sdk.HostCallResponse{Error: fmt.Sprintf("set_system_prompt: %v", err)}
	}
	h.uiBridge().SetSystemPrompt(params.Prompt)
	return sdk.HostCallResponse{}
}

func (h *Host) handleExec(ctx context.Context, ext *Extension, req sdk.HostCallRequest) sdk.HostCallResponse {
	if ext == nil || !ext.HasPermission(sdk.PermExec) {
		return sdk.HostCallResponse{Error: "exec: permission denied: requires exec"}
	}
	if h.capabilityProvider() == nil {
		return sdk.HostCallResponse{Error: "exec: not supported by host"}
	}
	var params struct {
		Command string `json:"command"`
		Dir     string `json:"dir"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return sdk.HostCallResponse{Error: fmt.Sprintf("exec: %v", err)}
	}
	output, err := h.capabilityProvider().Exec(ctx, params.Command, params.Dir, nil)
	if err != nil {
		result, _ := json.Marshal(map[string]string{"output": output, "error": err.Error()})
		return sdk.HostCallResponse{Result: result}
	}
	result, _ := json.Marshal(map[string]string{"output": output})
	return sdk.HostCallResponse{Result: result}
}

func (h *Host) handleGetEnv(ext *Extension, req sdk.HostCallRequest) sdk.HostCallResponse {
	if h.capabilityProvider() == nil {
		return sdk.HostCallResponse{Error: "get_env: not supported by host"}
	}
	var params struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return sdk.HostCallResponse{Error: fmt.Sprintf("get_env: %v", err)}
	}
	output, err := h.capabilityProvider().GetEnv(params.Name)
	if err != nil {
		return sdk.HostCallResponse{Error: err.Error()}
	}
	result, _ := json.Marshal(map[string]string{"value": output})
	return sdk.HostCallResponse{Result: result}
}

func (h *Host) handleReadFile(ext *Extension, req sdk.HostCallRequest) sdk.HostCallResponse {
	if ext == nil || !ext.HasPermission(sdk.PermFileRead) {
		return sdk.HostCallResponse{Error: "read_file: permission denied: requires file_read"}
	}
	if h.capabilityProvider() == nil {
		return sdk.HostCallResponse{Error: "read_file: not supported by host"}
	}
	var params struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return sdk.HostCallResponse{Error: fmt.Sprintf("read_file: %v", err)}
	}
	if params.Path == "" {
		return sdk.HostCallResponse{Error: "read_file: path is required"}
	}
	content, err := h.capabilityProvider().ReadFile(params.Path)
	if err != nil {
		return sdk.HostCallResponse{Error: err.Error()}
	}
	result, _ := json.Marshal(map[string]string{"content": content})
	return sdk.HostCallResponse{Result: result}
}

func (h *Host) handleWriteFile(ext *Extension, req sdk.HostCallRequest) sdk.HostCallResponse {
	if ext == nil || !ext.HasPermission(sdk.PermFileWrite) {
		return sdk.HostCallResponse{Error: "write_file: permission denied: requires file_write"}
	}
	if h.capabilityProvider() == nil {
		return sdk.HostCallResponse{Error: "write_file: not supported by host"}
	}
	var params struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return sdk.HostCallResponse{Error: fmt.Sprintf("write_file: %v", err)}
	}
	if params.Path == "" {
		return sdk.HostCallResponse{Error: "write_file: path is required"}
	}
	if err := h.capabilityProvider().WriteFile(params.Path, params.Content); err != nil {
		return sdk.HostCallResponse{Error: err.Error()}
	}
	result, _ := json.Marshal(map[string]string{"written": params.Path})
	return sdk.HostCallResponse{Result: result}
}

func (h *Host) handleAppendFile(ext *Extension, req sdk.HostCallRequest) sdk.HostCallResponse {
	if ext == nil || !ext.HasPermission(sdk.PermFileWrite) {
		return sdk.HostCallResponse{Error: "append_file: permission denied: requires file_write"}
	}
	if h.capabilityProvider() == nil {
		return sdk.HostCallResponse{Error: "append_file: not supported by host"}
	}
	var params struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return sdk.HostCallResponse{Error: fmt.Sprintf("append_file: %v", err)}
	}
	if params.Path == "" {
		return sdk.HostCallResponse{Error: "append_file: path is required"}
	}
	if err := h.capabilityProvider().AppendFile(params.Path, params.Content); err != nil {
		return sdk.HostCallResponse{Error: err.Error()}
	}
	result, _ := json.Marshal(map[string]string{"appended": params.Path})
	return sdk.HostCallResponse{Result: result}
}

func (h *Host) handleHTTPPost(ext *Extension, req sdk.HostCallRequest) sdk.HostCallResponse {
	if ext == nil || !ext.HasPermission(sdk.PermNetworkWrite) {
		return sdk.HostCallResponse{Error: "http_post: permission denied: requires network_write"}
	}
	if h.capabilityProvider() == nil {
		return sdk.HostCallResponse{Error: "http_post: not supported by host"}
	}
	var params struct {
		Headers map[string]string `json:"headers"`
		URL     string            `json:"url"`
		Body    []byte            `json:"body"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return sdk.HostCallResponse{Error: fmt.Sprintf("http_post: %v", err)}
	}
	if params.URL == "" {
		return sdk.HostCallResponse{Error: "http_post: url is required"}
	}
	statusCode, respBody, err := h.capabilityProvider().HTTPPost(params.URL, params.Headers, params.Body)
	if err != nil {
		return sdk.HostCallResponse{Error: err.Error()}
	}
	result, _ := json.Marshal(map[string]any{hostResultStatus: statusCode, "body": string(respBody)})
	return sdk.HostCallResponse{Result: result}
}

func (h *Host) handleConfigRead(ext *Extension) sdk.HostCallResponse {
	if h.capabilityProvider() == nil {
		return sdk.HostCallResponse{Error: "config_read: not supported by host"}
	}
	group := ""
	if ext != nil {
		group = ext.name
	}
	data, err := h.capabilityProvider().ConfigRead(group)
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

	if h.uiBridge() != nil {
		h.uiBridge().ToolResult(params.ToolCallID, params.Result, params.IsError)
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
	if h.agentBridge() == nil {
		return sdk.HostCallResponse{Error: "agent_spawn: not supported by host"}
	}
	var params struct {
		ID             string `json:"id"`
		Name           string `json:"name"`
		SystemPrompt   string `json:"system_prompt"`
		ModelName      string `json:"model_name"`
		InitialPrompt  string `json:"initial_prompt"`
		CallerID       string `json:"caller_id"`
		ThinkingBudget int    `json:"thinking_budget"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return sdk.HostCallResponse{Error: fmt.Sprintf("agent_spawn: %v", err)}
	}
	if err := h.agentBridge().Spawn(context.Background(), SpawnRequest{
		ID:             params.ID,
		Name:           params.Name,
		SystemPrompt:   params.SystemPrompt,
		ModelName:      params.ModelName,
		InitialPrompt:  params.InitialPrompt,
		ThinkingBudget: params.ThinkingBudget,
		CallerID:       params.CallerID,
	}); err != nil {
		return sdk.HostCallResponse{Error: err.Error()}
	}
	result, _ := json.Marshal(map[string]string{"agent_id": params.ID, "status": "created"})
	return sdk.HostCallResponse{Result: result}
}

func (h *Host) handleAgentClose(req sdk.HostCallRequest) sdk.HostCallResponse {
	if h.agentBridge() == nil {
		return sdk.HostCallResponse{Error: "agent_close: not supported by host"}
	}
	var params struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return sdk.HostCallResponse{Error: fmt.Sprintf("agent_close: %v", err)}
	}
	if err := h.agentBridge().Close(params.ID); err != nil {
		return sdk.HostCallResponse{Error: err.Error()}
	}
	return sdk.HostCallResponse{}
}

func (h *Host) handleAgentSendMessage(req sdk.HostCallRequest) sdk.HostCallResponse {
	if h.agentBridge() == nil {
		return sdk.HostCallResponse{Error: "agent_send_message: not supported by host"}
	}
	var params struct {
		ID      string `json:"id"`
		Message string `json:"message"`
		Type    string `json:"type,omitempty"` // optional: "system", "steering", or "" (normal)
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return sdk.HostCallResponse{Error: fmt.Sprintf("agent_send_message: %v", err)}
	}
	switch params.Type {
	case "", "normal", "steering", "system":
		// valid message types
	default:
		return sdk.HostCallResponse{Error: "agent_send_message: unknown message type: " + params.Type}
	}
	msg := sdk.Message{
		Role:    sdk.RoleUser,
		Content: params.Message,
		Type:    sdk.MessageType(params.Type),
	}
	if err := h.agentBridge().SendMessage(params.ID, msg); err != nil {
		return sdk.HostCallResponse{Error: err.Error()}
	}
	return sdk.HostCallResponse{}
}

func (h *Host) handleAgentDeliver(req sdk.HostCallRequest) sdk.HostCallResponse {
	if h.agentBridge() == nil {
		return sdk.HostCallResponse{Error: "agent_deliver: not supported by host"}
	}
	var params struct {
		ID      string `json:"id"`
		Message string `json:"message"`
		Type    string `json:"type,omitempty"` // optional: "system", "steering", or "" (normal)
		Wake    *bool  `json:"wake,omitempty"` // optional: defaults to true
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return sdk.HostCallResponse{Error: fmt.Sprintf("agent_deliver: %v", err)}
	}
	switch params.Type {
	case "", "normal", "steering", "system":
		// valid message types
	default:
		return sdk.HostCallResponse{Error: "agent_deliver: unknown message type: " + params.Type}
	}
	wake := true
	if params.Wake != nil {
		wake = *params.Wake
	}
	msg := sdk.Message{
		Role:    sdk.RoleUser,
		Content: params.Message,
		Type:    sdk.MessageType(params.Type),
	}
	if err := h.agentBridge().Deliver(params.ID, msg, wake); err != nil {
		return sdk.HostCallResponse{Error: err.Error()}
	}
	return sdk.HostCallResponse{}
}

func (h *Host) handleAgentRun(req sdk.HostCallRequest) sdk.HostCallResponse {
	if h.agentBridge() == nil {
		return sdk.HostCallResponse{Error: "agent_run: not supported by host"}
	}
	var params struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return sdk.HostCallResponse{Error: fmt.Sprintf("agent_run: %v", err)}
	}
	if err := h.agentBridge().Run(params.ID); err != nil {
		return sdk.HostCallResponse{Error: err.Error()}
	}
	return sdk.HostCallResponse{}
}

func (h *Host) handleAgentList() sdk.HostCallResponse {
	if h.agentBridge() == nil {
		return sdk.HostCallResponse{Error: "agent_list: not supported by host"}
	}
	agents, err := h.agentBridge().List()
	if err != nil {
		return sdk.HostCallResponse{Error: fmt.Sprintf("agent_list: %v", err)}
	}
	result, _ := json.Marshal(map[string][]AgentInfo{"agents": agents})
	return sdk.HostCallResponse{Result: result}
}

func (h *Host) handleAgentTokenCount() sdk.HostCallResponse {
	if h.agentBridge() == nil {
		return sdk.HostCallResponse{Error: "agent_token_count: not supported by host"}
	}
	count := h.agentBridge().TokenCount()
	result, _ := json.Marshal(map[string]int64{"count": count})
	return sdk.HostCallResponse{Result: result}
}

func (h *Host) handleTeamCreate(req sdk.HostCallRequest) sdk.HostCallResponse {
	if h.teamBridge() == nil {
		return sdk.HostCallResponse{Error: "team_create: not supported by host"}
	}
	var params struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return sdk.HostCallResponse{Error: fmt.Sprintf("team_create: %v", err)}
	}
	if err := h.teamBridge().Create(params.ID, params.Name); err != nil {
		return sdk.HostCallResponse{Error: err.Error()}
	}
	result, _ := json.Marshal(map[string]string{"team_id": params.ID, "status": "created"})
	return sdk.HostCallResponse{Result: result}
}

func (h *Host) handleTeamClose(req sdk.HostCallRequest) sdk.HostCallResponse {
	if h.teamBridge() == nil {
		return sdk.HostCallResponse{Error: "team_close: not supported by host"}
	}
	var params struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return sdk.HostCallResponse{Error: fmt.Sprintf("team_close: %v", err)}
	}
	if err := h.teamBridge().Close(context.Background(), params.ID); err != nil {
		return sdk.HostCallResponse{Error: err.Error()}
	}
	return sdk.HostCallResponse{}
}

func (h *Host) handleTeamAddMember(req sdk.HostCallRequest) sdk.HostCallResponse {
	if h.teamBridge() == nil {
		return sdk.HostCallResponse{Error: "team_add_member: not supported by host"}
	}
	var params struct {
		TeamID  string `json:"team_id"`
		AgentID string `json:"agent_id"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return sdk.HostCallResponse{Error: fmt.Sprintf("team_add_member: %v", err)}
	}
	if err := h.teamBridge().AddMember(params.TeamID, params.AgentID); err != nil {
		return sdk.HostCallResponse{Error: err.Error()}
	}
	return sdk.HostCallResponse{}
}

func (h *Host) handleTeamRemoveMember(req sdk.HostCallRequest) sdk.HostCallResponse {
	if h.teamBridge() == nil {
		return sdk.HostCallResponse{Error: "team_remove_member: not supported by host"}
	}
	var params struct {
		TeamID  string `json:"team_id"`
		AgentID string `json:"agent_id"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return sdk.HostCallResponse{Error: fmt.Sprintf("team_remove_member: %v", err)}
	}
	if err := h.teamBridge().RemoveMember(params.TeamID, params.AgentID); err != nil {
		return sdk.HostCallResponse{Error: err.Error()}
	}
	return sdk.HostCallResponse{}
}

func (h *Host) handleTeamGetInfo(req sdk.HostCallRequest) sdk.HostCallResponse {
	if h.teamBridge() == nil {
		return sdk.HostCallResponse{Error: "team_get_info: not supported by host"}
	}
	var params struct {
		TeamID string `json:"team_id"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return sdk.HostCallResponse{Error: fmt.Sprintf("team_get_info: %v", err)}
	}
	if params.TeamID == "" {
		return sdk.HostCallResponse{Error: "team_get_info: team_id is required"}
	}
	members, err := h.teamBridge().GetMembers(params.TeamID)
	if err != nil {
		return sdk.HostCallResponse{Error: err.Error()}
	}
	result, _ := json.Marshal(map[string]any{"team_id": params.TeamID, "members": members})
	return sdk.HostCallResponse{Result: result}
}

func (h *Host) handleTeamList() sdk.HostCallResponse {
	if h.teamBridge() == nil {
		return sdk.HostCallResponse{Error: "team_list: not supported by host"}
	}
	teams, err := h.teamBridge().List()
	if err != nil {
		return sdk.HostCallResponse{Error: fmt.Sprintf("team_list: %v", err)}
	}
	result, _ := json.Marshal(map[string][]string{"teams": teams})
	return sdk.HostCallResponse{Result: result}
}

func (h *Host) handleShowPicker(req sdk.HostCallRequest) sdk.HostCallResponse {
	if h.uiBridge() == nil {
		return sdk.HostCallResponse{Error: "show_picker: not supported by host"}
	}
	var params sdk.ShowPickerParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return sdk.HostCallResponse{Error: fmt.Sprintf("show_picker: %v", err)}
	}
	h.uiBridge().ShowPicker(params.Title, params.Items, params.Callback)
	return sdk.HostCallResponse{}
}

func (h *Host) handleAgentResetHistory(req sdk.HostCallRequest) sdk.HostCallResponse {
	if h.uiBridge() == nil {
		return sdk.HostCallResponse{Error: "agent_reset_history: not supported by host"}
	}
	var params sdk.AgentResetHistoryParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return sdk.HostCallResponse{Error: fmt.Sprintf("agent_reset_history: %v", err)}
	}
	if err := h.uiBridge().ResetHistory(params.Messages); err != nil {
		return sdk.HostCallResponse{Error: err.Error()}
	}
	return sdk.HostCallResponse{}
}

func (h *Host) handleMCPSpawn(ext *Extension, req sdk.HostCallRequest) sdk.HostCallResponse {
	if ext == nil || !ext.HasPermission(sdk.PermExec) {
		return sdk.HostCallResponse{Error: "mcp_spawn: permission denied: requires exec"}
	}
	if h.mcpBridge() == nil {
		return sdk.HostCallResponse{Error: "mcp_spawn: not supported by host"}
	}
	var params struct {
		Env     map[string]string `json:"env"`
		ID      string            `json:"id"`
		Command string            `json:"command"`
		Args    []string          `json:"args"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return sdk.HostCallResponse{Error: fmt.Sprintf("mcp_spawn: %v", err)}
	}
	if err := h.mcpBridge().Spawn(params.ID, params.Command, params.Args, params.Env); err != nil {
		return sdk.HostCallResponse{Error: err.Error()}
	}
	result, _ := json.Marshal(map[string]string{"id": params.ID, "status": "spawned"})
	return sdk.HostCallResponse{Result: result}
}

func (h *Host) handleMCPClose(req sdk.HostCallRequest) sdk.HostCallResponse {
	if h.mcpBridge() == nil {
		return sdk.HostCallResponse{Error: "mcp_close: not supported by host"}
	}
	var params struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return sdk.HostCallResponse{Error: fmt.Sprintf("mcp_close: %v", err)}
	}
	if err := h.mcpBridge().Close(params.ID); err != nil {
		return sdk.HostCallResponse{Error: err.Error()}
	}
	return sdk.HostCallResponse{}
}

func (h *Host) handleMCPSend(req sdk.HostCallRequest) sdk.HostCallResponse {
	if h.mcpBridge() == nil {
		return sdk.HostCallResponse{Error: "mcp_send: not supported by host"}
	}
	var params struct {
		ID   string          `json:"id"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return sdk.HostCallResponse{Error: fmt.Sprintf("mcp_send: %v", err)}
	}
	if err := h.mcpBridge().Send(params.ID, []byte(params.Data)); err != nil {
		return sdk.HostCallResponse{Error: err.Error()}
	}
	return sdk.HostCallResponse{}
}

func (h *Host) handleMCPRead(req sdk.HostCallRequest) sdk.HostCallResponse {
	if h.mcpBridge() == nil {
		return sdk.HostCallResponse{Error: "mcp_read: not supported by host"}
	}
	var params struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return sdk.HostCallResponse{Error: fmt.Sprintf("mcp_read: %v", err)}
	}
	data, err := h.mcpBridge().Read(params.ID)
	if err != nil {
		return sdk.HostCallResponse{Error: err.Error()}
	}
	result, _ := json.Marshal(map[string]json.RawMessage{"data": data})
	return sdk.HostCallResponse{Result: result}
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

	// Load optional manifest for permission declarations and priority.
	manifest := loadManifest(path, h.logger)
	var perms []sdk.Permission
	if manifest != nil {
		perms = manifest.Permissions
	}

	return h.loadExtension(ctx, path, data, false, perms, manifest)
}

// LoadBytes instantiates a WASM module from in-memory bytes.
// Use this for embedded built-in extensions.
// When trusted is true the extension is granted all permissions without a
// manifest; when trusted is false the caller must supply the permissions slice.
func (h *Host) LoadBytes(ctx context.Context, name string, data []byte, trusted bool) error {
	return h.loadExtension(ctx, name, data, trusted, nil)
}

// loadExtension is the common implementation for Load and LoadBytes.
func (h *Host) loadExtension(
	ctx context.Context,
	name string,
	data []byte,
	trusted bool,
	perms []sdk.Permission,
	manifest ...*sdk.ExtensionManifest,
) error {
	modName := moduleNameFromPath(name)

	cfg := wazero.NewModuleConfig().
		WithName(modName).
		WithStartFunctions().
		WithRandSource(crand.Reader).
		WithFSConfig(wazero.NewFSConfig().WithDirMount("/", "/"))
	// Pass all host environment variables to WASM modules.
	for _, kv := range os.Environ() {
		if idx := strings.IndexByte(kv, '='); idx > 0 {
			cfg = cfg.WithEnv(kv[:idx], kv[idx+1:])
		}
	}
	mod, err := h.runtime.InstantiateWithConfig(ctx, data, cfg)
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

	priority := 100 // user extension default
	if trusted {
		priority = 0 // built-ins always run first
	}
	if len(manifest) > 0 && manifest[0] != nil && manifest[0].Priority != nil {
		priority = *manifest[0].Priority
	}
	ext := &Extension{
		name:          extensionDisplayName(name),
		module:        mod,
		subscriptions: make(map[sdk.EventType]bool),
		store:         NewStore(),
		trusted:       trusted,
		permissions:   permMap,
		Priority:      priority,
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

// loadManifest reads a companion JSON manifest at "<basename>.json" alongside
// the WASM file. Missing or invalid manifest files are silently ignored.
func loadManifest(wasmPath string, logger *slog.Logger) *sdk.ExtensionManifest {
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
	return &manifest
}

// RegisterNativeTool registers a Go-native tool handler that is invoked directly
// in ExecuteTool without WASM dispatch. The tool schema is also registered so the
// LLM sees it alongside WASM-backed tools.
//
// fn receives the raw JSON input and returns (result string, isError bool).
// Native tools still fire EventAfterToolCall so WASM extensions can observe results.
func (h *Host) RegisterNativeTool(tool sdk.Tool, fn func(ctx context.Context, input json.RawMessage) (string, bool)) {
	h.nativeToolsMu.Lock()
	h.nativeTools[tool.Name] = fn
	h.nativeToolsMu.Unlock()

	h.mu.Lock()
	h.registeredTools[tool.Name] = tool
	h.mu.Unlock()

	if h.uiBridge() != nil {
		_ = h.uiBridge().RegisterTool(tool)
	}
}

// RegisterToolSchema adds a tool to the registered tools map so the LLM sees it,
// without setting up any Go or WASM dispatch handler. Use this for components
// (e.g. the MCP bridge) that handle dispatch via an alternative mechanism such
// as the EventBus.
func (h *Host) RegisterToolSchema(tool sdk.Tool) {
	h.mu.Lock()
	if _, exists := h.registeredTools[tool.Name]; !exists {
		h.registeredTools[tool.Name] = tool
	}
	h.mu.Unlock()
	if h.uiBridge() != nil {
		_ = h.uiBridge().RegisterTool(tool)
	}
}

// ExecuteTool dispatches a tool call to subscribed extensions and blocks until
// an extension returns the result via the tool_result host_call method.
// It dispatches EventBeforeToolCall so extensions may intercept the call.
// The call is cancelled and an error is returned if ctx is cancelled.
func (h *Host) ExecuteTool(
	ctx context.Context,
	agentID, toolCallID, toolName string,
	input json.RawMessage,
) (toolResult, error) {
	// Check native handler first — no WASM dispatch needed.
	h.nativeToolsMu.RLock()
	nativeFn, isNative := h.nativeTools[toolName]
	h.nativeToolsMu.RUnlock()

	if isNative {
		// Fire before_tool_call as a transform chain so interceptors can rewrite
		// the input (e.g. a security layer) or block the call with a reason.
		// Native tools have no WASM implementer, so no subscriber calls tool_result
		// during the chain — the host executes nativeFn with the final input.
		finalInput, blocked, reason := h.runBeforeToolCall(ctx, agentID, toolCallID, toolName, input)
		if blocked {
			return toolResult{Result: blockReason(reason), IsError: true}, nil
		}

		result, isError := nativeFn(ctx, finalInput)

		// Fire after_tool_call as a transform chain so interceptors can rewrite or
		// redact the tool's output before the model sees it (the symmetric partner
		// of the before_tool_call input rewrite).
		result, isError = h.runAfterToolCall(ctx, agentID, toolCallID, toolName, result, isError)
		tr := toolResult{Result: result, IsError: isError}

		if h.uiBridge() != nil {
			h.uiBridge().AfterToolCall(agentID, toolCallID, toolName, result, isError)
		}
		return tr, nil
	}

	// Register the pending result channel BEFORE running the chain: the
	// implementing extension is itself a before_tool_call subscriber and calls
	// tool_result synchronously within _on_event during the chain. Registering
	// first ensures that result is delivered to ch and never dropped.
	ch := make(chan toolResult, 1)
	h.pendingMu.Lock()
	h.pendingTools[toolCallID] = ch
	h.pendingMu.Unlock()

	// Run the before_tool_call transform chain ONCE. Interceptors (lower
	// priority) rewrite/block the input; the implementing extension (higher
	// priority) sees the threaded final input and calls tool_result. A block
	// short-circuits before the implementer runs, so tool_result is never called.
	_, blocked, reason := h.runBeforeToolCall(ctx, agentID, toolCallID, toolName, input)
	if blocked {
		h.pendingMu.Lock()
		delete(h.pendingTools, toolCallID)
		h.pendingMu.Unlock()
		return toolResult{Result: blockReason(reason), IsError: true}, nil
	}

	// Block until the extension returns the result or the context is cancelled.
	select {
	case result := <-ch:
		// Run the after_tool_call transform chain so interceptors can rewrite or
		// redact the tool's output before it reaches the model.
		result.Result, result.IsError = h.runAfterToolCall(ctx, agentID, toolCallID, toolName, result.Result, result.IsError)

		// Notify the UI bridge so the TUI can update the tool call display.
		if ui := h.uiBridge(); ui != nil {
			ui.AfterToolCall(agentID, toolCallID, toolName, result.Result, result.IsError)
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

// runBeforeToolCall runs the before_tool_call interceptor chain for a tool call
// and returns the final (possibly rewritten) tool input, whether the call was
// blocked, and the block reason. Interceptors may rewrite Input or block the
// call (see DispatchEventChain). A malformed transformed payload is tolerated:
// the original input is kept and a warning is logged.
func (h *Host) runBeforeToolCall(
	ctx context.Context,
	agentID, toolCallID, toolName string,
	input json.RawMessage,
) (finalInput json.RawMessage, blocked bool, reason string) {
	payload, _ := json.Marshal(sdk.BeforeToolCallPayload{
		AgentID:    agentID,
		ToolCallID: toolCallID,
		ToolName:   toolName,
		Input:      input,
	})
	evt := sdk.Event{Type: sdk.EventBeforeToolCall, Payload: payload}
	finalEvt, blocked, reason, err := h.DispatchEventChain(ctx, evt)
	if err != nil {
		h.logger.Warn("extension: before_tool_call chain marshal error", "tool", toolName, "err", err)
		return input, false, ""
	}
	if blocked {
		return input, true, reason
	}
	// Extract the (possibly transformed) Input from the final payload. A
	// malformed transform keeps the original input.
	var finalPayload sdk.BeforeToolCallPayload
	if uerr := json.Unmarshal(finalEvt.Payload, &finalPayload); uerr != nil {
		h.logger.Warn("extension: before_tool_call transform produced invalid payload; keeping original input",
			"tool", toolName, "err", uerr)
		return input, false, ""
	}
	if len(finalPayload.Input) == 0 {
		return input, false, ""
	}
	return finalPayload.Input, false, ""
}

// runAfterToolCall runs the after_tool_call interceptor chain on a tool's result
// and returns the final (possibly rewritten/redacted) result and error flag.
// Interceptors may transform the result via EventResponse.Payload (an
// AfterToolCallPayload). A blocking response replaces the result with the block
// reason and forces IsError=true. A malformed transform is tolerated (original
// result kept, warning logged). This is the output-side counterpart of
// runBeforeToolCall.
func (h *Host) runAfterToolCall(
	ctx context.Context,
	agentID, toolCallID, toolName, result string,
	isError bool,
) (finalResult string, finalIsError bool) {
	payload, _ := json.Marshal(sdk.AfterToolCallPayload{
		AgentID:    agentID,
		ToolCallID: toolCallID,
		ToolName:   toolName,
		Result:     result,
		IsError:    isError,
	})
	evt := sdk.Event{Type: sdk.EventAfterToolCall, Payload: payload}
	finalEvt, blocked, reason, err := h.DispatchEventChain(ctx, evt)
	if err != nil {
		h.logger.Warn("extension: after_tool_call chain marshal error", "tool", toolName, "err", err)
		return result, isError
	}
	if blocked {
		// A block on the output side redacts the result wholesale with the reason.
		return blockReason(reason), true
	}
	var finalPayload sdk.AfterToolCallPayload
	if uerr := json.Unmarshal(finalEvt.Payload, &finalPayload); uerr != nil {
		h.logger.Warn("extension: after_tool_call transform produced invalid payload; keeping original result",
			"tool", toolName, "err", uerr)
		return result, isError
	}
	return finalPayload.Result, finalPayload.IsError
}

// blockReason formats the user-facing tool result text for a blocked tool call.
func blockReason(reason string) string {
	if reason == "" {
		return "tool call blocked by extension"
	}
	return "tool call blocked: " + reason
}

// SendToolResult delivers a tool result for the given toolCallID.
// This is used by native Go components (like the MCP bridge) to return tool results
// without going through the WASM host_call mechanism.
func (h *Host) SendToolResult(toolCallID, result string, isError bool) {
	h.pendingMu.Lock()
	ch, hasPending := h.pendingTools[toolCallID]
	if hasPending {
		delete(h.pendingTools, toolCallID)
	}
	h.pendingMu.Unlock()

	if hasPending {
		ch <- toolResult{Result: result, IsError: isError}
	}

	if h.uiBridge() != nil {
		h.uiBridge().ToolResult(toolCallID, result, isError)
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

// HasSubscribers reports whether any loaded extension is subscribed to evt.
// Used to avoid building/dispatching event payloads when nothing will consume
// them (e.g. the log dispatcher batches only once a log sink exists).
func (h *Host) HasSubscribers(evt sdk.EventType) bool {
	h.mu.RLock()
	exts := make([]*Extension, len(h.extensions))
	copy(exts, h.extensions)
	h.mu.RUnlock()
	for _, ext := range exts {
		ext.subMu.RLock()
		subscribed := ext.subscriptions[evt]
		ext.subMu.RUnlock()
		if subscribed {
			return true
		}
	}
	return false
}

func (h *Host) DispatchEvent(ctx context.Context, evt sdk.Event) ([]sdk.EventResponse, error) {
	// Publish to the event bus (fire-and-forget, no-op if no subscribers).
	h.Bus.Publish(ctx, evt)

	evtJSON, err := json.Marshal(evt)
	if err != nil {
		return nil, fmt.Errorf("marshal event: %w", err)
	}

	h.mu.RLock()
	exts := make([]*Extension, len(h.extensions))
	copy(exts, h.extensions)
	h.mu.RUnlock()

	// Sort by priority ascending (lower = runs first), alphabetical within same priority.
	sort.Slice(exts, func(i, j int) bool {
		if exts[i].Priority != exts[j].Priority {
			return exts[i].Priority < exts[j].Priority
		}
		return exts[i].name < exts[j].name
	})

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

// DispatchEventChain threads evt's payload through subscribed extensions in
// priority order (lower priority first, name asc tiebreak — same order as
// DispatchEvent). Each extension's _on_event may:
//   - observe: return nil/zero → payload unchanged, chain continues.
//   - transform: return EventResponse.Payload → that payload replaces the
//     event's payload for subsequent interceptors and the final result.
//   - block: return Block or Cancel → the chain stops and blocked is true,
//     with reason taken from EventResponse.Error.
//
// It returns the final (possibly transformed) event, whether the chain was
// blocked, the block reason, and any fatal marshal error. A WASM trap on one
// extension is logged and skipped (that extension is treated as observe-only).
//
// A transformed Payload that is empty is treated as "no change". The host does
// not validate the payload shape here — the caller at each seam unmarshals the
// final payload into the expected type and is responsible for tolerating a
// malformed transform (keep prior payload, log).
func (h *Host) DispatchEventChain(ctx context.Context, evt sdk.Event) (sdk.Event, bool, string, error) {
	h.mu.RLock()
	exts := make([]*Extension, len(h.extensions))
	copy(exts, h.extensions)
	h.mu.RUnlock()

	sort.Slice(exts, func(i, j int) bool {
		if exts[i].Priority != exts[j].Priority {
			return exts[i].Priority < exts[j].Priority
		}
		return exts[i].name < exts[j].name
	})

	current := evt
	for _, ext := range exts {
		ext.subMu.RLock()
		subscribed := ext.subscriptions[current.Type]
		ext.subMu.RUnlock()
		if !subscribed {
			continue
		}

		evtJSON, err := json.Marshal(current)
		if err != nil {
			return current, false, "", fmt.Errorf("marshal event: %w", err)
		}

		resp, dispErr := h.dispatchToExtension(ctx, ext, evtJSON)
		if dispErr != nil {
			h.logger.Warn("extension: chain dispatch error", "extension", ext.name, "err", dispErr)
			continue
		}

		next, blocked, reason := applyInterceptorResponse(current, resp, ext.name)
		if blocked {
			return current, true, reason, nil
		}
		current = next
	}
	return current, false, "", nil
}

// applyInterceptorResponse applies one interceptor's response to the running
// event in a chain. It is the pure decision step of DispatchEventChain:
//   - Block or Cancel → (current, true, reason): the chain stops. reason falls
//     back to a default naming the extension when Error is empty.
//   - non-empty Payload → (event with the new payload, false, ""): the transform
//     is threaded to the next interceptor.
//   - otherwise (observe) → (current, false, ""): unchanged.
func applyInterceptorResponse(current sdk.Event, resp sdk.EventResponse, extName string) (sdk.Event, bool, string) {
	if resp.Block || resp.Cancel {
		reason := resp.Error
		if reason == "" {
			reason = "blocked by extension " + extName
		}
		return current, true, reason
	}
	if len(resp.Payload) > 0 {
		return sdk.Event{Type: current.Type, Payload: resp.Payload}, false, ""
	}
	return current, false, ""
}

// dispatchToExtension calls _on_event on a single extension.
// callMu serializes concurrent invocations — WASM linear memory is shared
// within the module instance, so parallel calls race on global SDK state.
func (h *Host) dispatchToExtension(ctx context.Context, ext *Extension, evtJSON []byte) (sdk.EventResponse, error) {
	ext.callMu.Lock()
	defer ext.callMu.Unlock()

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
	// Snapshot native tool names before acquiring h.mu to avoid lock-order
	// inversion with RegisterNativeTool (which acquires nativeToolsMu then h.mu).
	h.nativeToolsMu.RLock()
	nativeNames := make(map[string]struct{}, len(h.nativeTools))
	for name := range h.nativeTools {
		nativeNames[name] = struct{}{}
	}
	h.nativeToolsMu.RUnlock()

	h.mu.Lock()
	old := h.extensions
	h.extensions = nil
	// Preserve native tools — they are registered once at startup and must
	// survive reloads. Only clear WASM-owned tool registrations.
	preserved := make(map[string]sdk.Tool)
	for name, tool := range h.registeredTools {
		if _, isNative := nativeNames[name]; isNative {
			preserved[name] = tool
		}
	}
	h.registeredTools = preserved
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

// moduleNameFromPath returns the full path as the wazero module name.
// This guarantees uniqueness and avoids conflicts with host modules (e.g. "env").
func moduleNameFromPath(path string) string {
	return path
}

// extensionDisplayName returns a short human-readable name for logging:
// the base filename without the .wasm suffix.
func extensionDisplayName(path string) string {
	return strings.TrimSuffix(filepath.Base(path), ".wasm")
}

func (h *Host) handleGetOS() sdk.HostCallResponse {
	result, _ := json.Marshal(map[string]string{
		"os":   runtime.GOOS,
		"arch": runtime.GOARCH,
	})
	return sdk.HostCallResponse{Result: result}
}

func (h *Host) handleGetStatusInfo() sdk.HostCallResponse {
	if h.uiBridge() == nil {
		// Return a minimal empty response if the UI bridge hasn't been set yet.
		result, _ := json.Marshal(sdk.StatusInfo{Statuses: map[string]string{}})
		return sdk.HostCallResponse{Result: result}
	}
	info := h.uiBridge().GetStatusInfo()
	result, _ := json.Marshal(info)
	return sdk.HostCallResponse{Result: result}
}

func (h *Host) handleSetStatusLine(req sdk.HostCallRequest) sdk.HostCallResponse {
	var params struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return sdk.HostCallResponse{Error: fmt.Sprintf("set_status_line: %v", err)}
	}
	if h.uiBridge() != nil {
		h.uiBridge().SetStatus("_override", params.Text)
	}
	return sdk.HostCallResponse{}
}

// handleGetContextUsage returns the current context window usage for the main agent.
// Returns a zero-valued ContextUsage when no agent bridge is configured, consistent
// with how handleGetStatusInfo handles a missing UI bridge.
// No permission check required — this is read-only observability data.
func (h *Host) handleGetContextUsage() sdk.HostCallResponse {
	var cu sdk.ContextUsage
	if h.agentBridge() != nil {
		cu = h.agentBridge().MainAgentContextUsage()
	}
	result, _ := json.Marshal(cu)
	return sdk.HostCallResponse{Result: result}
}
