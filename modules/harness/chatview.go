package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import "charm.land/bubbles/v2/viewport"

// ChatView renders the conversation history in a scrollable viewport.
type ChatView struct {
	current        string
	lastDoneToolID string
	histContent    string
	toolLog        []ToolLogEntry
	messages       []chatMessage
	queued         []chatMessage // messages sent while streaming; rendered below current
	vp             viewport.Model
	width          int
	height         int
	histDirty      bool
	afterTool      bool
}
