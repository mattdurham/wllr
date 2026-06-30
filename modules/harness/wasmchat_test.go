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
	m.wasmChat = true
	m.width = 60
	m.scene = newSceneWithChat(t, "hello from wasm")

	m.refreshWASMChat()

	if !m.chat.ExternalMode() {
		t.Fatal("chat should be in external mode after refresh")
	}
	if !strings.Contains(m.chat.externalContent, "hello from wasm") {
		t.Fatalf("external content missing scene text: %q", m.chat.externalContent)
	}
}

func TestRefreshWASMChat_DisabledIsNoOp(t *testing.T) {
	m := New(nil, "main", nil)
	m.wasmChat = false
	m.width = 60
	m.scene = newSceneWithChat(t, "should not appear")

	m.refreshWASMChat()

	if m.chat.ExternalMode() {
		t.Fatal("chat must not enter external mode when wasmChat is disabled")
	}
}

func TestRefreshWASMChat_NoAreaIsNoOp(t *testing.T) {
	m := New(nil, "main", nil)
	m.wasmChat = true
	m.width = 60
	// scene exists but has no "chat" area.

	m.refreshWASMChat()

	if m.chat.ExternalMode() {
		t.Fatal("chat must not enter external mode when the chat area is absent")
	}
}

func TestRenderScenes_SkipsChatAreaInWASMMode(t *testing.T) {
	m := New(nil, "main", nil)
	m.wasmChat = true
	m.width = 60
	m.scene = newSceneWithChat(t, "transcript text")

	// The transcript area is rendered inside the chat viewport, so renderScenes
	// must not also stack it below the chat.
	if got := m.renderScenes(); strings.Contains(got, "transcript text") {
		t.Fatalf("renderScenes must skip the chat area in WASM mode: %q", got)
	}
}
