package harness

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattdurham/wllr/modules/extension"
	"github.com/mattdurham/wllr/modules/sdk"
)

// newSceneWithChat returns a SceneRenderer with a "chat" area holding a single
// text node carrying the given text.
func newSceneWithChat(t *testing.T, text string) *SceneRenderer {
	t.Helper()
	s := NewSceneRenderer()
	if err := s.CreateArea(sdk.UIArea{ID: wasmChatAreaID, Placement: sdk.UIAreaMain}); err != nil {
		t.Fatalf("create area: %v", err)
	}
	if err := s.ApplyPatch(sdk.UIPatchParams{Area: wasmChatAreaID, Ops: []sdk.UIPatchOp{
		{Op: sdk.UIOpSetRoot, Node: &sdk.UINode{ID: "chat-root", Type: sdk.UINodeText, Text: text}},
	}}); err != nil {
		t.Fatalf("patch: %v", err)
	}
	return s
}

func TestRefreshWASMChat_FeedsSceneIntoViewport(t *testing.T) {
	m := New(nil, "main", nil)
	m.width = 60
	m.scene = newSceneWithChat(t, "hello from wasm")

	m.refreshWASMChat()

	if !strings.Contains(m.chat.externalContent, "hello from wasm") {
		t.Fatalf("viewport content missing scene text: %q", m.chat.externalContent)
	}
}

func TestRefreshWASMChat_NoAreaIsNoOp(t *testing.T) {
	m := New(nil, "main", nil)
	m.width = 60
	// scene exists but has no "chat" area.

	m.refreshWASMChat()

	if m.chat.externalContent != "" {
		t.Fatalf("expected empty content when chat area is absent, got %q", m.chat.externalContent)
	}
}

func TestSceneDirty_StatuslineDoesNotRefreshChat(t *testing.T) {
	m := New(nil, "main", nil)
	m.width = 60
	m.scene = newSceneWithChat(t, "new chat content")
	m.chat.SetExternalContent("old chat content")

	next, _ := m.Update(sceneDirtyMsg{Area: statuslineAreaID})
	m = next.(Model)

	if got := m.chat.externalContent; got != "old chat content" {
		t.Fatalf("statusline dirty should not refresh chat viewport, got %q", got)
	}
}

func TestSceneDirty_ChatRefreshesChat(t *testing.T) {
	m := New(nil, "main", nil)
	m.width = 60
	m.scene = newSceneWithChat(t, "new chat content")
	m.chat.SetExternalContent("old chat content")

	next, _ := m.Update(sceneDirtyMsg{Area: wasmChatAreaID})
	m = next.(Model)

	if !strings.Contains(m.chat.externalContent, "new chat content") {
		t.Fatalf("chat dirty should refresh chat viewport, got %q", m.chat.externalContent)
	}
}

func TestSceneDirty_AppendOnlyChatCoalescesRefresh(t *testing.T) {
	m := New(nil, "main", nil)
	m.width = 60
	m.scene = newSceneWithChat(t, "new chat content")
	m.chat.SetExternalContent("old chat content")

	next, cmd := m.Update(sceneDirtyMsg{Area: wasmChatAreaID, AppendOnly: true})
	m = next.(Model)

	if cmd == nil {
		t.Fatal("append-only chat dirty should schedule a delayed refresh")
	}
	if got := m.chat.externalContent; got != "old chat content" {
		t.Fatalf("append-only chat dirty should not refresh immediately, got %q", got)
	}
	if !m.chatAppendDirty || !m.chatAppendRefreshScheduled {
		t.Fatalf(
			"append-only chat dirty should mark pending refresh: dirty=%v scheduled=%v",
			m.chatAppendDirty,
			m.chatAppendRefreshScheduled,
		)
	}

	next, _ = m.Update(chatAppendRefreshMsg{})
	m = next.(Model)

	if !strings.Contains(m.chat.externalContent, "new chat content") {
		t.Fatalf("delayed append refresh should refresh chat viewport, got %q", m.chat.externalContent)
	}
	if m.chatAppendDirty || m.chatAppendRefreshScheduled {
		t.Fatalf(
			"delayed append refresh should clear pending flags: dirty=%v scheduled=%v",
			m.chatAppendDirty,
			m.chatAppendRefreshScheduled,
		)
	}
}

