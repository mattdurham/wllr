package main

// wasmhistory_e2e_test.go loads the real built history.wasm and drives the
// /history command through the extension host with a capturing UIBridge,
// verifying the split-picker payload (split flag + per-session transcript
// previews) end to end across the WASM boundary.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mattdurham/wllr/modules/extension"
	"github.com/mattdurham/wllr/modules/sdk"
)

// captureBridge is a no-op UIBridge that records show_picker payloads.
type captureBridge struct {
	onPicker func(sdk.ShowPickerParams)
}

func (b *captureBridge) Notify(string)                                      {}
func (b *captureBridge) ShowModal(string)                                   {}
func (b *captureBridge) ShowPicker(p sdk.ShowPickerParams)                  { b.onPicker(p) }
func (b *captureBridge) ShowAgentTree(sdk.ShowAgentTreeParams)              {}
func (b *captureBridge) SetFocusedAgent(string)                             {}
func (b *captureBridge) ShowTextInput(string, string, string, string)       {}
func (b *captureBridge) Abort()                                             {}
func (b *captureBridge) SetStatus(string, string)                           {}
func (b *captureBridge) GetStatusInfo() sdk.StatusInfo                      { return sdk.StatusInfo{} }
func (b *captureBridge) SendMessage(sdk.Message)                            {}
func (b *captureBridge) RegisterCommand(string, string, bool) error         { return nil }
func (b *captureBridge) RegisterTool(sdk.Tool) error                        { return nil }
func (b *captureBridge) SetSystemPrompt(string)                             {}
func (b *captureBridge) AppendSystemPrompt(string)                          {}
func (b *captureBridge) SetModel(string, string) error                      { return nil }
func (b *captureBridge) ResetHistory([]sdk.Message) error                   { return nil }
func (b *captureBridge) ToolResult(string, string, bool)                    {}
func (b *captureBridge) AfterToolCall(string, string, string, string, bool) {}
func (b *captureBridge) ConsoleOutput(string)                               {}
func (b *captureBridge) ConsoleClear()                                      {}
func (b *captureBridge) CreateArea(sdk.UIArea) error                        { return nil }
func (b *captureBridge) PatchUI(sdk.UIPatchParams) error                    { return nil }
func (b *captureBridge) RemoveArea(string)                                  {}
func (b *captureBridge) UpdateArea(sdk.UIUpdateAreaParams) error            { return nil }

var _ extension.UIBridge = (*captureBridge)(nil)

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return data
}

func TestHistoryWASM_ShowPickerSplitPayload(t *testing.T) {
	wasmPath := filepath.Join("builtins", "history.wasm")
	data, err := os.ReadFile(wasmPath)
	if err != nil {
		t.Skipf("history.wasm not built (run make builtins): %v", err)
	}

	// Create a fake session store with one session containing a conversation,
	// in the directory /history derives from the test process's cwd (no
	// session_start has fired, so the listing scopes by host cwd).
	home := t.TempDir()
	t.Setenv("HOME", home)
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	sanitized := strings.NewReplacer("/", "--", " ", "_").Replace(strings.TrimPrefix(wd, "/"))
	sessDir := filepath.Join(home, ".wllr", "sessions", sanitized)
	if err := os.MkdirAll(sessDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	sess := `{"type":"session","id":"abc","timestamp":"2026-09-23T10:00:00Z","cwd":"/test/proj"}
{"type":"message","role":"user","content":"what is the answer"}
{"type":"message","role":"assistant","content":"forty two"}`
	if err := os.WriteFile(filepath.Join(sessDir, "2026-09-23T10-00-00_abc.jsonl"), []byte(sess), 0o600); err != nil {
		t.Fatalf("write session: %v", err)
	}

	h := extension.NewHost(nil)
	ctx := context.Background()
	defer func() { _ = h.Close(ctx) }()

	perms := builtinManifestPermissions("history")
	var got *sdk.ShowPickerParams
	h.SetUIBridge(&captureBridge{onPicker: func(p sdk.ShowPickerParams) { got = &p }})

	if err := h.LoadBytes(ctx, "history.wasm", data, true, perms...); err != nil {
		t.Fatalf("LoadBytes history.wasm: %v", err)
	}

	// Fire the /history command.
	results, err := h.DispatchEvent(ctx, sdk.Event{
		Type:    sdk.EventOnCommand,
		Payload: mustJSON(t, sdk.OnCommandPayload{Name: "history"}),
	})
	if err != nil {
		t.Fatalf("dispatch history command: %v", err)
	}
	for _, r := range results {
		if r.Error != "" {
			t.Fatalf("history command error: %s", r.Error)
		}
	}

	if got == nil {
		t.Fatal("show_picker was not called")
	}
	if !got.Split {
		t.Error("show_picker payload should carry split=true")
	}
	if len(got.Items) != 1 {
		t.Fatalf("got %d items, want 1", len(got.Items))
	}
	item := got.Items[0]
	if !strings.HasSuffix(item.ID, "2026-09-23T10-00-00_abc.jsonl") {
		t.Errorf("item.ID = %q, want the session file path", item.ID)
	}
	if !strings.Contains(item.Preview, "you:") || !strings.Contains(item.Preview, "what is the answer") {
		t.Errorf("item.Preview missing user transcript: %q", item.Preview)
	}
	if !strings.Contains(item.Preview, "asst:") || !strings.Contains(item.Preview, "forty two") {
		t.Errorf("item.Preview missing assistant transcript: %q", item.Preview)
	}
}
