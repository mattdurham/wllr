package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"encoding/json"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattdurham/wllr/modules/agent"
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
	c.toolLog = append(
		c.toolLog,
		ToolLogEntry{ID: id, AgentID: agentID, Name: toolName, Preview: toolInputPreview(input)},
	)
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
			c.toolLog = append(
				c.toolLog,
				ToolLogEntry{ID: id, AgentID: agentID, Name: toolName, Done: true, IsError: isError},
			)
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
	if width <= 0 || height <= 0 || len(c.toolLog) == 0 {
		return nil
	}
	start := len(c.toolLog) - toolActivityEntryLimit
	if start < 0 {
		start = 0
	}
	lines := make([]string, 0, height)
	for _, e := range c.toolLog[start:] {
		status := "running"
		if e.Done && e.IsError {
			status = "error"
		} else if e.Done {
			status = "done"
		}
		line := status + " " + e.Name
		if e.AgentID != "" && e.AgentID != agent.MainAgentID {
			line += " [" + e.AgentID + "]"
		}
		if e.Preview != "" {
			preview := strings.Join(strings.Fields(e.Preview), " ")
			budget := width - ansi.StringWidth(line) - 2
			// For paths retain the filename and trailing directories; commands
			// and other arguments retain their beginning. The log stays intact.
			for _, key := range []string{"path: ", "file_path: "} {
				if value, ok := strings.CutPrefix(preview, key); ok && budget > len(key) {
					preview = key + ansi.TruncateLeft(value, budget-len(key), "…")
					break
				}
			}
			if budget > 0 {
				line += "  " + ansi.Truncate(preview, budget, "…")
			} else {
				line += "  " + preview
			}
		}
		lines = append(lines, ansi.Truncate(line, width, "…"))
	}
	if len(lines) > height {
		lines = lines[:height]
	}
	return lines
}

const toolActivityEntryLimit = 3

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
	// The viewport always renders its full configured height, padding short
	// content with space-only rows. Those rows are layout space, not transcript
	// content; trim them here so lower panes do not appear separated by a large
	// blank gap. The outer model still pads the complete view to terminal height.
	out := c.vp.View()
	if c.selActive && c.selDirty {
		out = c.highlightSelection(out)
	}
	return strings.TrimRight(out, " \n")
}

// toolInputPreview returns a single-line summary of the tool's JSON input,
// suitable for compact display in the modal tool log.
// Returns empty string if input cannot be parsed or yields nothing useful.
func toolInputPreview(input string) string {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(input), &fields); err != nil {
		return ""
	}
	priority := []string{
		"command",
		"path",
		"file_path",
		"name",
		"message",
		"query",
		"pattern",
		"text",
		"url",
		"content",
	}
	for _, key := range priority {
		var value string
		if err := json.Unmarshal(fields[key], &value); err == nil && value != "" {
			return key + ": " + value
		}
	}
	return ""
}

// truncateRunes shortens s to at most width display columns, appending an
// ellipsis when it does not fit. It measures display width (via ansi.Truncate),
// not rune count, so wide characters cannot push the result past width and
// overflow the pane that renders it (issue #45).
func truncateRunes(s string, width int) string {
	if width <= 0 {
		return s
	}
	if ansi.StringWidth(s) <= width {
		return s
	}
	if width == 1 {
		return "…"
	}
	return ansi.Truncate(s, width, "…")
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
