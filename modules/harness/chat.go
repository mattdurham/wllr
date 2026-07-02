package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
)

// NewChatView creates a ChatView with the given dimensions.
func NewChatView(width, height int) ChatView {
	vp := viewport.New()
	vp.SetWidth(width)
	vp.SetHeight(height)
	return ChatView{vp: vp, width: width, height: height}
}

// SetSize updates the viewport dimensions. No-op when dimensions are unchanged.
// The transcript content is re-fed by the Model after a resize.
func (c *ChatView) SetSize(width, height int) {
	if c.width == width && c.height == height {
		return
	}
	wasAtBottom := c.vp.AtBottom()
	c.width = width
	c.height = height
	c.vp.SetWidth(width)
	c.vp.SetHeight(height)
	c.vp.SetContent(c.externalContent)
	if wasAtBottom {
		c.vp.GotoBottom()
	}
}

// ClearToolLog resets the per-turn tool-call log. Called at the start of a turn.
func (c *ChatView) ClearToolLog() { c.toolLog = nil }

// AddToolCall records a tool call in the per-turn log (shown via /tools).
func (c *ChatView) AddToolCall(id, agentID, toolName, input string) {
	c.toolLog = append(c.toolLog, ToolLogEntry{ID: id, AgentID: agentID, Name: toolName, Preview: toolInputPreview(input)})
}

// UpdateToolCall marks the matching pending tool log entry as done.
// If the ID is missing (legacy/internal callers), it falls back to the last
// pending entry so the log still completes.
func (c *ChatView) UpdateToolCall(id, agentID, toolName string, isError bool, _ string) {
	if id != "" {
		for i := len(c.toolLog) - 1; i >= 0; i-- {
			if c.toolLog[i].ID == id {
				c.toolLog[i].Done = true
				c.toolLog[i].IsError = isError
				if c.toolLog[i].AgentID == "" {
					c.toolLog[i].AgentID = agentID
				}
				if c.toolLog[i].Name == "" {
					c.toolLog[i].Name = toolName
				}
				return
			}
		}
		if toolName != "" {
			c.toolLog = append(c.toolLog, ToolLogEntry{ID: id, AgentID: agentID, Name: toolName, Done: true, IsError: isError})
			return
		}
	}
	for i := len(c.toolLog) - 1; i >= 0; i-- {
		if !c.toolLog[i].Done {
			c.toolLog[i].Done = true
			c.toolLog[i].IsError = isError
			break
		}
	}
}

func (c *ChatView) ToolActivityLines(width, height int) []string {
	if height <= 0 || len(c.toolLog) == 0 {
		return nil
	}
	start := len(c.toolLog) - height
	if start < 0 {
		start = 0
	}
	lines := make([]string, 0, len(c.toolLog)-start)
	for _, e := range c.toolLog[start:] {
		status := "running"
		if e.Done && e.IsError {
			status = "error"
		} else if e.Done {
			status = "done"
		}
		line := status + " " + e.Name
		if e.AgentID != "" && e.AgentID != "main" {
			line += " [" + e.AgentID + "]"
		}
		if e.Preview != "" {
			line += "  " + e.Preview
		}
		lines = append(lines, truncateRunes(line, width))
	}
	return lines
}

// Update handles viewport scrolling.
func (c ChatView) Update(msg tea.Msg) (ChatView, tea.Cmd) {
	var cmd tea.Cmd
	c.vp, cmd = c.vp.Update(msg)
	return c, cmd
}

// ScrollUp scrolls the chat viewport up by n lines.
func (c *ChatView) ScrollUp(n int) { c.vp.ScrollUp(n) }

// ScrollDown scrolls the chat viewport down by n lines.
func (c *ChatView) ScrollDown(n int) { c.vp.ScrollDown(n) }

// View renders the chat content.
func (c ChatView) View() string {
	return c.vp.View()
}

// toolInputPreview returns a single-line summary of the tool's JSON input,
// suitable for compact display in the modal tool log.
// Returns empty string if input cannot be parsed or yields nothing useful.
func toolInputPreview(input string) string {
	priority := []string{"command", "path", "name", "message", "query", "text", "url", "content"}
	for _, key := range priority {
		needle := `"` + key + `"`
		idx := strings.Index(input, needle)
		if idx < 0 {
			continue
		}
		rest := input[idx+len(needle):]
		colon := strings.Index(rest, ":")
		if colon < 0 {
			continue
		}
		rest = strings.TrimSpace(rest[colon+1:])
		if len(rest) == 0 {
			continue
		}
		if rest[0] == '"' {
			end := strings.Index(rest[1:], `"`)
			if end >= 0 {
				val := rest[1 : end+1]
				preview := key + ": " + val
				if r := []rune(preview); len(r) > 80 {
					preview = string(r[:79]) + "…"
				}
				return preview
			}
		}
		break
	}
	return ""
}

func truncateRunes(s string, width int) string {
	if width <= 0 {
		return s
	}
	r := []rune(s)
	if len(r) <= width {
		return s
	}
	if width == 1 {
		return "…"
	}
	return string(r[:width-1]) + "…"
}

// ToolLogModal returns a formatted string for display in the modal overlay.
// Returns a message indicating no tools were called if the log is empty.
func (c *ChatView) ToolLogModal() string {
	if len(c.toolLog) == 0 {
		return "No tools called this turn."
	}
	var sb strings.Builder
	sb.WriteString("Tool calls this turn:\n\n")
	for i, e := range c.toolLog {
		var status string
		if !e.Done {
			status = "◌ running"
		} else if e.IsError {
			status = "● error"
		} else {
			status = "● done"
		}
		fmt.Fprintf(&sb, "%d. %s  %s\n", i+1, e.Name, status)
		if e.Preview != "" {
			fmt.Fprintf(&sb, "   %s\n", e.Preview)
		}
	}
	return strings.TrimRight(sb.String(), "\n")
}