func TestSceneDirty_AppendOnlyChatUsesFastSuffixRefresh(t *testing.T) {
	m := New(nil, "main", nil)
	m.width = 60
	m.scene = NewSceneRenderer()
	if err := m.scene.CreateArea(sdk.UIArea{ID: wasmChatAreaID, Placement: sdk.UIAreaMain}); err != nil {
		t.Fatalf("create area: %v", err)
	}
	if err := m.scene.ApplyPatch(sdk.UIPatchParams{Area: wasmChatAreaID, Ops: []sdk.UIPatchOp{
		{Op: sdk.UIOpSetRoot, Node: &sdk.UINode{ID: "chat-root", Type: sdk.UINodeVStack, Children: []sdk.UINode{
			{ID: "u1", Type: sdk.UINodeText, Text: "prompt", Props: &sdk.UIProps{Border: "rounded"}},
			{ID: "a1", Type: sdk.UINodeText, Text: "", Props: &sdk.UIProps{Border: "rounded", Wrap: true}},
		}}},
	}}); err != nil {
		t.Fatalf("set root: %v", err)
	}
	m.refreshWASMChat()
	before := m.chat.externalContent

	if err := m.scene.ApplyPatch(sdk.UIPatchParams{Area: wasmChatAreaID, Ops: []sdk.UIPatchOp{
		{Op: sdk.UIOpAppendText, ID: "a1", Text: "hello"},
	}}); err != nil {
		t.Fatalf("append: %v", err)
	}
	next, _ := m.Update(sceneDirtyMsg{Area: wasmChatAreaID, AppendOnly: true, AppendID: "a1", AppendText: "hello"})
	m = next.(Model)
	next, _ = m.Update(chatAppendRefreshMsg{})
	m = next.(Model)

	if !strings.Contains(m.chat.externalContent, "hello") {
		t.Fatalf("fast append refresh should add appended text, got %q", m.chat.externalContent)
	}
	if !strings.Contains(m.chat.externalContent, "prompt") {
		t.Fatalf("fast append refresh should preserve previous transcript content, got %q", m.chat.externalContent)
	}
	if m.chat.externalContent == before {
		t.Fatal("fast append refresh should update viewport content")
	}
}

func TestResetChatArea_EmptiesTranscript(t *testing.T) {
	m := New(nil, "main", nil)
	m.width = 60
	m.scene = newSceneWithChat(t, "to be cleared")

	m.resetChatArea()

	if got := m.scene.Render(wasmChatAreaID, 60); got != "" {
		t.Fatalf("resetChatArea should empty the transcript, got %q", got)
	}
}

// ResetHistoryMsg must both wipe the transcript and schedule a rebuild: the
// replay lands in agent context, and without the rebuild the chat would go
// blank, making the restore look like it did nothing.
func TestResetHistoryMsg_ResetsAndDispatchesRebuild(t *testing.T) {
	m := New(newTestPool(), "main", nil)
	m.width = 60
	m.scene = newSceneWithChat(t, "stale transcript")
	m.extHost = extension.NewHost(nil)
	ctx := context.Background()
	defer func() { _ = m.extHost.Close(ctx) }()

	next, cmd := m.Update(ResetHistoryMsg{Messages: []sdk.Message{
		{Role: sdk.RoleUser, Content: "restored question"},
	}})
	m = next.(Model)

	if cmd == nil {
		t.Fatal("ResetHistoryMsg should return a rebuild-dispatch cmd")
	}
	if got := m.scene.Render(wasmChatAreaID, 60); got != "" {
		t.Fatalf("ResetHistoryMsg should empty the transcript first, got %q", got)
	}

	res := cmd()
	evtResult, ok := res.(ExtensionEventResultMsg)
	if !ok {
		t.Fatalf("rebuild cmd returned %T, want ExtensionEventResultMsg", res)
	}
	if evtResult.Err != nil {
		t.Fatalf("rebuild dispatch error: %v", evtResult.Err)
	}
}

