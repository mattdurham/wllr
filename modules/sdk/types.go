// Package sdk defines shared types for the bob coding harness and its WASM extensions.
package sdk

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

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
	// EventTick is dispatched once per second by the harness for time-based extensions.
	EventTick EventType = "tick"
	// EventBeforeToolCall is dispatched before a tool is executed.
	// Extensions may cancel the call by setting Cancel: true in their response.
	EventBeforeToolCall EventType = "before_tool_call"
	// EventAfterToolCall is dispatched after a tool result is available.
	EventAfterToolCall EventType = "after_tool_call"
	// EventOnCommand is dispatched when the user invokes a slash command registered
	// by an extension via register_command.
	EventOnCommand EventType = "on_command"
	// EventContextUsage is dispatched after each completed turn with the current
	// context window usage as a ContextUsagePayload.
	EventContextUsage EventType = "context_usage"
	// EventToken is dispatched with a batch of streamed assistant text (a
	// TokenPayload) as the agent produces it. Batched (~30ms) to keep the WASM
	// boundary crossing rate bounded. Used to route streaming text through
	// extensions that drive the UI scene graph.
	EventToken EventType = "token"
	// EventNotify is dispatched whenever a system notification line is shown in
	// the chat (a NotifyPayload). Lets extensions that own the transcript render
	// notifications into their scene graph.
	EventNotify EventType = "notify"
)

// Event is dispatched to extensions via _on_event.

// EventResponse is the optional JSON response from _on_event.

// Payload types for each event.

// SessionStartPayload is the payload for EventSessionStart.

// BeforeAgentStartPayload is the payload for EventBeforeAgentStart.

// BeforeProviderRequestPayload is the payload for EventBeforeProviderRequest.

// AfterProviderResponsePayload is the payload for EventAfterProviderResponse.

// OnToolCallPayload is the payload for EventOnToolCall.

// OnToolResultPayload is the payload for EventOnToolResult.

// MessageStartPayload is the payload for EventMessageStart.

// MessageEndPayload is the payload for EventMessageEnd.

// ShutdownPayload is the payload for EventShutdown.

// UsageStats holds token usage from a provider response.

// Role is a message role (user or assistant).
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// Message is a chat message.

// Tool is a function the LLM may call.

// Override, when true, allows this registration to replace an existing tool
// with the same name. Without this flag, duplicate registrations are rejected.

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
	// PermUI grants the extension the right to drive the TUI scene graph:
	// create/remove areas and apply scene-graph patches.
	PermUI Permission = "ui"
)

// ExtensionManifest is loaded from the JSON file alongside a .wasm extension.
// It declares the permissions the extension requires.

// Permissions is the list of permissions the extension requires.

// BeforeToolCallPayload is the payload for EventBeforeToolCall.

// AfterToolCallPayload is the payload for EventAfterToolCall.

// OnCommandPayload is the payload for EventOnCommand.

// HostCallRequest is the JSON payload sent by an extension via host_call.

