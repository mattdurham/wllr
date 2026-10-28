package harness

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/mattdurham/wllr/modules/extension"
	"github.com/mattdurham/wllr/modules/sdk"
)

func TestBuiltinWASMTranscriptAndStatusline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	h := extension.NewHost(nil)
	t.Cleanup(func() { _ = h.Close(ctx) })
	m := New(nil, "main", h)
	m.width, m.height = 100, 30
	m.live.setModel("test-model")
	h.SetUIBridge(&harnessUIBridge{scene: m.scene, live: m.live, cmds: m.commands})
	for _, name := range []string{"agents", "statusline"} {
		data, err := os.ReadFile("../../cmd/builtins/" + name + ".wasm")
		if os.IsNotExist(err) {
			t.Skipf("run make builtins: %v", err)
		}
		if err != nil {
			t.Fatal(err)
		}
		if err := h.LoadBytes(ctx, name+".wasm", data, true, sdk.PermUI); err != nil {
			t.Fatal(err)
		}
	}
	for _, event := range []struct {
		kind    sdk.EventType
		payload any
	}{
		{sdk.EventSessionStart, sdk.SessionStartPayload{Reason: "new_session"}},
		{sdk.EventBeforeAgentStart, sdk.BeforeAgentStartPayload{AgentID: "main", Prompt: "visible user prompt"}},
		{sdk.EventToken, sdk.TokenPayload{AgentID: "main", Text: "visible assistant reply"}},
	} {
		payload, err := json.Marshal(event.payload)
		if err != nil {
			t.Fatal(err)
		}
		results, err := h.DispatchEvent(ctx, sdk.Event{Type: event.kind, Payload: payload})
		if err != nil {
			t.Fatal(err)
		}
		for _, result := range results {
			if result.Error != "" {
				t.Fatal(result.Error)
			}
		}
	}
	chat := m.scene.Render(wasmChatAreaID, 100)
	for _, text := range []string{"visible user prompt", "visible assistant reply"} {
		if !strings.Contains(ansi.Strip(chat), text) {
			t.Fatalf("missing %q in chat: %q", text, chat)
		}
	}
	status := m.renderStatusLine()
	if !strings.Contains(status, "test-model") {
		t.Fatalf("missing statusline: %q", status)
	}
	if chat == ansi.Strip(chat) {
		t.Fatalf("chat lost colors: %q", chat)
	}
	if status == ansi.Strip(status) {
		t.Fatalf("statusline lost colors: %q", status)
	}
	m.refreshWASMChat()
	view := m.View().Content
	if view == ansi.Strip(view) {
		t.Fatalf("view lost colors: %q", view)
	}
	for _, text := range []string{"visible user prompt", "visible assistant reply", "test-model"} {
		if !strings.Contains(ansi.Strip(view), text) {
			t.Fatalf("view missing %q: %q", text, view)
		}
	}
}
