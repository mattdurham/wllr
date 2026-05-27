// Package harness implements the bubbletea TUI for the bob coding assistant.
package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// TokenMsg carries a single streamed token from the provider.

// StreamDoneMsg signals that a provider stream has finished.
// AgentID identifies which agent finished; only the main agent's done
// message updates streaming state and finalizes the chat message.

// ExtensionEventResultMsg carries results from dispatching an event to extensions.

// ReloadMsg triggers a hot-reload of all loaded extensions.

// NotifyMsg carries a notification message to display in the chat.

// StatusUpdateMsg sets or updates a keyed value in the status bar.

// SubmitMsg carries user-submitted input text.
// Display, if non-empty, is shown in the chat instead of Content
// (useful when Content is a large internal payload like a skill XML block).

// CommandMsg carries a parsed slash command.

// ToolCallStartMsg is sent when the agent dispatches a tool call.

// ShowModalMsg asks the TUI to open a modal overlay with the given text.

// ToolCallDoneMsg is sent when a tool call completes (via OnAfterToolCall).

// abortStreamMsg is sent by the OnAbort callback to cancel the active agent turn
// through the bubbletea program.

// dispatchOnCommandMsg is sent when an extension-registered slash command is invoked.
// The harness dispatches EventOnCommand to all subscribed extensions.

// streamTickMsg fires periodically while streaming to update the working indicator.

// ShowPickerMsg asks the TUI to open the interactive picker overlay.

// ResetHistoryMsg asks the TUI to replace the main agent's history and
// rebuild the chat view from the supplied messages.

// sessionStartDoneMsg is returned after EventSessionStart has been dispatched to
// all extensions. It is distinct from ExtensionEventResultMsg so the harness can
// inject the default action prompt exactly once, after all session_start handlers
// have had a chance to register tools and commands.

// agentWakeupMsg is sent when OnAgentRun triggers a main-agent turn (e.g.
// a sub-agent called send_message). It sets m.streaming=true so the TUI
// shows the "working." indicator during the agent-triggered turn.
