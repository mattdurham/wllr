// Package sdk defines shared types for the bob coding harness and its WASM extensions.
package sdk

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import "encoding/json"

// EventType identifies a lifecycle event dispatched to extensions.
type EventType string

const (
	EventSessionStart          EventType = "session_start"
	EventBeforeAgentStart      EventType = "before_agent_start"
	EventBeforeProviderRequest EventType = "before_provider_request"
	EventAfterProviderResponse EventType = "after_provider_response"
	EventOnToolCall            EventType = "on_tool_call"
	EventOnToolResult          EventType = "on_tool_result"
	EventMessageStart          EventType = "message_start"
	EventMessageEnd            EventType = "message_end"
	EventShutdown              EventType = "shutdown"
	// EventBeforeToolCall is dispatched before a tool is executed.
	// Extensions may cancel the call by setting Cancel: true in their response.
	EventBeforeToolCall EventType = "before_tool_call"
	// EventAfterToolCall is dispatched after a tool result is available.
	EventAfterToolCall EventType = "after_tool_call"
)

// Event is dispatched to extensions via _on_event.
type Event struct {
	Type    EventType       `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// EventResponse is the optional JSON response from _on_event.
type EventResponse struct {
	Cancel bool   `json:"cancel,omitempty"`
	Block  bool   `json:"block,omitempty"`
	Error  string `json:"error,omitempty"`
}

// Payload types for each event.

// SessionStartPayload is the payload for EventSessionStart.
type SessionStartPayload struct {
	Reason string `json:"reason"`
}

// BeforeAgentStartPayload is the payload for EventBeforeAgentStart.
type BeforeAgentStartPayload struct {
	Prompt       string `json:"prompt"`
	SystemPrompt string `json:"system_prompt"`
}

// BeforeProviderRequestPayload is the payload for EventBeforeProviderRequest.
type BeforeProviderRequestPayload struct {
	Messages []Message `json:"messages"`
	Model    string    `json:"model"`
}

// AfterProviderResponsePayload is the payload for EventAfterProviderResponse.
type AfterProviderResponsePayload struct {
	Usage UsageStats `json:"usage"`
}

// OnToolCallPayload is the payload for EventOnToolCall.
type OnToolCallPayload struct {
	ToolCallID string          `json:"tool_call_id"`
	ToolName   string          `json:"tool_name"`
	Input      json.RawMessage `json:"input"`
}

// OnToolResultPayload is the payload for EventOnToolResult.
type OnToolResultPayload struct {
	ToolCallID string `json:"tool_call_id"`
	Result     string `json:"result"`
	IsError    bool   `json:"is_error"`
}

// MessageStartPayload is the payload for EventMessageStart.
type MessageStartPayload struct {
	Role string `json:"role"`
}

// MessageEndPayload is the payload for EventMessageEnd.
type MessageEndPayload struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ShutdownPayload is the payload for EventShutdown.
type ShutdownPayload struct {
	Reason string `json:"reason"`
}

// UsageStats holds token usage from a provider response.
type UsageStats struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// Role is a message role (user or assistant).
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// Message is a chat message.
type Message struct {
	Role    Role   `json:"role"`
	Content string `json:"content"`
}

// Tool is a function the LLM may call.
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
	// Override, when true, allows this registration to replace an existing tool
	// with the same name. Without this flag, duplicate registrations are rejected.
	Override bool `json:"override,omitempty"`
}

// Permission identifies a capability that an extension may request.
// User extensions declare permissions in their manifest; built-in extensions
// (loaded via Host.LoadBytes) are granted all permissions automatically.
type Permission string

const (
	// PermExec grants the extension the right to execute arbitrary commands.
	PermExec Permission = "exec"
	// PermFileOpen grants the extension the right to open files.
	PermFileOpen Permission = "file_open"
	// PermFileRead grants the extension the right to read file contents.
	PermFileRead Permission = "file_read"
	// PermFileWrite grants the extension the right to write file contents.
	PermFileWrite Permission = "file_write"
	// PermNetworkRead grants the extension the right to read from the network.
	PermNetworkRead Permission = "network_read"
	// PermNetworkWrite grants the extension the right to write to the network.
	PermNetworkWrite Permission = "network_write"
)

// ExtensionManifest is loaded from the JSON file alongside a .wasm extension.
// It declares the permissions the extension requires.
type ExtensionManifest struct {
	// Permissions is the list of permissions the extension requires.
	Permissions []Permission `json:"permissions"`
}

// BeforeToolCallPayload is the payload for EventBeforeToolCall.
type BeforeToolCallPayload struct {
	ToolCallID string          `json:"tool_call_id"`
	ToolName   string          `json:"tool_name"`
	Input      json.RawMessage `json:"input"`
}

// AfterToolCallPayload is the payload for EventAfterToolCall.
type AfterToolCallPayload struct {
	ToolCallID string `json:"tool_call_id"`
	ToolName   string `json:"tool_name"`
	Result     string `json:"result"`
	IsError    bool   `json:"is_error"`
}

// HostCallRequest is the JSON payload sent by an extension via host_call.
type HostCallRequest struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

// HostCallResponse is the JSON response returned by the host via host_call.
type HostCallResponse struct {
	Result json.RawMessage `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
}

// host_call method constants.
const (
	MethodSubscribe       = "subscribe"
	MethodRegisterTool    = "register_tool"
	MethodRegisterCommand = "register_command"
	MethodSendMessage     = "send_message"
	MethodSetStatus       = "set_status"
	MethodNotify          = "notify"
	MethodToolResult      = "tool_result"
	MethodStoreSet        = "store_set"
	MethodStoreGet        = "store_get"
	MethodAbort           = "abort"
	// MethodRequestPermission checks whether the calling extension holds a
	// permission.  Returns an error response if the permission is not granted.
	MethodRequestPermission = "request_permission"
	// MethodGetEnv reads environment variables from the host. Requires PermFileRead (env is read-only).
	MethodGetEnv = "get_env"
	// MethodConfigRead reads the calling extension's config group from the shared config file.
	// No params — the group is determined by the extension's registered name.
	MethodConfigRead = "config_read"
	// MethodExec executes a shell command on the host. Requires PermExec.
	MethodExec = "exec"
	// MethodBeforeToolCall is sent by extensions that want to intercept tool
	// calls before execution.
	MethodBeforeToolCall = "before_tool_call"
	// MethodAfterToolCall is sent by extensions that want to observe tool
	// results after execution.
	MethodAfterToolCall = "after_tool_call"

	// Agent management host_call methods. Called by the agents WASM extension.
	MethodAgentSpawn       = "agent_spawn"
	MethodAgentClose       = "agent_close"
	MethodAgentSendMessage = "agent_send_message"
	MethodAgentList        = "agent_list"
	MethodAgentTokenCount  = "agent_token_count"

	// Team management host_call methods.
	MethodTeamCreate       = "team_create"
	MethodTeamClose        = "team_close"
	MethodTeamAddMember    = "team_add_member"
	MethodTeamRemoveMember = "team_remove_member"
)
