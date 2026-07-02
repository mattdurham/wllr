package harness

import (
	"strings"
	"testing"

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
		t.Fatalf("append-only chat dirty should mark pending refresh: dirty=%v scheduled=%v", m.chatAppendDirty, m.chatAppendRefreshScheduled)
	}

	next, _ = m.Update(chatAppendRefreshMsg{})
	m = next.(Model)

	if !strings.Contains(m.chat.externalContent, "new chat content") {
		t.Fatalf("delayed append refresh should refresh chat viewport, got %q", m.chat.externalContent)
	}
	if m.chatAppendDirty || m.chatAppendRefreshScheduled {
		t.Fatalf("delayed append refresh should clear pending flags: dirty=%v scheduled=%v", m.chatAppendDirty, m.chatAppendRefreshScheduled)
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
