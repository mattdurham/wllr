package extension

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"context"
	"encoding/json"

	"github.com/mattdurham/wllr/modules/sdk"
)

// SpawnRequest carries the parameters for spawning a sub-agent.
// All fields have safe zero values: ModelName="" falls back to the pool's default
// model; ThinkingBudget=0 disables extended thinking; InitialPrompt="" skips the
// first turn; SystemPrompt="" uses only the agent-identity suffix injected by Spawner.
type SpawnRequest struct {
	ID           string
	Name         string
	SystemPrompt string
	// ModelName selects the language model. Empty string falls back to the pool's
	// default model name.
	ModelName     string
	InitialPrompt string
	// CallerID is the agent ID that issued the create_agent call (i.e. the parent agent).
	// Empty string for agents spawned directly by the host or in tests.
	// Passed through to SpawnOpts.CreatorID so the Agent records its parent.
	CallerID string
	// ThinkingBudget enables extended thinking with the given token budget.
	// Zero means disabled. Only supported on Anthropic models.
	ThinkingBudget int
}

// AgentBridge is the interface extensions call to manage agents.
// Set once on Host at startup via Host.SetAgentBridge; replaces the
// OnAgentSpawn, OnAgentClose, OnAgentSendMessage, OnAgentRun,
// OnAgentList, OnAgentTokenCount callback fields.
//
// If SetAgentBridge is never called, the host returns "not supported by host"
// for all agent operations. Install the earlyAgentBridge stub (see harness/bridges.go)
// before loading extensions to return a descriptive error instead.
//
// All methods are safe to call concurrently from multiple goroutines.
// Implementations must be goroutine-safe; they are called from concurrent WASM dispatch.
type AgentBridge interface {
	Spawn(ctx context.Context, req SpawnRequest) error
	Close(id string) error
	SendMessage(id string, msg sdk.Message) error
	// Deliver appends msg to the agent's inbox and, when wake is true, ensures
	// the agent processes it (starts a drain turn if idle; relies on
	// drain-until-empty if already running). This is the atomic
	// deliver-and-process primitive that replaces a SendMessage+Run pair.
	Deliver(id string, msg sdk.Message, wake bool) error
	Run(id string) error
	List() ([]AgentInfo, error)
	TokenCount() int64
	SetHistory(id string, messages []sdk.Message) error
	// MainAgentContextUsage returns the current context window usage for the main agent.
	// Returns a zero-valued ContextUsage before the first turn completes or when no
	// main agent is registered.
	MainAgentContextUsage() sdk.ContextUsage
	// SnapshotInbox returns a copy of the agent's inbox without draining.
	SnapshotInbox(id string) ([]sdk.Message, error)
	// DeleteFromInbox removes messages from the agent's inbox.
	// At least one of byIndex or byMessageID must be provided.
	DeleteFromInbox(id string, byIndex int, byMessageID string) (int, error)
	// EditInboxMessage updates a message in the agent's inbox.
	// At least one of byIndex or byMessageID must be provided.
	// Content must be non-empty (Anthropic invariant).
	EditInboxMessage(id string, byIndex int, byMessageID string, newContent string) error
}

// TeamBridge is the interface extensions call to manage teams.
// Set once on Host at startup via Host.SetTeamBridge; replaces the
// OnTeamCreate, OnTeamClose, OnTeamAddMember, OnTeamRemoveMember,
// OnTeamGetInfo, OnTeamList callback fields.
//
// If SetTeamBridge is never called, the host returns "not supported by host"
// for all team operations.
//
// All methods are safe to call concurrently from multiple goroutines.
// Implementations must be goroutine-safe; they are called from concurrent WASM dispatch.
type TeamBridge interface {
	Create(id, name string) error
	Close(ctx context.Context, id string) error
	AddMember(teamID, agentID string) error
	RemoveMember(teamID, agentID string) error
	GetMembers(teamID string) ([]string, error)
	List() ([]string, error)
}