// The rebuild event payload is the contract the transcript-owning extension
// (agents) registers against; pin the name and argument.
func TestTranscriptRebuildEvent_Payload(t *testing.T) {
	evt := transcriptRebuildEvent("main")
	if evt.Type != sdk.EventOnCommand {
		t.Fatalf("event type = %q, want %q", evt.Type, sdk.EventOnCommand)
	}
	var p sdk.OnCommandPayload
	if err := json.Unmarshal(evt.Payload, &p); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if p.Name != TranscriptRebuildCallback || len(p.Args) != 1 || p.Args[0] != "main" {
		t.Fatalf("payload = %+v, want name %q args [main]", p, TranscriptRebuildCallback)
	}
}

// An empty agent id means the root agent. Focus reconciliation rebuilds through
// this empty-arg form after the focused sub-agent closes, so the extension can
// tell it apart from a named-agent rebuild.
func TestTranscriptRebuildEvent_EmptyArgMeansRoot(t *testing.T) {
	evt := transcriptRebuildEvent("")
	var p sdk.OnCommandPayload
	if err := json.Unmarshal(evt.Payload, &p); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if p.Name != TranscriptRebuildCallback || len(p.Args) != 1 || p.Args[0] != "" {
		t.Fatalf("payload = %+v, want name %q args [\"\"]", p, TranscriptRebuildCallback)
	}
}

// Without an extension host there is nothing to rebuild through; the reset
// must still happen and no cmd may be returned.
func TestResetHistoryMsg_NilHost_ResetsOnly(t *testing.T) {
	m := New(newTestPool(), "main", nil)
	m.width = 60
	m.scene = newSceneWithChat(t, "stale transcript")

	next, cmd := m.Update(ResetHistoryMsg{})
	m = next.(Model)

	if cmd != nil {
		t.Error("ResetHistoryMsg with nil extHost should return nil cmd")
	}
	if got := m.scene.Render(wasmChatAreaID, 60); got != "" {
		t.Fatalf("ResetHistoryMsg should still empty the transcript, got %q", got)
	}
}

func TestRenderScenes_SkipsChatArea(t *testing.T) {
	m := New(nil, "main", nil)
	m.width = 60
	m.scene = newSceneWithChat(t, "transcript text")

	// The transcript area is rendered inside the chat viewport, so renderScenes
	// must not also stack it below the chat.
	if got := m.renderScenes(); strings.Contains(got, "transcript text") {
		t.Fatalf("renderScenes must skip the chat area: %q", got)
	}
}

// Issue #45 regression: no line of the composed view may be wider than the
// terminal. An over-wide line is hard-wrapped by the terminal, which lands the
// wrapped remainder beside the following row's border and desynchronises the
// whole repaint into doubled borders (`││`) and fused corners (`────╮│`).
//
// The transcript message box itself is correct at every width; the panes that
// pad content by hand were not, because tool previews were wrapped by rune
// count rather than display width.
func TestViewNeverExceedsTerminalWidth(t *testing.T) {
	const prose = "Good question — let me ground the answer in the actual timing " +
		"constants and config rather than guessing, by checking the acceptance " +
		"package's poll intervals and the gate `invocation`."
	widePreview := `{"command":"echo '` + strings.Repeat("🎉", 40) + `' "}`

	for _, width := range []int{20, 40, 60, 79, 80, 81, 83, 100, 120, 200} {
		m := New(nil, "main", nil)
		next, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: 30})
		m = next.(Model)

		m.scene = NewSceneRenderer()
		if err := m.scene.CreateArea(sdk.UIArea{ID: wasmChatAreaID, Placement: sdk.UIAreaMain}); err != nil {
			t.Fatalf("create area: %v", err)
		}
		if err := m.scene.ApplyPatch(sdk.UIPatchParams{Area: wasmChatAreaID, Ops: []sdk.UIPatchOp{
			{Op: sdk.UIOpSetRoot, Node: &sdk.UINode{ID: wasmChatRootID, Type: sdk.UINodeVStack, Children: []sdk.UINode{
				{ID: "a1", Type: sdk.UINodeText, Text: prose, Props: transcriptMessageBoxProps("accent")},
			}}},
		}}); err != nil {
			t.Fatalf("patch: %v", err)
		}
		m.chat.AddToolCall("t1", "main", "exec", widePreview)
		m.refreshWASMChat()

		for i, line := range strings.Split(m.View().Content, "\n") {
			if got := lipgloss.Width(line); got > width {
				t.Fatalf("width %d: view line %d is %d columns, want <= %d: %q",
					width, i, got, width, ansi.Strip(line))
			}
		}
	}
}

