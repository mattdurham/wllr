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
	vp       viewport.Model
	messages []chatMessage
	current  string // current in-progress assistant message
	width    int
	height   int
	// lastDoneToolID is set when a tool call completes; subsequent tokens
	// are routed into that tool box until the next tool call or FinalizeMessage.
	lastDoneToolID string

	// histContent caches the rendered historical messages.
	// Rebuilt only when messages change, not on every streaming token.
	histContent string
	histDirty   bool
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

// SetSize updates the viewport dimensions.
func (c *ChatView) SetSize(width, height int) {
	c.width = width
	c.height = height
	c.vp.SetWidth(width)
	c.vp.SetHeight(height)
	c.invalidateHistory()
	c.refreshContent()
}

// AppendToken adds a token to the in-progress assistant message and scrolls to bottom.
// If a tool call recently completed, tokens are routed into that tool box instead.
func (c *ChatView) AppendToken(token string) {
	if c.lastDoneToolID != "" {
		for i := range c.messages {
			if c.messages[i].role == "tool" && c.messages[i].toolID == c.lastDoneToolID {
				c.messages[i].toolResponse += token
				// toolResponse is part of messages, so the history cache is stale.
				c.invalidateHistory()
				c.refreshContent()
				c.vp.GotoBottom()
				return
			}
		}
	}
	c.current += token
	c.refreshContent()
	c.vp.GotoBottom()
}

// FinalizeMessage seals the in-progress message and resets tool routing.
func (c *ChatView) FinalizeMessage() {
	c.lastDoneToolID = ""
	if c.current == "" {
		return
	}
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

// AddNotification appends a system/notification line.
func (c *ChatView) AddNotification(text string) {
	c.messages = append(c.messages, chatMessage{role: "system", content: text})
	c.invalidateHistory()
	c.refreshContent()
	c.vp.GotoBottom()
}

// AddToolCall appends a pending tool call entry to the chat history.
// Any in-progress streaming text is sealed first so it appears before the box.
func (c *ChatView) AddToolCall(id, toolName, input string) {
	if c.current != "" {
		c.messages = append(c.messages, chatMessage{role: sdk.RoleAssistant, content: c.current})
		c.current = ""
	}
	c.lastDoneToolID = ""
	c.messages = append(c.messages, chatMessage{
		role:      "tool",
		toolID:    id,
		toolName:  toolName,
		toolInput: input,
	})
	c.invalidateHistory()
	c.refreshContent()
	c.vp.GotoBottom()
}

// UpdateToolCall marks an existing tool call as done and begins routing
// subsequent tokens into its box.
func (c *ChatView) UpdateToolCall(id string, isError bool, output string) {
	for i := range c.messages {
		if c.messages[i].role == "tool" && c.messages[i].toolID == id {
			c.messages[i].toolDone = true
			c.messages[i].toolError = isError
			c.messages[i].toolOutput = output // stored but not rendered
			break
		}
	}
	c.lastDoneToolID = id
	c.invalidateHistory()
	c.refreshContent()
	c.vp.GotoBottom()
}

// Clear resets the chat history.
func (c *ChatView) Clear() {
	c.messages = nil
	c.current = ""
	c.lastDoneToolID = ""
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
			if c.messages[i].role == "tool" {
				lastToolIdx = i
				break
			}
		}

		var sb strings.Builder
		for i, m := range c.messages {
			var old bool
			if m.role == "tool" {
				// Only the most recent tool call keeps color; all others go grey.
				old = i != lastToolIdx
			} else {
				old = i < recentStart
			}
			renderMessage(&sb, m, c.width, old)
		}
		c.histContent = sb.String()
		c.histDirty = false
	}

	if c.current != "" {
		var sb strings.Builder
		sb.WriteString(c.histContent)
		renderMessage(&sb, chatMessage{role: sdk.RoleAssistant, content: c.current}, c.width, false)
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
	case "tool":
		renderToolCall(sb, m, width, old)
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

func renderToolCall(sb *strings.Builder, m chatMessage, width int, old bool) {
	border := toolBorderStyle
	var dot string
	if old {
		border = toolBorderOldStyle
		dot = toolDotOldStyle.Render("●")
	} else if !m.toolDone {
		dot = toolPendingStyle.Render("◌")
	} else if m.toolError {
		dot = toolErrorStyle.Render("●")
	} else {
		dot = toolSuccessStyle.Render("●")
	}

	preview := toolInputPreview(m.toolInput)

	if width < 14 {
		width = 14
	}
	innerWidth := width - 2 // subtract ╭ and ╮

	// Visible rune count in the label: "─ " + dot(1) + "  " + toolName + "  " + preview
	labelRunes := 2 + 1 + 2 + len([]rune(m.toolName)) + 2 + len([]rune(preview))
	fillLen := innerWidth - labelRunes
	if fillLen < 0 {
		fillLen = 0
	}
	fill := strings.Repeat("─", fillLen)

	// ╭─ ◌  toolname  preview──────╮
	top := border.Render("╭─ ") + dot + border.Render("  "+m.toolName+"  "+preview+fill+"╮")
	sb.WriteString(top)
	sb.WriteString("\n")

	// LLM response lines inside the box (streams in after the tool executes).
	if m.toolResponse != "" {
		contentWidth := innerWidth - 2 // space padding on each side
		if contentWidth < 1 {
			contentWidth = 1
		}
		textSt := asstTextStyle
		if old {
			textSt = asstTextOldStyle
		}
		for _, line := range strings.Split(strings.TrimRight(m.toolResponse, "\n"), "\n") {
			runes := []rune(line)
			if len(runes) > contentWidth {
				runes = runes[:contentWidth]
			}
			padding := strings.Repeat(" ", contentWidth-len(runes))
			sb.WriteString(border.Render("│") + " ")
			sb.WriteString(textSt.Render(string(runes)))
			sb.WriteString(padding + " ")
			sb.WriteString(border.Render("│") + "\n")
		}
	}

	// ╰──────────────────────────────╯
	bottom := border.Render("╰" + strings.Repeat("─", innerWidth) + "╯")
	sb.WriteString(bottom)
	sb.WriteString("\n\n")
}

func toolInputPreview(input string) string {
	var m map[string]json.RawMessage
	if json.Unmarshal([]byte(input), &m) == nil {
		for _, key := range []string{"command", "path", "name", "query", "text", "content", "url"} {
			if v, ok := m[key]; ok {
				var s string
				if json.Unmarshal(v, &s) == nil && s != "" {
					if len([]rune(s)) > 20 {
						return string([]rune(s)[:20]) + "…"
					}
					return s
				}
			}
		}
	}
	r := []rune(input)
	if len(r) > 20 {
		return string(r[:20]) + "…"
	}
	return input
}
