package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
	"github.com/mattdurham/wllr/sdk"
)

const roleToolMessage = "tool"

// chatMessage is a finalised message in the chat history.

// ToolLogEntry records one tool call during the current agent turn.

// toolInputPreview result

// ChatView renders the conversation history in a scrollable viewport.

// current in-progress assistant message
// lastDoneToolID is set when a tool call completes; subsequent tokens
// are routed into that tool box until the next tool call or FinalizeMessage.

// histContent caches the rendered historical messages.
// Rebuilt only when messages change, not on every streaming token.

// toolLog records tool calls for the current turn. Cleared by FinalizeMessage.
// Shown on demand via /tools command or ctrl+t — not rendered inline.

// afterTool is true after a tool call completes and before the next token
// arrives. The first new token after a tool gets "\n\n" prepended so all
// text within one turn flows as one block in c.current.

var systemStyle = lipgloss.NewStyle().Italic(true).Foreground(lipgloss.Color("#555555"))

// NewChatView creates a ChatView with the given dimensions.
func NewChatView(width, height int) ChatView {
	vp := viewport.New()
	vp.SetWidth(width)
	vp.SetHeight(height)
	// histDirty starts true so the first refreshContent always builds histContent
	// using the histDirty flag rather than the histContent=="" fallback.
	return ChatView{vp: vp, width: width, height: height, histDirty: true}
}

// SetSize updates the viewport dimensions. No-op when dimensions are unchanged.
func (c *ChatView) SetSize(width, height int) {
	if c.width == width && c.height == height {
		return
	}
	c.width = width
	c.height = height
	c.vp.SetWidth(width)
	c.vp.SetHeight(height)
	c.invalidateHistory()
	c.refreshContent()
}

// AppendToken adds a token to the in-progress assistant message and scrolls to bottom.
// All tokens go to c.current regardless of tool state so the response is never
// fragmented across multiple boxes when the LLM interleaves text and tool calls.
func (c *ChatView) AppendToken(token string) {
	if c.afterTool {
		// Only add separator when there's preceding text — prevents leading
		// blank lines when the agent calls tools before writing any text.
		if c.current != "" {
			c.current += "\n\n"
		}
		c.afterTool = false
	}
	c.current += token
	c.refreshContent()
	c.vp.GotoBottom()
}

// FinalizeMessage seals the in-progress message and resets tool routing.
func (c *ChatView) FinalizeMessage() {
	c.lastDoneToolID = ""
	c.toolLog = nil
	c.afterTool = false
	if c.current == "" {
		// Still refresh so the viewport reflects cleared tool state.
		c.refreshContent()
		return
	}
	// All text from the turn is already in c.current (including pre- and
	// post-tool-call text, joined by \n\n via afterTool). Seal it as one message.
	c.messages = append(c.messages, chatMessage{role: sdk.RoleAssistant, content: c.current})
	c.current = ""
	c.invalidateHistory()
	c.refreshContent()
	c.vp.GotoBottom()
}

// AddUserMessage prepends a user message to the history.
func (c *ChatView) AddUserMessage(content string) {
	c.messages = append(c.messages, chatMessage{role: sdk.RoleUser, content: content})
	c.invalidateHistory()
	c.refreshContent()
	c.vp.GotoBottom()
}

// AddQueuedUserMessage stores a message sent while the agent was mid-turn.
// It is rendered below the current streaming output, not in the history.
func (c *ChatView) AddQueuedUserMessage(content string) {
	c.queued = append(c.queued, chatMessage{role: sdk.RoleUser, content: content, queued: true})
	c.refreshContent()
	c.vp.GotoBottom()
}

// UnqueueLastMessage moves all queued messages into the history and clears the queue.
// Called on StreamDoneMsg so the messages appear as normal history entries.
func (c *ChatView) UnqueueLastMessage() {
	if len(c.queued) == 0 {
		return
	}
	for _, m := range c.queued {
		m.queued = false
		c.messages = append(c.messages, m)
	}
	c.queued = nil
	c.invalidateHistory()
	c.refreshContent()
}