// CapabilityProvider exposes OS-level capabilities to WASM extensions.
// Set once on Host at startup via Host.SetCapabilities; replaces the
// OnExec, OnGetEnv, OnReadFile, OnWriteFile, OnHTTPPost, OnConfigRead
// callback fields.
//
// If SetCapabilities is never called, the host returns "not supported by host"
// for all capability operations.
//
// All methods are safe to call concurrently from multiple goroutines.
// Implementations must be goroutine-safe; they are called from concurrent WASM dispatch.
type CapabilityProvider interface {
	Exec(ctx context.Context, command, dir string, onLine func(string)) (string, error)
	GetEnv(name string) (string, error)
	ReadFile(path string) (string, error)
	WriteFile(path, content string) error
	// AppendFile appends content to a file, creating it (and parent dirs) if
	// absent. Used by log-sink extensions.
	AppendFile(path, content string) error
	HTTPPost(url string, headers map[string]string, body []byte) (int, []byte, error)
	ConfigRead(group string) (json.RawMessage, error)
}

// UIBridge is the interface extensions call to interact with the UI.
// Set once on Host at startup via Host.SetUIBridge; replaces the
// OnNotify, OnModal, OnShowPicker, OnAbort, OnSetStatus, OnGetStatusInfo,
// OnSendMessage, OnRegisterCommand, OnRegisterTool, OnSetSystemPrompt,
// OnAppendSystemPrompt, OnAgentResetHistory, OnToolResult, OnAfterToolCall,
// OnConsoleOutput, OnConsoleClear callback fields.
//
// If SetUIBridge is never called (or only an earlyUIBridge stub is installed),
// UI operations return no-ops or appropriate stub responses.
//
// All methods are safe to call concurrently from multiple goroutines.
// The harness renderer explicitly documents: "All methods must be safe to call
// from goroutines outside the bubbletea event loop." UIBridge implementations
// must uphold the same guarantee.
type UIBridge interface {
	Notify(text string)
	ShowModal(text string)
	ShowPicker(title string, items []sdk.ShowPickerItem, callback string)
	Abort()
	SetStatus(key, value string)
	GetStatusInfo() sdk.StatusInfo
	SendMessage(msg sdk.Message)
	RegisterCommand(name, desc string, instant bool) error
	RegisterTool(tool sdk.Tool) error
	SetSystemPrompt(prompt string)
	AppendSystemPrompt(text string)
	ResetHistory(messages []sdk.Message) error
	ToolResult(toolCallID, result string, isError bool)
	AfterToolCall(agentID, toolCallID, toolName, result string, isError bool)
	ConsoleOutput(line string)
	ConsoleClear()
	// CreateArea registers a new UI scene-graph area owned by an extension.
	// Returns an error if the area ID already exists.
	CreateArea(area sdk.UIArea) error
	// PatchUI applies a batch of scene-graph patch ops to an area atomically.
	// Returns an error if the area or any referenced node is missing.
	PatchUI(params sdk.UIPatchParams) error
	// RemoveArea removes a UI area and its scene graph. Removing a missing
	// area is a no-op.
	RemoveArea(id string)
	// UpdateArea updates the sizing constraints and/or weight of an existing
	// area. Omitted (empty/nil) fields leave current values unchanged.
	// Returns an error if the area ID does not exist.
	UpdateArea(params sdk.UIUpdateAreaParams) error
}

// MCPBridge is the interface extensions call to manage MCP server subprocesses.
// Set once on Host at startup via Host.SetMCPBridge; replaces the
// OnMCPSpawn, OnMCPClose, OnMCPSend, OnMCPRead callback fields.
//
// If SetMCPBridge is never called, the host returns "not supported by host"
// for all MCP operations.
//
// All methods are safe to call concurrently from multiple goroutines.
// Implementations must be goroutine-safe; they are called from concurrent WASM dispatch.
type MCPBridge interface {
	Spawn(id, command string, args []string, env map[string]string) error
	Close(id string) error
	Send(id string, data []byte) error
	Read(id string) (json.RawMessage, error)
}
