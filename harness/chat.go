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
}

var (
	userStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#00FFFF"))
	assistantStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFFFF"))
	systemStyle = lipgloss.NewStyle().
			Italic(true).
			Foreground(lipgloss.Color("#888888"))
	toolBorderStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#555555"))
	toolSuccessStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#00AA00"))
	toolErrorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#CC3333"))
	toolPendingStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#666666"))
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
	c.refreshContent()
}

// AppendToken adds a token to the in-progress assistant message and scrolls to bottom.
// If a tool call recently completed, tokens are routed into that tool box instead.
func (c *ChatView) AppendToken(token string) {
	if c.lastDoneToolID != "" {
		for i := range c.messages {
			if c.messages[i].role == "tool" && c.messages[i].toolID == c.lastDoneToolID {
				c.messages[i].toolResponse += token
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
	c.refreshContent()
}

// AddUserMessage prepends a user message to the history.
func (c *ChatView) AddUserMessage(content string) {
	c.messages = append(c.messages, chatMessage{role: sdk.RoleUser, content: content})
	c.refreshContent()
	c.vp.GotoBottom()
}

// AddNotification appends a system/notification line.
func (c *ChatView) AddNotification(text string) {
	c.messages = append(c.messages, chatMessage{role: "system", content: text})
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
	c.refreshContent()
}

// Clear resets the chat history.
func (c *ChatView) Clear() {
	c.messages = nil
	c.current = ""
	c.lastDoneToolID = ""
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

// View renders the chat content.
func (c ChatView) View() string {
	return c.vp.View()
}

// refreshContent rebuilds the viewport content from messages.
func (c *ChatView) refreshContent() {
	var sb strings.Builder
	for _, m := range c.messages {
		renderMessage(&sb, m, c.width)
	}
	if c.current != "" {
		renderMessage(&sb, chatMessage{role: sdk.RoleAssistant, content: c.current}, c.width)
	}
	c.vp.SetContent(sb.String())
}

func renderMessage(sb *strings.Builder, m chatMessage, width int) {
	const minWidth = 20
	if width < minWidth {
		width = minWidth
	}
	switch m.role {
	case sdk.RoleUser:
		prefix := userStyle.Render("You: ")
		sb.WriteString(prefix)
		sb.WriteString(lipgloss.Wrap(m.content, width-5, ""))
		sb.WriteString("\n\n")
	case sdk.RoleAssistant:
		sb.WriteString(assistantStyle.Render(lipgloss.Wrap(m.content, width, "")))
		sb.WriteString("\n\n")
	case "tool":
		renderToolCall(sb, m, width)
		return // renderToolCall writes its own newlines
	default:
		sb.WriteString(systemStyle.Render(lipgloss.Wrap("» "+m.content, width, "")))
		sb.WriteString("\n\n")
	}
}

func renderToolCall(sb *strings.Builder, m chatMessage, width int) {
	var dot string
	if !m.toolDone {
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
	top := toolBorderStyle.Render("╭─ ") + dot + toolBorderStyle.Render("  "+m.toolName+"  "+preview+fill+"╮")
	sb.WriteString(top)
	sb.WriteString("\n")

	// LLM response lines inside the box (streams in after the tool executes).
	if m.toolResponse != "" {
		contentWidth := innerWidth - 2 // space padding on each side
		if contentWidth < 1 {
			contentWidth = 1
		}
		for _, line := range strings.Split(strings.TrimRight(m.toolResponse, "\n"), "\n") {
			runes := []rune(line)
			if len(runes) > contentWidth {
				runes = runes[:contentWidth]
			}
			padding := strings.Repeat(" ", contentWidth-len(runes))
			sb.WriteString(toolBorderStyle.Render("│"))
			sb.WriteString(" ")
			sb.WriteString(string(runes))
			sb.WriteString(padding)
			sb.WriteString(" ")
			sb.WriteString(toolBorderStyle.Render("│"))
			sb.WriteString("\n")
		}
	}

	// ╰──────────────────────────────╯
	bottom := toolBorderStyle.Render("╰" + strings.Repeat("─", innerWidth) + "╯")
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
