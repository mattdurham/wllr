package harness

import "charm.land/bubbles/v2/viewport"

// ChatView renders the conversation history in a scrollable viewport.
type ChatView struct {
	current        string
	lastDoneToolID string
	histContent    string
	toolLog        []ToolLogEntry
	messages       []chatMessage
	vp             viewport.Model
	width          int
	height         int
	histDirty      bool
	afterTool      bool
}
