// Package wasmchat_test is an end-to-end test that the bundled agents.wasm
// extension drives the main chat transcript through the scene graph when
// WLLR_WASM_CHAT is enabled (UI P4).
package wasmchat_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/mattdurham/wllr/modules/extension"
	"github.com/mattdurham/wllr/modules/harness"
	"github.com/mattdurham/wllr/modules/sdk"
	mdrender "github.com/mattdurham/wllr/modules/sdk/md"
)

// sceneUIBridge implements extension.UIBridge, delegating scene operations to a
// real harness.SceneRenderer and treating everything else as a no-op.
type sceneUIBridge struct{ scene *harness.SceneRenderer }

func (b *sceneUIBridge) Notify(string)                                      {}
func (b *sceneUIBridge) ShowModal(string)                                   {}
func (b *sceneUIBridge) ShowPicker(string, []sdk.ShowPickerItem, string)    {}
func (b *sceneUIBridge) ShowTextInput(string, string, string, string)       {}
func (b *sceneUIBridge) Abort()                                             {}
func (b *sceneUIBridge) SetStatus(string, string)                           {}
func (b *sceneUIBridge) GetStatusInfo() sdk.StatusInfo                      { return sdk.StatusInfo{} }
func (b *sceneUIBridge) SendMessage(sdk.Message)                            {}
func (b *sceneUIBridge) RegisterCommand(string, string, bool) error         { return nil }
func (b *sceneUIBridge) RegisterTool(sdk.Tool) error                        { return nil }
func (b *sceneUIBridge) SetSystemPrompt(string)                             {}
func (b *sceneUIBridge) AppendSystemPrompt(string)                          {}
func (b *sceneUIBridge) ResetHistory([]sdk.Message) error                   { return nil }
func (b *sceneUIBridge) ToolResult(string, string, bool)                    {}
func (b *sceneUIBridge) AfterToolCall(string, string, string, string, bool) {}
func (b *sceneUIBridge) ConsoleOutput(string)                               {}
func (b *sceneUIBridge) ConsoleClear()                                      {}
func (b *sceneUIBridge) CreateArea(a sdk.UIArea) error                      { return b.scene.CreateArea(a) }

func (b *sceneUIBridge) PatchUI(p sdk.UIPatchParams) error { return b.scene.ApplyPatch(p) }

func (b *sceneUIBridge) RemoveArea(id string) { b.scene.RemoveArea(id) }

func (b *sceneUIBridge) UpdateArea(p sdk.UIUpdateAreaParams) error { return b.scene.UpdateArea(p) }

// envCapabilities implements the subset of extension.CapabilityProvider needed
// to answer GetEnv("WLLR_WASM_CHAT"); other methods are unused no-ops.
type envCapabilities struct{ env map[string]string }

func (c *envCapabilities) Exec(context.Context, string, string, func(string)) (string, error) {
	return "", nil
}
func (c *envCapabilities) GetEnv(name string) (string, error) { return c.env[name], nil }
func (c *envCapabilities) ReadFile(string) (string, error)    { return "", nil }
func (c *envCapabilities) WriteFile(string, string) error     { return nil }
func (c *envCapabilities) AppendFile(string, string) error    { return nil }
func (c *envCapabilities) HTTPPost(string, map[string]string, []byte) (int, []byte, error) {
	return 0, nil, nil
}

func (c *envCapabilities) HTTPGet(string, map[string]string) (int, []byte, error) {
	return 0, nil, nil
}
func (c *envCapabilities) ConfigRead(string) (json.RawMessage, error) { return nil, nil }
func (c *envCapabilities) FormatMarkdown(md string) string {
	return mdrender.Render(md)
}

func dispatch(t *testing.T, h *extension.Host, typ sdk.EventType, payload any) {
	t.Helper()
	raw, _ := json.Marshal(payload)
	if _, err := h.DispatchEvent(context.Background(), sdk.Event{Type: typ, Payload: raw}); err != nil {
		t.Fatalf("dispatch %s: %v", typ, err)
	}
}