// transcriptMessageBoxProps mirrors the agents extension's message-box props
// (extensions/agents/chat.go messageBoxProps). Keeping a copy here pins the
// render contract the transcript depends on; if the extension drifts, this
// test's mirror is the reminder to re-check the visual result.
func transcriptMessageBoxProps(fg string) *sdk.UIProps {
	return &sdk.UIProps{
		Border: "rounded", Fg: fg, Padding: []int{0, 1},
		Margin: []int{0, 0, 1, 0}, Width: "fill", Wrap: true,
	}
}

// Consecutive transcript messages must render with a blank line between their
// boxes. Without the bottom margin, adjacent boxes touch (bottom border
// immediately followed by top border) and a replayed history reads as one
// solid wall of text.
func TestTranscriptMessageBoxesRenderSeparated(t *testing.T) {
	s := NewSceneRenderer()
	if err := s.CreateArea(sdk.UIArea{ID: wasmChatAreaID, Placement: sdk.UIAreaMain}); err != nil {
		t.Fatalf("create area: %v", err)
	}
	ops := make([]sdk.UIPatchOp, 0, 5)
	ops = append(ops, sdk.UIPatchOp{Op: sdk.UIOpSetRoot, Node: &sdk.UINode{ID: wasmChatRootID, Type: sdk.UINodeVStack}})
	for i, id := range []string{"u1", "a1", "u2", "a2"} {
		ops = append(ops, sdk.UIPatchOp{Op: sdk.UIOpInsert, Parent: wasmChatRootID, Node: &sdk.UINode{
			ID:    id,
			Type:  sdk.UINodeText,
			Text:  fmt.Sprintf("message %d", i),
			Props: transcriptMessageBoxProps("success"),
		}})
	}
	if err := s.ApplyPatch(sdk.UIPatchParams{Area: wasmChatAreaID, Ops: ops}); err != nil {
		t.Fatalf("patch: %v", err)
	}

	lines := strings.Split(s.Render(wasmChatAreaID, 80), "\n")
	boxesSeen := 0
	for i, line := range lines {
		if !strings.Contains(line, "╭") {
			continue
		}
		boxesSeen++
		if boxesSeen == 1 {
			continue // the first box may sit flush at the top
		}
		// Every later box must have a blank line above it and a box bottom
		// border above that.
		if i == 0 || strings.TrimSpace(lines[i-1]) != "" {
			t.Fatalf("box %d has no blank line above it: %q", boxesSeen, lines[i-1])
		}
		if i < 2 || !strings.Contains(lines[i-2], "╰") {
			t.Fatalf("blank line above box %d is not preceded by a box bottom border: %q", boxesSeen, lines[i-2])
		}
	}
	if boxesSeen < 2 {
		t.Fatalf("expected at least 2 boxes in render, saw %d:\n%s", boxesSeen, s.Render(wasmChatAreaID, 80))
	}
}
