package extension

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"context"
	"encoding/json"

	"github.com/mattdurham/wllr/modules/sdk"
)

// SpawnRequest carries the parameters for spawning a sub-agent.
// Replaces the positional parameter list previously used in OnAgentSpawn.
type SpawnRequest struct {
	ID             string
	Name           string
	SystemPrompt   string
	ModelName      string
	InitialPrompt  string
	ThinkingBudget int
}

// AgentBridge is the interface extensions call to manage agents.
// Set once on Host at startup via Host.SetAgentBridge; replaces the
// OnAgentSpawn, OnAgentClose, OnAgentSendMessage, OnAgentRun,
// OnAgentList, OnAgentTokenCount callback fields.
type AgentBridge interface {
	Spawn(ctx context.Context, req SpawnRequest) error
	Close(id string) error
	SendMessage(id, message string) error
	Run(id string) error
	List() ([]AgentInfo, error)
	TokenCount() int64
	SetHistory(id string, messages []sdk.Message) error
}

// TeamBridge is the interface extensions call to manage teams.
// Set once on Host at startup via Host.SetTeamBridge; replaces the
// OnTeamCreate, OnTeamClose, OnTeamAddMember, OnTeamRemoveMember,
// OnTeamGetInfo, OnTeamList callback fields.
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
type CapabilityProvider interface {
	Exec(ctx context.Context, command, dir string, onLine func(string)) (string, error)
	GetEnv(name string) (string, error)
	ReadFile(path string) (string, error)
	WriteFile(path, content string) error
	HTTPPost(url string, headers map[string]string, body []byte) (int, []byte, error)
	ConfigRead(group string) (json.RawMessage, error)
}

// UIBridge is the interface extensions call to interact with the UI.
// Set once on Host at startup via Host.SetUIBridge; replaces the
// OnNotify, OnModal, OnShowPicker, OnAbort, OnSetStatus, OnGetStatusInfo,
// OnSendMessage, OnRegisterCommand, OnRegisterTool, OnSetSystemPrompt,
// OnAppendSystemPrompt, OnAgentResetHistory, OnToolResult, OnAfterToolCall,
// OnConsoleOutput, OnConsoleClear callback fields.
type UIBridge interface {
	Notify(text string)
	ShowModal(text string)
	ShowPicker(title string, items []sdk.ShowPickerItem, callback string)
	Abort()
	SetStatus(key, value string)
	GetStatusInfo() sdk.StatusInfo
	SendMessage(msg sdk.Message)
	RegisterCommand(name, desc string) error
	RegisterTool(tool sdk.Tool) error
	SetSystemPrompt(prompt string)
	AppendSystemPrompt(text string)
	ResetHistory(messages []sdk.Message) error
	ToolResult(toolCallID, result string, isError bool)
	AfterToolCall(toolCallID, toolName, result string, isError bool)
	ConsoleOutput(line string)
	ConsoleClear()
}

// MCPBridge is the interface extensions call to manage MCP server subprocesses.
// Set once on Host at startup via Host.SetMCPBridge; replaces the
// OnMCPSpawn, OnMCPClose, OnMCPSend, OnMCPRead callback fields.
type MCPBridge interface {
	Spawn(id, command string, args []string, env map[string]string) error
	Close(id string) error
	Send(id string, data []byte) error
	Read(id string) (json.RawMessage, error)
}