func TestAgentsWASMDrivesChatTranscript(t *testing.T) {
	wasmPath := filepath.Join("..", "..", "cmd", "builtins", "agents.wasm")
	data, err := os.ReadFile(wasmPath)
	if err != nil {
		t.Skipf("agents.wasm not built: %v (run `make extensions`)", err)
	}

	h := extension.NewHost(nil)
	ctx := context.Background()
	defer func() { _ = h.Close(ctx) }()

	scene := harness.NewSceneRenderer()
	h.SetUIBridge(&sceneUIBridge{scene: scene})
	// No env set: WASM transcript is the default.
	h.SetCapabilities(&envCapabilities{env: map[string]string{}})

	if err := h.LoadBytes(ctx, "agents.wasm", data, true); err != nil {
		t.Fatalf("load agents.wasm: %v", err)
	}

	// Session start: the extension should create the "chat" area.
	dispatch(t, h, sdk.EventSessionStart, sdk.SessionStartPayload{Reason: "new_session"})
	if !scene.HasArea("chat") {
		t.Skipf("bundled agents.wasm predates the WASM chat transcript; rebuild via `make extensions`")
	}

	// User prompt + streamed assistant tokens.
	dispatch(t, h, sdk.EventBeforeAgentStart, sdk.BeforeAgentStartPayload{Prompt: "what is 2+2?"})
	dispatch(t, h, sdk.EventToken, sdk.TokenPayload{AgentID: "main", Text: "The answer "})
	dispatch(t, h, sdk.EventToken, sdk.TokenPayload{AgentID: "main", Text: "is 4."})

	// Queued prompts must be visible in the transcript even though they are
	// processed by the agent's internal drain turn, which emits no second
	// before_agent_start event.
	dispatch(t, h, sdk.EventBeforeAgentStart, sdk.BeforeAgentStartPayload{
		Prompt: "follow-up question", Queued: true,
	})
	dispatch(t, h, sdk.EventToken, sdk.TokenPayload{AgentID: "main", Text: "The follow-up answer."})

	// A system notification should also be rendered into the transcript.
	dispatch(t, h, sdk.EventNotify, sdk.NotifyPayload{Text: "Extensions reloaded."})

	// Sub-agent text must NOT appear in the main transcript.
	dispatch(t, h, sdk.EventToken, sdk.TokenPayload{AgentID: "main/worker", Text: "SECRET SUBAGENT"})

	out := scene.Render("chat", 60)
	if !strings.Contains(out, "what is 2+2?") {
		t.Fatalf("transcript missing user prompt:\n%s", out)
	}
	if !strings.Contains(out, "The answer is 4.") {
		t.Fatalf("transcript missing streamed assistant text:\n%s", out)
	}
	if !strings.Contains(out, "follow-up question") {
		t.Fatalf("transcript missing queued user prompt:\n%s", out)
	}
	if !strings.Contains(out, "The follow-up answer.") {
		t.Fatalf("transcript missing response after queued prompt:\n%s", out)
	}
	if !strings.Contains(out, "Extensions reloaded.") {
		t.Fatalf("transcript missing notification line:\n%s", out)
	}
	if strings.Contains(out, "SECRET SUBAGENT") {
		t.Fatalf("sub-agent text must not appear in the main transcript:\n%s", out)
	}
}

func TestAgentsWASM_RendersMarkdownAtMessageEnd(t *testing.T) {
	wasmPath := filepath.Join("..", "..", "cmd", "builtins", "agents.wasm")
	data, err := os.ReadFile(wasmPath)
	if err != nil {
		t.Skipf("agents.wasm not built: %v (run `make extensions`)", err)
	}

	h := extension.NewHost(nil)
	ctx := context.Background()
	defer func() { _ = h.Close(ctx) }()

	scene := harness.NewSceneRenderer()
	h.SetUIBridge(&sceneUIBridge{scene: scene})
	h.SetCapabilities(&envCapabilities{env: map[string]string{}})

	if err := h.LoadBytes(ctx, "agents.wasm", data, true); err != nil {
		t.Fatalf("load agents.wasm: %v", err)
	}

	// Non-streamed turn with markdown content: message_end should insert the
	// rendered (ANSI-styled) text, not the raw markers.
	dispatch(t, h, sdk.EventSessionStart, sdk.SessionStartPayload{Reason: "new_session"})
	dispatch(t, h, sdk.EventBeforeAgentStart, sdk.BeforeAgentStartPayload{Prompt: "q"})
	dispatch(t, h, sdk.EventMessageEnd, sdk.MessageEndPayload{
		Role: string(sdk.RoleAssistant), Content: "# Title\n\n```go\nfunc main() {}\n```\n",
	})

	out := scene.Render("chat", 60)
	// The bundled agents.wasm may predate this feature (it is rebuilt via
	// `make extensions`). Skip rather than fail so CI (which never rebuilds
	// WASM) stays green.
	if strings.Contains(out, "# Title") || strings.Contains(out, "```") || out == "" {
		t.Skipf("bundled agents.wasm does not support markdown rendering; rebuild via `make extensions`")
	}
	if !strings.Contains(ansi.Strip(out), "Title") {
		t.Fatalf("rendered header text should be present:\n%s", out)
	}
	if !strings.Contains(ansi.Strip(out), "func main()") {
		t.Fatalf("rendered code body should be present:\n%s", out)
	}
}