// AddNotification appends a system/notification line.
func (c *ChatView) AddNotification(text string) {
	c.messages = append(c.messages, chatMessage{role: sdk.Role("system"), content: text})
	c.invalidateHistory()
	c.refreshContent()
	c.vp.GotoBottom()
}

// AddToolCall records the tool call in the log. Text already in c.current stays
// there — all turn text remains in one block. After the tool completes, the
// next token gets "\n\n" prepended via the afterTool flag so segments separate
// cleanly without creating multiple boxes.
func (c *ChatView) AddToolCall(id, toolName, input string) {
	c.lastDoneToolID = ""
	c.toolLog = append(c.toolLog, ToolLogEntry{Name: toolName, Preview: toolInputPreview(input)})
	c.refreshContent()
}

// UpdateToolCall marks the last pending tool log entry as done and sets afterTool so
// the next streaming token starts a new paragraph within c.current.
func (c *ChatView) UpdateToolCall(id string, isError bool, output string) {
	c.lastDoneToolID = id
	for i := len(c.toolLog) - 1; i >= 0; i-- {
		if !c.toolLog[i].Done {
			c.toolLog[i].Done = true
			c.toolLog[i].IsError = isError
			break
		}
	}
	c.afterTool = true
	c.refreshContent()
}

// Clear resets the chat history.
func (c *ChatView) Clear() {
	c.messages = nil
	c.current = ""
	c.lastDoneToolID = ""
	c.toolLog = nil
	c.afterTool = false
	c.histContent = ""
	c.histDirty = false
	c.refreshContent()
}

// MessageCount returns the number of finalised messages.
func (c *ChatView) MessageCount() int { return len(c.messages) }

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

// invalidateHistory marks the historical-messages cache as stale so it will
// be rebuilt on the next refreshContent call.
func (c *ChatView) invalidateHistory() {
	c.histDirty = true
}

// refreshContent rebuilds the viewport content from messages.
// The rendered historical messages are cached; only the streaming current
// message is appended on every token, avoiding a full O(n) rebuild each time.
func (c *ChatView) refreshContent() {
	if c.histDirty {
		// Find the start of the most recent user turn and the last tool call.
		recentStart := 0
		for i := len(c.messages) - 1; i >= 0; i-- {
			if c.messages[i].role == sdk.RoleUser {
				recentStart = i
				break
			}
		}
		lastToolIdx := -1
		for i := len(c.messages) - 1; i >= 0; i-- {
			if c.messages[i].role == roleToolMessage {
				lastToolIdx = i
				break
			}
		}

		var sb strings.Builder
		i := 0
		for i < len(c.messages) {
			m := c.messages[i]

			if m.role != roleToolMessage {
				old := i < recentStart
				renderMessage(&sb, m, c.width, old)
				i++
				continue
			}

			// Collect the run of consecutive tool messages.
			j := i
			for j < len(c.messages) && c.messages[j].role == roleToolMessage {
				j++
			}
			group := c.messages[i:j]
			old := lastToolIdx < i // whole group is old if last tool is before this group
			renderToolGroup(&sb, group, c.width, old)
			i = j
		}
		c.histContent = sb.String()
		c.histDirty = false
	}

	var sb strings.Builder
	sb.WriteString(c.histContent)
	if c.current != "" {
		renderMessage(&sb, chatMessage{role: sdk.RoleAssistant, content: c.current}, c.width, false)
	}
	for _, q := range c.queued {
		renderMessage(&sb, q, c.width, false)
	}
	if sb.Len() > len(c.histContent) {
		c.vp.SetContent(sb.String())
	} else {
		c.vp.SetContent(c.histContent)
	}
}

