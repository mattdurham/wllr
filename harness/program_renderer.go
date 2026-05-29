package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	tea "charm.land/bubbletea/v2"
	"github.com/mattdurham/wllr/sdk"
)

// programRenderer implements Renderer by sending bubbletea messages via p.Send.
// All methods are safe to call from goroutines outside the bubbletea event loop
// because tea.Program.Send is goroutine-safe.
type programRenderer struct {
	p *tea.Program
}

// newProgramRenderer creates a Renderer backed by the given bubbletea program.
func newProgramRenderer(p *tea.Program) Renderer {
	return &programRenderer{p: p}
}

func (r *programRenderer) AppendToken(token string) {
	r.p.Send(TokenMsg{Token: token})
}

func (r *programRenderer) FinalizeMessage() {
	// FinalizeMessage is a no-op in the bubbletea model: the stream-done message
	// finalizes the message. This is called by the session layer for non-streaming
	// paths (future use).
}

func (r *programRenderer) AddUserMessage(content, display string) {
	r.p.Send(SubmitMsg{Content: content, Display: display})
}

func (r *programRenderer) AddNotification(text string) {
	r.p.Send(NotifyMsg{Text: text})
}

func (r *programRenderer) SetStreaming(active bool, err error) {
	r.p.Send(StreamDoneMsg{Err: err})
	_ = active
}

func (r *programRenderer) ShowModal(text string) {
	r.p.Send(ShowModalMsg{Text: text})
}

func (r *programRenderer) ShowPicker(title string, items []sdk.ShowPickerItem, callback string) {
	r.p.Send(ShowPickerMsg{Title: title, Items: items, Callback: callback})
}

func (r *programRenderer) AddToolCall(id, toolName, input string) {
	r.p.Send(ToolCallStartMsg{ID: id, ToolName: toolName, Input: input})
}

func (r *programRenderer) UpdateToolCall(id string, isError bool, output string) {
	r.p.Send(ToolCallDoneMsg{ID: id, IsError: isError, Output: output})
}

func (r *programRenderer) SetStatus(key, value string) {
	r.p.Send(StatusUpdateMsg{Key: key, Value: value})
}

func (r *programRenderer) AppendConsoleLine(line string) {
	r.p.Send(ConsoleMsg{Line: line})
}

func (r *programRenderer) ClearConsole() {
	r.p.Send(ConsoleMsg{Clear: true})
}

func (r *programRenderer) Abort() {
	r.p.Send(abortStreamMsg{})
}

func (r *programRenderer) ResetHistory(messages []sdk.Message) error {
	r.p.Send(ResetHistoryMsg{Messages: messages})
	return nil
}

// compile-time check
var _ Renderer = (*programRenderer)(nil)
