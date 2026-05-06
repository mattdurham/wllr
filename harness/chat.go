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
	toolID    string
	toolName  string
	toolInput string
	toolDone  bool
	toolError bool
}

// ChatView renders the conversation history in a scrollable viewport.
type ChatView struct {
	vp       viewport.Model
	messages []chatMessage
	current  string // current in-progress assistant message
	width    int
	height   int
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
func (c *ChatView) AppendToken(token string) {
	c.current += token
	c.refreshContent()
	c.vp.GotoBottom()
}

// FinalizeMessage seals the in-progress message and adds it to the history.
func (c *ChatView) FinalizeMessage() {
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
func (c *ChatView) AddToolCall(id, toolName, input string) {
	c.messages = append(c.messages, chatMessage{
		role:      "tool",
		toolID:    id,
		toolName:  toolName,
		toolInput: input,
	})
	c.refreshContent()
	c.vp.GotoBottom()
}

// UpdateToolCall marks an existing tool call entry as done (success or error).
func (c *ChatView) UpdateToolCall(id string, isError bool) {
	for i := range c.messages {
		if c.messages[i].role == "tool" && c.messages[i].toolID == id {
			c.messages[i].toolDone = true
			c.messages[i].toolError = isError
			break
		}
	}
	c.refreshContent()
}

// Clear resets the chat history.
func (c *ChatView) Clear() {
	c.messages = nil
	c.current = ""
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
		prefix := assistantStyle.Render("wllr: ")
		sb.WriteString(prefix)
		sb.WriteString(lipgloss.Wrap(m.content, width-6, ""))
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
	// ╰──────────────────────────────╯
	bottom := toolBorderStyle.Render("╰" + strings.Repeat("─", innerWidth) + "╯")

	sb.WriteString(top)
	sb.WriteString("\n")
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