// HostCallResponse is the JSON response returned by the host via host_call.

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
	// MethodGetOS returns the host operating system and architecture strings.
	// Returns {"os": "darwin", "arch": "arm64"} (GOOS/GOARCH values).
	// No permission required.
	MethodGetOS = "get_os"
	// MethodConfigRead reads the calling extension's config group from the shared config file.
	// No params — the group is determined by the extension's registered name.
	MethodConfigRead = "config_read"
	// MethodModal displays text in a modal overlay window.
	MethodModal = "modal"
	// MethodSetSystemPrompt sets the system prompt on the main agent.
	MethodSetSystemPrompt = "set_system_prompt"
	// MethodAppendSystemPrompt appends text to the existing system prompt.
	MethodAppendSystemPrompt = "append_system_prompt"
	// MethodExec executes a shell command on the host. Requires PermExec.
	MethodExec = "exec"
	// MethodReadFile reads the contents of a file on the host filesystem.
	// Requires PermFileRead.
	MethodReadFile = "read_file"
	// MethodWriteFile writes content to a file on the host filesystem.
	// Requires PermFileWrite.
	MethodWriteFile = "write_file"
	// MethodHTTPPost makes an HTTP POST request from the host.
	// Requires PermNetworkWrite.
	MethodHTTPPost = "http_post"
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
	// MethodAgentDeliver appends a message to an agent's inbox and, when wake is
	// true (default), ensures the agent processes it — the atomic counterpart to
	// an agent_send_message + agent_run pair. Params: {id, message, type?, wake?}.
	MethodAgentDeliver = "agent_deliver"
	// MethodAgentRun triggers an immediate turn for an existing agent.
	MethodAgentRun        = "agent_run"
	MethodAgentList       = "agent_list"
	MethodAgentTokenCount = "agent_token_count"
	// Team management host_call methods.
	MethodTeamCreate       = "team_create"
	MethodTeamClose        = "team_close"
	MethodTeamAddMember    = "team_add_member"
	MethodTeamRemoveMember = "team_remove_member"
	// MethodTeamGetInfo returns the member agent IDs for a team.
	MethodTeamGetInfo = "team_get_info"
	// MethodTeamList returns all registered team IDs.
	MethodTeamList = "team_list"

	// MCP bridge host_call methods.
	MethodMCPSpawn = "mcp_spawn"
	MethodMCPClose = "mcp_close"
	MethodMCPSend  = "mcp_send"
	MethodMCPRead  = "mcp_read"

	// MethodShowPicker opens an interactive TUI list picker. After the user
	// selects an item the harness fires EventOnCommand{name: callback, args: [id]}.
	MethodShowPicker = "show_picker"
	// MethodAgentResetHistory replaces the main agent's conversation history
	// and rebuilds the chat view from the supplied messages.
	MethodAgentResetHistory = "agent_reset_history"

	// MethodGetStatusInfo returns the current status bar data so extensions can
	// build a custom status line. Returns StatusInfo JSON.
	// No permission required — this is read-only observability.
	MethodGetStatusInfo = "get_status_info"

	// MethodGetContextUsage returns the current context window usage as a
	// ContextUsage JSON object. No permission required — this is read-only observability.
	MethodGetContextUsage = "get_context_usage"

	// MethodSetStatusLine replaces the entire status bar text with a custom
	// string for this session. Pass an empty string to revert to the default
	// auto-generated line.
	// No permission required.
	MethodSetStatusLine = "set_status_line"

	// MethodUICreateArea registers a new UI area (a named screen region the
	// extension owns). Params: UICreateAreaParams. Requires PermUI.
	MethodUICreateArea = "ui_create_area"
	// MethodUIPatch applies a batch of scene-graph patch ops to an area.
	// Params: UIPatchParams. Requires PermUI.
	MethodUIPatch = "ui_patch"
	// MethodUIRemoveArea removes a UI area and its scene graph.
	// Params: {"area": "<id>"}. Requires PermUI.
	MethodUIRemoveArea = "ui_remove_area"
	// MethodUIUpdateArea updates the sizing constraints and/or weight of an
	// existing area. Params: UIUpdateAreaParams. Requires PermUI.
	// Returns an error if the area ID does not exist.
	MethodUIUpdateArea = "ui_update_area"
)

// ShowPickerItem is one entry displayed in the interactive picker overlay.

// ShowPickerParams is the params blob for the show_picker host_call.

// AgentResetHistoryParams is the params blob for the agent_reset_history host_call.

// StatusInfo is returned by the get_status_info host_call.
// It gives extensions a read-only snapshot of the current status bar state
// so they can compose a fully custom status line.

// Statuses is the current set of keyed status values set via set_status.
// The "_override" key is excluded — use set_status_line to manage it.

// Provider is the active provider name (e.g. "anthropic").

// Model is the active model name (e.g. "claude-opus-4-5").

// Tokens is the total token count across all agents in the current session.

// Working is true while the LLM is streaming a response.
