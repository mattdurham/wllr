package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import "charm.land/bubbles/v2/viewport"

// ChatView is a scrollable viewport whose transcript content is produced
// externally — by a WASM extension driving the "chat" scene area — and fed in
// via SetExternalContent. The viewport (scroll, size, GotoBottom) is
// harness-owned; ChatView no longer renders messages itself.
//
// It also retains a per-turn tool-call log, which is independent of the
// transcript and surfaced via the /tools command (ToolLogModal).
type ChatView struct {
	externalContent string
	toolLog         []ToolLogEntry
	vp              viewport.Model
	width           int
	height          int

	// Mouse text selection, stored in viewport-relative screen cells (see
	// selCell). selActive spans a left-button press inside the transcript to the
	// following release; selDirty records whether the pointer actually moved, so
	// a bare click does not copy a single cell. See selection.go.
	selActive bool
	selDirty  bool
	selAnchor selCell
	selFocus  selCell
}

// SetExternalContent replaces the viewport content with the supplied string
// (produced by the WASM transcript). It follows the tail only when the viewport
// was already at the bottom, preserving manual scrollback during streaming.
func (c *ChatView) SetExternalContent(content string) {
	wasAtBottom := c.vp.AtBottom()
	c.externalContent = content
	c.vp.SetContent(content)
	if wasAtBottom {
		c.vp.GotoBottom()
	}
}
