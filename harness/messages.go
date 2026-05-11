// Package harness implements the bubbletea TUI for the bob coding assistant.
package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import "github.com/mattdurham/wllr/sdk"

// TokenMsg carries a single streamed token from the provider.
type TokenMsg struct{ Token string }

// StreamDoneMsg signals that a provider stream has finished.
// AgentID identifies which agent finished; only the main agent's done
// message updates streaming state and finalizes the chat message.
type StreamDoneMsg struct {
	Err     error
	AgentID string
}

// ExtensionEventResultMsg carries results from dispatching an event to extensions.
type ExtensionEventResultMsg struct {
	Err     error
	Results []sdk.EventResponse
}

// ReloadMsg triggers a hot-reload of all loaded extensions.
type ReloadMsg struct{}

// NotifyMsg carries a notification message to display in the chat.
type NotifyMsg struct{ Text string }

// StatusUpdateMsg sets or updates a keyed value in the status bar.
type StatusUpdateMsg struct{ Key, Value string }

// SubmitMsg carries user-submitted input text.
// Display, if non-empty, is shown in the chat instead of Content
// (useful when Content is a large internal payload like a skill XML block).
type SubmitMsg struct {
	Content string
	Display string
}

// CommandMsg carries a parsed slash command.
type CommandMsg struct {
	Name string
	Args []string
}

// ToolCallStartMsg is sent when the agent dispatches a tool call.
type ToolCallStartMsg struct {
	ID       string
	ToolName string
	Input    string
}

// ShowModalMsg asks the TUI to open a modal overlay with the given text.
type ShowModalMsg struct{ Text string }

// ToolCallDoneMsg is sent when a tool call completes (via OnAfterToolCall).
type ToolCallDoneMsg struct {
	ID      string
	Output  string
	IsError bool
}

// abortStreamMsg is sent by the OnAbort callback to cancel the active agent turn
// through the bubbletea program.
type abortStreamMsg struct{}

// dispatchOnCommandMsg is sent when an extension-registered slash command is invoked.
// The harness dispatches EventOnCommand to all subscribed extensions.
type dispatchOnCommandMsg struct {
	Name string
	Args []string
}

// streamTickMsg fires periodically while streaming to update the working indicator.
type streamTickMsg struct{}

// ShowPickerMsg asks the TUI to open the interactive picker overlay.
type ShowPickerMsg struct {
	Title    string
	Callback string
	Items    []sdk.ShowPickerItem
}

// ResetHistoryMsg asks the TUI to replace the main agent's history and
// rebuild the chat view from the supplied messages.
type ResetHistoryMsg struct {
	Messages []sdk.Message
}

// sessionStartDoneMsg is returned after EventSessionStart has been dispatched to
// all extensions. It is distinct from ExtensionEventResultMsg so the harness can
// inject the default action prompt exactly once, after all session_start handlers
// have had a chance to register tools and commands.
type sessionStartDoneMsg struct {
	Err     error
	Results []sdk.EventResponse
}
