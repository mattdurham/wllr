package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import "github.com/mattdurham/wllr/modules/sdk"

// Renderer is the interface the session/orchestration layer calls to update the UI.
// All methods must be safe to call from goroutines outside the bubbletea event loop.
// The concrete implementation wraps tea.Program.Send calls.
type Renderer interface {
	// AppendToken adds a streaming text token to the in-progress message.
	AppendToken(token string)
	// FinalizeMessage seals the in-progress message into history.
	FinalizeMessage()
	// AddUserMessage adds a user message bubble to the chat.
	AddUserMessage(content, display string)
	// AddNotification adds a system notification line.
	AddNotification(text string)
	// SetStreaming updates the streaming state indicator.
	SetStreaming(active bool, err error)
	// ShowModal opens a modal overlay with the given text.
	ShowModal(text string)
	// ShowPicker opens an interactive item picker.
	ShowPicker(title string, items []sdk.ShowPickerItem, callback string)
	// AddToolCall records a tool call start.
	AddToolCall(id, agentID, toolName, input string)
	// UpdateToolCall records a tool call completion.
	UpdateToolCall(id, agentID, toolName string, isError bool, output string)
	// SetStatus updates a keyed status entry.
	SetStatus(key, value string)
	// AppendConsoleLine adds a line to the console pane.
	AppendConsoleLine(line string)
	// ClearConsole clears the console pane.
	ClearConsole()
	// Abort cancels the active agent turn.
	Abort()
	// ResetHistory replaces the chat history with the given messages.
	ResetHistory(messages []sdk.Message) error
}
