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
