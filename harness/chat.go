package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"encoding/json"
	"strings"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
	"github.com/mattdurham/wllr/sdk"
)

const roleToolMessage = "tool"

// chatMessage is a finalised message in the chat history.
type chatMessage struct {
	role    sdk.Role
	content string
	// populated when role == "tool"
	toolID       string
	toolName     string
	toolInput    string
	toolOutput   string // raw tool result (kept for logging; not rendered)
	toolResponse string // LLM response text that follows the tool call
	toolDone     bool
	toolError    bool
}

// ChatView renders the conversation history in a scrollable viewport.
type ChatView struct {
	current string // current in-progress assistant message
	// lastDoneToolID is set when a tool call completes; subsequent tokens
	// are routed into that tool box until the next tool call or FinalizeMessage.
	lastDoneToolID string

	// histContent caches the rendered historical messages.
	// Rebuilt only when messages change, not on every streaming token.
	histContent string
	messages    []chatMessage
	vp          viewport.Model
	width       int
	height      int
	histDirty   bool

	// activityName tracks the tool currently running during this turn.
	// Each new tool call replaces the previous value. Cleared by FinalizeMessage.
	// Rendered as a single live box between histContent and c.current.
	activityName  string
	activityInput string // raw JSON input for the active tool
	activityDone  bool
	activityError bool
}

var (
	// Assistant box: blue border, white text.
	asstBorderStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#89CFF0"))
	asstBorderOldStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#444444"))
	asstTextStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFFFF"))
	asstTextOldStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#555555"))

	systemStyle        = lipgloss.NewStyle().Italic(true).Foreground(lipgloss.Color("#555555"))
	userBorderStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#00AA00"))
	userBorderOldStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#444444"))
	oldTextStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("#555555"))
	toolBorderStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#555555"))
	toolBorderOldStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#555555"))
	toolDotOldStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))
	toolSuccessStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#00AA00"))
	toolErrorStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#CC3333"))
	toolPendingStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#666666"))
)

// NewChatView creates a ChatView with the given dimensions.
func NewChatView(width, height int) ChatView {
	vp := viewport.New()
	vp.SetWidth(width)
	vp.SetHeight(height)
	return ChatView{vp: vp, width: width, height: height}
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
	c.current += token
	c.refreshContent()
	c.vp.GotoBottom()
}

