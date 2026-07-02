package harness

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mattdurham/wllr/modules/sdk"
)

func BenchmarkScene_RenderAppendTextNode(b *testing.B) {
	scene := NewSceneRenderer()
	if err := scene.CreateArea(sdk.UIArea{ID: wasmChatAreaID, Placement: sdk.UIAreaMain}); err != nil {
		b.Fatal(err)
	}
	text := strings.Repeat("streaming assistant text ", 200)
	appendText := " appended token batch"
	if err := scene.ApplyPatch(sdk.UIPatchParams{Area: wasmChatAreaID, Ops: []sdk.UIPatchOp{
		{Op: sdk.UIOpSetRoot, Node: &sdk.UINode{
			ID:    "a1",
			Type:  sdk.UINodeText,
			Text:  text + appendText,
			Props: &sdk.UIProps{Border: "rounded", Padding: []int{0, 1}, Width: "fill", Wrap: true},
		}},
	}}); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, ok := scene.RenderAppendTextNode(wasmChatAreaID, "a1", 120, appendText); !ok {
			b.Fatal("node not found")
		}
	}
}

func BenchmarkModel_RefreshWASMChatAppend_1000Lines(b *testing.B) {
	m := newBenchmarkWASMChatModel(b, 1000)
	baseContent := m.chat.externalContent
	appendText := " appended token batch"
	if err := m.scene.ApplyPatch(sdk.UIPatchParams{Area: wasmChatAreaID, Ops: []sdk.UIPatchOp{
		{Op: sdk.UIOpAppendText, ID: "assistant-tail", Text: appendText},
	}}); err != nil {
		b.Fatal(err)
	}
	m.chatAppendID = "assistant-tail"
	m.chatAppendText = appendText

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.chat.externalContent = baseContent
		m.chatAppendID = "assistant-tail"
		m.chatAppendText = appendText
		if !m.refreshWASMChatAppend() {
			b.Fatal("append refresh fell back")
		}
	}
}

func BenchmarkModel_RefreshWASMChat_Full_1000Lines(b *testing.B) {
	m := newBenchmarkWASMChatModel(b, 1000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.refreshWASMChat()
	}
}

func newBenchmarkWASMChatModel(tb testing.TB, lines int) Model {
	tb.Helper()
	m := New(nil, "main", nil)
	m.width = 120
	m.height = 40
	m.scene = NewSceneRenderer()
	if err := m.scene.CreateArea(sdk.UIArea{ID: wasmChatAreaID, Placement: sdk.UIAreaMain}); err != nil {
		tb.Fatal(err)
	}
	children := make([]sdk.UINode, 0, lines+1)
	for i := 0; i < lines; i++ {
		children = append(children, sdk.UINode{
			ID:   fmt.Sprintf("msg-%04d", i),
			Type: sdk.UINodeText,
			Text: fmt.Sprintf("transcript line %04d with enough content to measure wrapping and viewport replacement", i),
		})
	}
	children = append(children, sdk.UINode{
		ID:    "assistant-tail",
		Type:  sdk.UINodeText,
		Text:  strings.Repeat("assistant tail ", 50),
		Props: &sdk.UIProps{Border: "rounded", Padding: []int{0, 1}, Width: "fill", Wrap: true},
	})
	if err := m.scene.ApplyPatch(sdk.UIPatchParams{Area: wasmChatAreaID, Ops: []sdk.UIPatchOp{
		{Op: sdk.UIOpSetRoot, Node: &sdk.UINode{ID: "chat-root", Type: sdk.UINodeVStack, Children: children}},
	}}); err != nil {
		tb.Fatal(err)
	}
	m.refreshWASMChat()
	return m
}
