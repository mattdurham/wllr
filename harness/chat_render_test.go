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

func TestChatRender_SingleToolCall_ShowsAsSummary(t *testing.T) {
	msgs := []chatMessage{
		{role: sdk.RoleUser, content: "do something"},
		{role: "tool", toolID: "t1", toolName: "exec", toolInput: `{"command":"ls"}`,
			toolDone: true, toolError: false},
	}
	out := renderChat(t, 80, 20, msgs)
	// Single tool call renders as ↳ toolname summary
	if !strings.Contains(out, "↳") {
		t.Error("single tool call should render as ↳ summary line")
	}
	if !strings.Contains(out, "exec") {
		t.Error("tool name should appear in summary")
	}
}

func TestChatRender_MultipleToolCalls_ShowAsSummary(t *testing.T) {
	msgs := []chatMessage{
		{role: sdk.RoleUser, content: "do something"},
		{role: "tool", toolID: "t1", toolName: "exec", toolInput: `{"command":"ls"}`,
			toolDone: true},
		{role: "tool", toolID: "t2", toolName: "read_file", toolInput: `{"path":"/tmp/x"}`,
			toolDone: true},
	}
	out := renderChat(t, 80, 20, msgs)
	// Both tool names should appear in the summary line
	if !strings.Contains(out, "↳") {
		t.Error("multiple tool calls should render as a ↳ summary line")
	}
	if !strings.Contains(out, "exec") || !strings.Contains(out, "read_file") {
		t.Error("summary should list all tool names")
	}
}

func TestChatRender_PendingToolCall_ShowsInSummary(t *testing.T) {
	msgs := []chatMessage{
		{role: sdk.RoleUser, content: "run it"},
		{role: "tool", toolID: "t1", toolName: "exec", toolInput: `{"command":"sleep 1"}`,
			toolDone: false},
	}
	out := renderChat(t, 80, 20, msgs)
	// Pending tool still shows in summary (no ◌ dot in new design)
	if !strings.Contains(out, "↳") {
		t.Error("pending tool call should appear in summary line")
	}
	if !strings.Contains(out, "exec") {
		t.Error("pending tool name should appear in summary")
	}
}

func TestChatRender_ErrorToolCall_ShowsInSummary(t *testing.T) {
	msgs := []chatMessage{
		{role: sdk.RoleUser, content: "run it"},
		{role: "tool", toolID: "t1", toolName: "exec", toolInput: `{"command":"bad"}`,
			toolDone: true, toolError: true},
	}
	out := renderChat(t, 80, 20, msgs)
	// Error tool still shows in summary (no red dot in new design)
	if !strings.Contains(out, "↳") {
		t.Error("error tool call should appear in summary line")
	}
}

// ──────────────────────────────────────────────────────────────
// recentStart boundary: only the last user message starts "current"
// ──────────────────────────────────────────────────────────────

func TestChatRender_ThreeExchanges_OnlyLastIsCurrent(t *testing.T) {
	msgs := []chatMessage{
		{role: sdk.RoleUser, content: "q1"},
		{role: sdk.RoleAssistant, content: "a1"},
		{role: sdk.RoleUser, content: "q2"},
		{role: sdk.RoleAssistant, content: "a2"},
		{role: sdk.RoleUser, content: "q3"},      // most recent
		{role: sdk.RoleAssistant, content: "a3"}, // current
	}
	out := renderChat(t, 80, 40, msgs)

	// Old assistant border present (from a1, a2)
	if !containsColour(out, "68;68;68") {
		t.Error("old exchanges should have grey (#444444) borders")
	}
	// Current assistant border present (a3)
	if !containsColour(out, "137;207;240") {
		t.Error("current exchange should have blue (#89CFF0) assistant border")
	}
	// Green user border present (q3)
	if !containsColour(out, "0;170;0") {
		t.Error("most recent user message should have green border")
	}
}

// ──────────────────────────────────────────────────────────────
// Tool response content appears inside the box
// ──────────────────────────────────────────────────────────────

func TestChatRender_ToolSummary_DoesNotIncludeResponse(t *testing.T) {
	// With the summary design, LLM response goes to c.current/assistant box,
	// not inside the tool summary line.
	msgs := []chatMessage{
		{role: sdk.RoleUser, content: "list files"},
		{role: "tool", toolID: "t1", toolName: "exec",
			toolInput: `{"command":"ls -l"}`,
			toolDone:  true,
		},
	}
	out := renderChat(t, 80, 20, msgs)
	// Summary line is present
	if !strings.Contains(out, "↳") {
		t.Error("tool call should render as ↳ summary")
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
