package harness

import (
	"strings"
	"testing"

	"github.com/mattdurham/wllr/sdk"
)

// renderChat builds a ChatView of the given size, populates it with msgs,
// and returns the viewport content string for assertion.
func renderChat(t *testing.T, width, height int, msgs []chatMessage) string {
	t.Helper()
	c := NewChatView(width, height)
	c.messages = msgs
	c.histDirty = true
	c.refreshContent()
	return c.vp.View()
}

// colourOf returns the ANSI colour escape sequence used in s nearest the
// pattern, or "" if pattern not found.  We use this to check which colour
// family a rendered element falls into without hard-coding full ANSI strings.
func containsColour(s, ansiHex string) bool {
	// lipgloss renders hex colours as truecolour ANSI: \x1b[38;2;R;G;Bm
	// We just check the hex string appears somewhere in the raw bytes.
	return strings.Contains(s, ansiHex)
}

// ──────────────────────────────────────────────────────────────
// User message border colour
// ──────────────────────────────────────────────────────────────

func TestChatRender_CurrentUserMsg_HasGreenBorder(t *testing.T) {
	msgs := []chatMessage{
		{role: sdk.RoleUser, content: "hello"},
	}
	out := renderChat(t, 80, 20, msgs)
	// Green border colour: #00AA00 → R=0 G=170 B=0
	if !containsColour(out, "0;170;0") {
		t.Error("current user message should have green border (#00AA00)")
	}
}

func TestChatRender_OldUserMsg_HasGreyBorder(t *testing.T) {
	msgs := []chatMessage{
		{role: sdk.RoleUser, content: "old message"},
		{role: sdk.RoleUser, content: "newer message"}, // makes the first one "old"
	}
	out := renderChat(t, 80, 20, msgs)
	// Old border: #444444 → R=68 G=68 B=68
	if !containsColour(out, "68;68;68") {
		t.Error("old user message should have grey border (#444444)")
	}
	// Current message should still have green border
	if !containsColour(out, "0;170;0") {
		t.Error("current user message should still have green border")
	}
}

// ──────────────────────────────────────────────────────────────
// Assistant message border colour
// ──────────────────────────────────────────────────────────────

func TestChatRender_CurrentAssistantMsg_HasBlueBorder(t *testing.T) {
	msgs := []chatMessage{
		{role: sdk.RoleUser, content: "question"},
		{role: sdk.RoleAssistant, content: "answer"},
	}
	out := renderChat(t, 80, 20, msgs)
	// Blue border: #89CFF0 → R=137 G=207 B=240
	if !containsColour(out, "137;207;240") {
		t.Error("current assistant message should have blue border (#89CFF0)")
	}
}

func TestChatRender_OldAssistantMsg_HasGreyBorder(t *testing.T) {
	msgs := []chatMessage{
		{role: sdk.RoleUser, content: "q1"},
		{role: sdk.RoleAssistant, content: "a1"},
		{role: sdk.RoleUser, content: "q2"}, // makes a1 old
		{role: sdk.RoleAssistant, content: "a2"},
	}
	out := renderChat(t, 80, 20, msgs)
	// Old assistant border: #444444 → 68;68;68
	if !containsColour(out, "68;68;68") {
		t.Error("old assistant message should have grey border (#444444)")
	}
	// Current assistant still blue
	if !containsColour(out, "137;207;240") {
		t.Error("current assistant message should still have blue border")
	}
}

// ──────────────────────────────────────────────────────────────
// Tool call dot colours
// ──────────────────────────────────────────────────────────────

func TestChatRender_ToolCalls_NotRendered(t *testing.T) {
	// Tool calls are completely hidden — only LLM text responses are shown.
	msgs := []chatMessage{
		{role: sdk.RoleUser, content: "do something"},
		{role: "tool", content: `exec: {"command":"ls"}`},
		{role: "tool", content: `read_file: {"path":"/tmp"}`},
	}
	out := renderChat(t, 80, 20, msgs)
	if strings.Contains(out, "↳") || strings.Contains(out, "exec") || strings.Contains(out, "read_file") {
		t.Error("tool calls should not render anything in the chat")
	}
}

// ──────────────────────────────────────────────────────────────
// histDirty caching: cache invalidated when messages change
// ──────────────────────────────────────────────────────────────

func TestChatView_HistDirty_RefreshesOnInvalidate(t *testing.T) {
	c := NewChatView(80, 20)
	c.AddUserMessage("first")
	first := c.vp.View()

	c.AddUserMessage("second")
	second := c.vp.View()

	if first == second {
		t.Error("viewport should update after adding a second message")
	}
	if !strings.Contains(second, "second") {
		t.Error("second message should be visible after adding it")
	}
}

func TestChatView_HistCache_NotRebuiltOnToken(t *testing.T) {
	c := NewChatView(80, 20)
	c.AddUserMessage("question")

	// Simulate streaming: histDirty should be false after messages settle
	c.histDirty = false
	before := c.histContent

	// AppendToken should NOT invalidate history cache (uses c.current path)
	c.AppendToken("hello")
	if c.histDirty {
		t.Error("AppendToken should not set histDirty — it uses c.current path")
	}
	if c.histContent != before {
		t.Error("histContent should be unchanged by AppendToken")
	}
}

func TestChatView_HistCache_InvalidatedOnNewUserMessage(t *testing.T) {
	c := NewChatView(80, 20)
	c.AddUserMessage("first")
	afterFirst := c.histContent

	c.AddUserMessage("second")
	// refreshContent runs synchronously inside AddUserMessage so histDirty
	// is already false again — but histContent must have been rebuilt.
	if c.histContent == afterFirst {
		t.Error("histContent should be rebuilt after AddUserMessage")
	}
	if !strings.Contains(c.histContent, "second") {
		t.Error("rebuilt histContent should include the new message")
	}
}