func renderMessage(sb *strings.Builder, m chatMessage, width int, old bool) {
	const minWidth = 20
	if width < minWidth {
		width = minWidth
	}
	switch m.role {
	case sdk.RoleUser:
		renderUserMessage(sb, m.content, width, old, m.queued)
		return
	case sdk.RoleAssistant:
		renderAssistantMessage(sb, m.content, width, old)
		return
	case roleToolMessage:
		// Fallback for single tool messages outside the group path.
		renderToolGroup(sb, []chatMessage{m}, width, old)
		return
	default:
		sb.WriteString(systemStyle.Render(lipgloss.Wrap("» "+m.content, width, "")))
		sb.WriteString("\n\n")
	}
}

func renderAssistantMessage(sb *strings.Builder, content string, width int, old bool) {
	if width < 14 {
		width = 14
	}
	borderColor := lipgloss.Color("#89CFF0")
	textColor := lipgloss.Color("#FFFFFF")
	if old {
		borderColor = lipgloss.Color("#444444")
		textColor = lipgloss.Color("#555555")
	}
	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Foreground(textColor).
		Padding(0, 1).
		Width(width - 2) // -2 for left+right border chars
	sb.WriteString(style.Render(content))
	sb.WriteString("\n\n")
}

func renderUserMessage(sb *strings.Builder, content string, width int, old bool, queued bool) {
	if width < 14 {
		width = 14
	}
	borderColor := lipgloss.Color("#44AA44") // light green border, no fill
	textColor := lipgloss.Color("#DDFFDD")
	if old || queued {
		borderColor = lipgloss.Color("#444444")
		textColor = lipgloss.Color("#555555")
	}
	if queued {
		// Render manually so we can embed "─ queued… ─" in the top border.
		innerWidth := width - 2
		b := lipgloss.NewStyle().Foreground(borderColor)
		label := "─ queued… "
		fillWidth := innerWidth - lipgloss.Width(label)
		if fillWidth < 0 {
			fillWidth = 0
		}
		header := b.Render("╭" + label + strings.Repeat("─", fillWidth) + "╮")
		body := lipgloss.NewStyle().
			Foreground(textColor).
			Padding(0, 1).
			Width(width - 2).
			Render(content)
		// Strip the border that lipgloss would add — we're using a plain content block.
		// Wrap each line in manual side borders.
		bodyLines := strings.Split(strings.TrimRight(body, "\n"), "\n")
		var bodySb strings.Builder
		for _, line := range bodyLines {
			visible := lipgloss.Width(line)
			pad := innerWidth - 2 - visible
			if pad < 0 {
				pad = 0
			}
			bodySb.WriteString(b.Render("│") + " " + line + strings.Repeat(" ", pad) + " " + b.Render("│") + "\n")
		}
		footer := b.Render("╰" + strings.Repeat("─", innerWidth) + "╯")
		sb.WriteString(header + "\n" + bodySb.String() + footer)
		sb.WriteString("\n\n")
		return
	}
	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Foreground(textColor).
		Padding(0, 1).
		Width(width - 2)
	sb.WriteString(style.Render(content))
	sb.WriteString("\n\n")
}

// renderToolGroup is intentionally empty — tool calls are not shown in the
// chat. Only the LLM's text responses (rendered as assistant messages) are
// visible. Tool calls happen silently in the background.
func renderToolGroup(_ *strings.Builder, _ []chatMessage, _ int, _ bool) {}

// toolInputPreview returns a single-line summary of the tool's JSON input,
// suitable for compact display in the modal tool log.
// Returns empty string if input cannot be parsed or yields nothing useful.
func toolInputPreview(input string) string {
	priority := []string{"command", "path", "name", "message", "query", "text", "url", "content"}
	// Inline JSON parsing to avoid importing encoding/json at package level.
	// Use a simple string search for the first high-value key.
	for _, key := range priority {
		needle := `"` + key + `"`
		idx := strings.Index(input, needle)
		if idx < 0 {
			continue
		}
		// Find the colon after the key.
		rest := input[idx+len(needle):]
		colon := strings.Index(rest, ":")
		if colon < 0 {
			continue
		}
		rest = strings.TrimSpace(rest[colon+1:])
		if len(rest) == 0 {
			continue
		}
		// Unquote simple string values.
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