// FinalizeMessage seals the in-progress message and resets tool routing.
func (c *ChatView) FinalizeMessage() {
	c.lastDoneToolID = ""
	hadActivity := c.activityName != ""
	c.activityName = ""
	c.activityInput = ""
	c.activityDone = false
	c.activityError = false
	if c.current == "" {
		if hadActivity {
			// Activity box was visible; refresh to remove it from the display.
			c.refreshContent()
			c.vp.GotoBottom()
		}
		return
	}
	// Merge into the previous assistant message if one is the last entry,
	// so consecutive AI responses appear as one box instead of many.
	if n := len(c.messages); n > 0 && c.messages[n-1].role == sdk.RoleAssistant {
		c.messages[n-1].content += "\n\n" + c.current
	} else {
		c.messages = append(c.messages, chatMessage{role: sdk.RoleAssistant, content: c.current})
	}
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

// AddNotification appends a system/notification line.
func (c *ChatView) AddNotification(text string) {
	c.messages = append(c.messages, chatMessage{role: sdk.Role("system"), content: text})
	c.invalidateHistory()
	c.refreshContent()
	c.vp.GotoBottom()
}

// AddToolCall updates the live activity box and merges any in-progress text
// into the current assistant block. Tool calls are not stored in c.messages
// (they are not rendered) so consecutive assistant text segments merge into
// one box without interruption.
func (c *ChatView) AddToolCall(id, toolName, input string) {
	if c.current != "" {
		// Merge into the last assistant message so all text from this turn
		// stays in one blue box, even when tool calls happen in between.
		if n := len(c.messages); n > 0 && c.messages[n-1].role == sdk.RoleAssistant {
			c.messages[n-1].content += "\n\n" + c.current
			c.invalidateHistory()
		} else {
			c.messages = append(c.messages, chatMessage{role: sdk.RoleAssistant, content: c.current})
			c.invalidateHistory()
		}
		c.current = ""
	}
	c.lastDoneToolID = ""
	c.activityName = toolName
	c.activityInput = input
	c.activityDone = false
	c.activityError = false
	c.refreshContent()
	c.vp.GotoBottom()
}

// UpdateToolCall marks the current activity box as done.
func (c *ChatView) UpdateToolCall(id string, isError bool, output string) {
	c.lastDoneToolID = id
	if c.activityName != "" {
		c.activityDone = true
		c.activityError = isError
	}
	c.refreshContent()
	c.vp.GotoBottom()
}

// Clear resets the chat history.
func (c *ChatView) Clear() {
	c.messages = nil
	c.current = ""
	c.lastDoneToolID = ""
	c.activityName = ""
	c.activityInput = ""
	c.activityDone = false
	c.activityError = false
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
	if c.histDirty || c.histContent == "" {
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

	if c.activityName != "" || c.current != "" {
		var sb strings.Builder
		sb.WriteString(c.histContent)
		if c.activityName != "" {
			renderActivityBox(&sb, c.activityName, c.activityInput, c.activityDone, c.activityError, c.width)
		}
		if c.current != "" {
			renderMessage(&sb, chatMessage{role: sdk.RoleAssistant, content: c.current}, c.width, false)
		}
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
		renderUserMessage(sb, m.content, width, old)
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
	innerWidth := width - 2
	contentWidth := innerWidth - 2
	if contentWidth < 1 {
		contentWidth = 1
	}

	border := asstBorderStyle
	text := asstTextStyle
	if old {
		border = asstBorderOldStyle
		text = asstTextOldStyle
	}

	sb.WriteString(border.Render("╭" + strings.Repeat("─", innerWidth) + "╮"))
	sb.WriteString("\n")

	wrapped := lipgloss.Wrap(content, contentWidth, "")
	for _, line := range strings.Split(strings.TrimRight(wrapped, "\n"), "\n") {
		runes := []rune(line)
		if len(runes) > contentWidth {
			runes = runes[:contentWidth]
		}
		padding := strings.Repeat(" ", contentWidth-len(runes))
		sb.WriteString(border.Render("│"))
		sb.WriteString(" " + text.Render(string(runes)) + padding + " ")
		sb.WriteString(border.Render("│"))
		sb.WriteString("\n")
	}

	sb.WriteString(border.Render("╰" + strings.Repeat("─", innerWidth) + "╯"))
	sb.WriteString("\n\n")
}

func renderUserMessage(sb *strings.Builder, content string, width int, old bool) {
	if width < 14 {
		width = 14
	}
	innerWidth := width - 2
	contentWidth := innerWidth - 2
	if contentWidth < 1 {
		contentWidth = 1
	}

	border := userBorderStyle
	text := lipgloss.NewStyle().Foreground(lipgloss.Color("#CCFFCC")) // soft green tint
	if old {
		border = userBorderOldStyle
		text = oldTextStyle
	}

	sb.WriteString(border.Render("╭" + strings.Repeat("─", innerWidth) + "╮"))
	sb.WriteString("\n")

	wrapped := lipgloss.Wrap(content, contentWidth, "")
	for _, line := range strings.Split(strings.TrimRight(wrapped, "\n"), "\n") {
		runes := []rune(line)
		if len(runes) > contentWidth {
			runes = runes[:contentWidth]
		}
		padding := strings.Repeat(" ", contentWidth-len(runes))
		sb.WriteString(border.Render("│"))
		sb.WriteString(" " + text.Render(string(runes)) + padding + " ")
		sb.WriteString(border.Render("│"))
		sb.WriteString("\n")
	}

	sb.WriteString(border.Render("╰" + strings.Repeat("─", innerWidth) + "╯"))
	sb.WriteString("\n\n")
}

// renderToolGroup is intentionally empty — tool calls are not shown in the
// chat. Only the LLM's text responses (rendered as assistant messages) are
// visible. Tool calls happen silently in the background.
func renderToolGroup(_ *strings.Builder, _ []chatMessage, _ int, _ bool) {}

// renderActivityBox draws a bordered box for the live activity indicator.
// It renders up to 4 content lines from the tool's JSON input, collapsing
// to fewer lines when the input has fewer meaningful fields.
func renderActivityBox(sb *strings.Builder, name, input string, done, isError bool, width int) {
	border := toolBorderStyle
	var dot string
	if !done {
		dot = toolPendingStyle.Render("◌")
	} else if isError {
		dot = toolErrorStyle.Render("●")
	} else {
		dot = toolSuccessStyle.Render("●")
	}

	if width < 14 {
		width = 14
	}
	innerWidth := width - 2
	contentWidth := innerWidth - 2 // "│ " + content + " │"
	if contentWidth < 1 {
		contentWidth = 1
	}

	// Top: ╭─ ◌  toolname ─────────╮
	labelRunes := 2 + 1 + 2 + len([]rune(name))
	fillLen := innerWidth - labelRunes
	if fillLen < 0 {
		fillLen = 0
	}
	top := border.Render("╭─ ") + dot + border.Render("  "+name+strings.Repeat("─", fillLen)+"╮")
	sb.WriteString(top + "\n")

	// 4 content lines from the tool input.
	lines := toolInputLines(input, 4, contentWidth)
	for _, line := range lines {
		runes := []rune(line)
		if len(runes) > contentWidth {
			runes = runes[:contentWidth]
		}
		padding := strings.Repeat(" ", contentWidth-len(runes))
		sb.WriteString(border.Render("│"))
		sb.WriteString(" " + asstTextStyle.Render(string(runes)) + padding + " ")
		sb.WriteString(border.Render("│") + "\n")
	}

	sb.WriteString(border.Render("╰"+strings.Repeat("─", innerWidth)+"╯") + "\n\n")
}

// toolInputLines parses the JSON tool input and returns up to maxLines of
// "key: value" strings suitable for display inside the activity box.
// Only non-empty lines are returned — no padding.
func toolInputLines(input string, maxLines, width int) []string {
	var lines []string

	var m map[string]json.RawMessage
	if json.Unmarshal([]byte(input), &m) == nil {
		// Emit high-value keys first, then whatever remains.
		priority := []string{"command", "path", "name", "message", "query", "text", "url", "content"}
		seen := make(map[string]bool, len(m))
		for _, key := range priority {
			if len(lines) >= maxLines {
				break
			}
			v, ok := m[key]
			if !ok {
				continue
			}
			seen[key] = true
			var s string
			if json.Unmarshal(v, &s) != nil {
				continue
			}
			line := key + ": " + s
			if r := []rune(line); len(r) > width {
				line = string(r[:width-1]) + "…"
			}
			lines = append(lines, line)
		}
		for key, v := range m {
			if len(lines) >= maxLines || seen[key] {
				continue
			}
			var s string
			if json.Unmarshal(v, &s) != nil {
				continue
			}
			line := key + ": " + s
			if r := []rune(line); len(r) > width {
				line = string(r[:width-1]) + "…"
			}
			lines = append(lines, line)
		}
	}

	return lines
}

