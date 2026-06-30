package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import "charm.land/bubbles/v2/viewport"

// ChatView renders the conversation history in a scrollable viewport.
//
// When externalMode is true the viewport content is supplied wholesale by an
// external producer (a WASM extension driving the "chat" scene area) via
// SetExternalContent; the internal message-rendering path is bypassed. The
// viewport itself (scroll, size, GotoBottom) is always harness-owned.
type ChatView struct {
	current         string
	lastDoneToolID  string
	histContent     string
	externalContent string
	toolLog         []ToolLogEntry
	messages        []chatMessage
	queued          []chatMessage // messages sent while streaming; rendered below current
	vp              viewport.Model
	width           int
	height          int
	histDirty       bool
	afterTool       bool
	externalMode    bool
}

// SetExternalContent switches the view into external mode and replaces the
// viewport content with the supplied string, scrolling to the bottom. Used when
// a WASM extension owns the transcript via the scene graph.
func (c *ChatView) SetExternalContent(content string) {
	c.externalMode = true
	c.externalContent = content
	c.vp.SetContent(content)
	c.vp.GotoBottom()
}

// ExternalMode reports whether the view is sourcing content externally.
func (c *ChatView) ExternalMode() bool { return c.externalMode }
