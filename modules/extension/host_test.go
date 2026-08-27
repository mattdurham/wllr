package extension

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mattdurham/wllr/modules/sdk"
)

// writeWASM writes bytes to a temp file and returns the path.
func writeWASM(t *testing.T, name string, data []byte) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write test wasm: %v", err)
	}
	return path
}

// --- Test bridge helpers ---
// These allow tests to provide callback-like functions via interface implementations.

// testAgentBridge implements AgentBridge using optional callback fields.
type testAgentBridge struct {
	onSpawn                 func(ctx context.Context, req SpawnRequest) error
	onClose                 func(id string) error
	onSendMessage           func(id string, msg sdk.Message) error
	onDeliver               func(id string, msg sdk.Message, wake bool) error
	onRun                   func(id string) error
	onList                  func() ([]AgentInfo, error)
	onTokenCount            func() int64
	onSetHistory            func(id string, messages []sdk.Message) error
	onMainAgentContextUsage func() sdk.ContextUsage
	onSnapshotInbox         func(id string) ([]sdk.Message, error)
	onDeleteFromInbox       func(id string, byIndex int, byMessageID string) (int, error)
	onEditInboxMessage      func(id string, byIndex int, byMessageID string, newContent string) error
}

func (b *testAgentBridge) Spawn(ctx context.Context, req SpawnRequest) error {
	if b.onSpawn != nil {
		return b.onSpawn(ctx, req)
	}
	return nil
}

func (b *testAgentBridge) Close(id string) error {
	if b.onClose != nil {
		return b.onClose(id)
	}
	return nil
}

func (b *testAgentBridge) SendMessage(id string, msg sdk.Message) error {
	if b.onSendMessage != nil {
		return b.onSendMessage(id, msg)
	}
	return nil
}

func (b *testAgentBridge) Deliver(id string, msg sdk.Message, wake bool) error {
	if b.onDeliver != nil {
		return b.onDeliver(id, msg, wake)
	}
	return nil
}

func (b *testAgentBridge) Run(id string) error {
	if b.onRun != nil {
		return b.onRun(id)
	}
	return nil
}

func (b *testAgentBridge) List() ([]AgentInfo, error) {
	if b.onList != nil {
		return b.onList()
	}
	return nil, nil
}

func (b *testAgentBridge) TokenCount() int64 {
	if b.onTokenCount != nil {
		return b.onTokenCount()
	}
	return 0
}

func (b *testAgentBridge) SetHistory(id string, messages []sdk.Message) error {
	if b.onSetHistory != nil {
		return b.onSetHistory(id, messages)
	}
	return nil
}

func (b *testAgentBridge) MainAgentContextUsage() sdk.ContextUsage {
	if b.onMainAgentContextUsage != nil {
		return b.onMainAgentContextUsage()
	}
	return sdk.ContextUsage{}
}

func (b *testAgentBridge) SnapshotInbox(id string) ([]sdk.Message, error) {
	if b.onSnapshotInbox != nil {
		return b.onSnapshotInbox(id)
	}
	return nil, nil
}

func (b *testAgentBridge) DeleteFromInbox(id string, byIndex int, byMessageID string) (int, error) {
	if b.onDeleteFromInbox != nil {
		return b.onDeleteFromInbox(id, byIndex, byMessageID)
	}
	return 0, nil
}

func (b *testAgentBridge) EditInboxMessage(id string, byIndex int, byMessageID string, newContent string) error {
	if b.onEditInboxMessage != nil {
		return b.onEditInboxMessage(id, byIndex, byMessageID, newContent)
	}
	return nil
}

// testTeamBridge implements TeamBridge using optional callback fields.
type testTeamBridge struct {
	onCreate       func(id, name string) error
	onClose        func(ctx context.Context, id string) error
	onAddMember    func(teamID, agentID string) error
	onRemoveMember func(teamID, agentID string) error
	onGetMembers   func(teamID string) ([]string, error)
	onList         func() ([]string, error)
}

func (b *testTeamBridge) Create(id, name string) error {
	if b.onCreate != nil {
		return b.onCreate(id, name)
	}
	return nil
}

func (b *testTeamBridge) Close(ctx context.Context, id string) error {
	if b.onClose != nil {
		return b.onClose(ctx, id)
	}
	return nil
}

func (b *testTeamBridge) AddMember(teamID, agentID string) error {
	if b.onAddMember != nil {
		return b.onAddMember(teamID, agentID)
	}
	return nil
}

func (b *testTeamBridge) RemoveMember(teamID, agentID string) error {
	if b.onRemoveMember != nil {
		return b.onRemoveMember(teamID, agentID)
	}
	return nil
}

func (b *testTeamBridge) GetMembers(teamID string) ([]string, error) {
	if b.onGetMembers != nil {
		return b.onGetMembers(teamID)
	}
	return nil, nil
}

func (b *testTeamBridge) List() ([]string, error) {
	if b.onList != nil {
		return b.onList()
	}
	return nil, nil
}

// testUIBridge implements UIBridge using optional callback fields.
type testUIBridge struct {
	onNotify          func(text string)
	onShowModal       func(text string)
	onShowPicker      func(title string, items []sdk.ShowPickerItem, callback string)
	onShowTextInput   func(title, placeholder, initialValue, callback string)
	onAbort           func()
	onSetStatus       func(key, value string)
	onGetStatusInfo   func() sdk.StatusInfo
	onSendMessage     func(msg sdk.Message)
	onRegisterCommand func(name, desc string, instant bool) error
	onRegisterTool    func(tool sdk.Tool) error
	onSetSystemPrompt func(prompt string)
	onAppendSP        func(text string)
	onResetHistory    func(messages []sdk.Message) error
	onToolResult      func(toolCallID, result string, isError bool)
	onAfterToolCall   func(agentID, toolCallID, toolName, result string, isError bool)
	onConsoleOutput   func(line string)
	onConsoleClear    func()
	createdAreas      []string
	patchedAreas      []string
	removedAreas      []string
}

func (b *testUIBridge) Notify(text string) {
	if b.onNotify != nil {
		b.onNotify(text)
	}
}

func (b *testUIBridge) ShowModal(text string) {
	if b.onShowModal != nil {
		b.onShowModal(text)
	}
}

func (b *testUIBridge) ShowPicker(title string, items []sdk.ShowPickerItem, callback string) {
	if b.onShowPicker != nil {
		b.onShowPicker(title, items, callback)
	}
}

func (b *testUIBridge) ShowTextInput(title, placeholder, initialValue, callback string) {
	if b.onShowTextInput != nil {
		b.onShowTextInput(title, placeholder, initialValue, callback)
	}
}

func (b *testUIBridge) Abort() {
	if b.onAbort != nil {
		b.onAbort()
	}
}

func (b *testUIBridge) SetStatus(key, value string) {
	if b.onSetStatus != nil {
		b.onSetStatus(key, value)
	}
}

func (b *testUIBridge) GetStatusInfo() sdk.StatusInfo {
	if b.onGetStatusInfo != nil {
		return b.onGetStatusInfo()
	}
	return sdk.StatusInfo{}
}

func (b *testUIBridge) SendMessage(msg sdk.Message) {
	if b.onSendMessage != nil {
		b.onSendMessage(msg)
	}
}

func (b *testUIBridge) RegisterCommand(name, desc string, instant bool) error {
	if b.onRegisterCommand != nil {
		return b.onRegisterCommand(name, desc, instant)
	}
	return nil
}

func (b *testUIBridge) RegisterTool(tool sdk.Tool) error {
	if b.onRegisterTool != nil {
		return b.onRegisterTool(tool)
	}
	return nil
}

func (b *testUIBridge) SetSystemPrompt(prompt string) {
	if b.onSetSystemPrompt != nil {
		b.onSetSystemPrompt(prompt)
	}
}

func (b *testUIBridge) AppendSystemPrompt(text string) {
	if b.onAppendSP != nil {
		b.onAppendSP(text)
	}
}

func (b *testUIBridge) ResetHistory(messages []sdk.Message) error {
	if b.onResetHistory != nil {
		return b.onResetHistory(messages)
	}
	return nil
}

func (b *testUIBridge) ToolResult(toolCallID, result string, isError bool) {
	if b.onToolResult != nil {
		b.onToolResult(toolCallID, result, isError)
	}
}

func (b *testUIBridge) AfterToolCall(agentID, toolCallID, toolName, result string, isError bool) {
	if b.onAfterToolCall != nil {
		b.onAfterToolCall(agentID, toolCallID, toolName, result, isError)
	}
}

func (b *testUIBridge) ConsoleOutput(line string) {
	if b.onConsoleOutput != nil {
		b.onConsoleOutput(line)
	}
}

func (b *testUIBridge) ConsoleClear() {
	if b.onConsoleClear != nil {
		b.onConsoleClear()
	}
}

func (b *testUIBridge) CreateArea(a sdk.UIArea) error {
	b.createdAreas = append(b.createdAreas, a.ID)
	return nil
}

func (b *testUIBridge) PatchUI(p sdk.UIPatchParams) error {
	b.patchedAreas = append(b.patchedAreas, p.Area)
	return nil
}

func (b *testUIBridge) RemoveArea(id string) {
	b.removedAreas = append(b.removedAreas, id)
}

func (b *testUIBridge) UpdateArea(_ sdk.UIUpdateAreaParams) error { return nil }

// testCapabilityProvider implements CapabilityProvider using optional callback fields.
type testCapabilityProvider struct {
	onExec           func(ctx context.Context, command, dir string, onLine func(string)) (string, error)
	onGetEnv         func(name string) (string, error)
	onReadFile       func(path string) (string, error)
	onWriteFile      func(path, content string) error
	onAppendFile     func(path, content string) error
	onHTTPPost       func(url string, headers map[string]string, body []byte) (int, []byte, error)
	onHTTPGet        func(url string, headers map[string]string) (int, []byte, error)
	onConfigRead     func(group string) (json.RawMessage, error)
	onFormatMarkdown func(markdown string) string
}

func (p *testCapabilityProvider) Exec(ctx context.Context, command, dir string, onLine func(string)) (string, error) {
	if p.onExec != nil {
		return p.onExec(ctx, command, dir, onLine)
	}
	return "", nil
}

func (p *testCapabilityProvider) GetEnv(name string) (string, error) {
	if p.onGetEnv != nil {
		return p.onGetEnv(name)
	}
	return "", nil
}

func (p *testCapabilityProvider) ReadFile(path string) (string, error) {
	if p.onReadFile != nil {
		return p.onReadFile(path)
	}
	return "", nil
}

func (p *testCapabilityProvider) WriteFile(path, content string) error {
	if p.onWriteFile != nil {
		return p.onWriteFile(path, content)
	}
	return nil
}

func (p *testCapabilityProvider) AppendFile(path, content string) error {
	if p.onAppendFile != nil {
		return p.onAppendFile(path, content)
	}
	return nil
}

func (p *testCapabilityProvider) HTTPPost(url string, headers map[string]string, body []byte) (int, []byte, error) {
	if p.onHTTPPost != nil {
		return p.onHTTPPost(url, headers, body)
	}
	return 200, nil, nil
}

func (p *testCapabilityProvider) HTTPGet(url string, headers map[string]string) (int, []byte, error) {
	if p.onHTTPGet != nil {
		return p.onHTTPGet(url, headers)
	}
	return 200, nil, nil
}

func (p *testCapabilityProvider) ConfigRead(group string) (json.RawMessage, error) {
	if p.onConfigRead != nil {
		return p.onConfigRead(group)
	}
	return nil, nil
}

func (p *testCapabilityProvider) FormatMarkdown(markdown string) string {
	if p.onFormatMarkdown != nil {
		return p.onFormatMarkdown(markdown)
	}
	return markdown
}

func TestHost_Load_MinimalWASM(t *testing.T) {
	ctx := context.Background()
	h := NewHost(nil)
	defer func() { _ = h.Close(ctx) }()

	path := writeWASM(t, "minimal.wasm", minimalWASM)
	if err := h.Load(ctx, path); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(h.extensions) != 1 {
		t.Errorf("expected 1 extension, got %d", len(h.extensions))
	}
}

func TestHost_Load_FileNotFound(t *testing.T) {
	ctx := context.Background()
	h := NewHost(nil)
	defer func() { _ = h.Close(ctx) }()

	err := h.Load(ctx, "/nonexistent/path/to/extension.wasm")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestHost_Load_MissingExport(t *testing.T) {
	ctx := context.Background()
	h := NewHost(nil)
	defer func() { _ = h.Close(ctx) }()

	path := writeWASM(t, "missing-free.wasm", missingFreeWASM)
	err := h.Load(ctx, path)
	if err == nil {
		t.Fatal("expected error for missing _free export, got nil")
	}
}

// --- Manifest loading tests ---

func TestHost_Load_JSONManifestPermissions(t *testing.T) {
	ctx := context.Background()
	h := NewHost(nil)
	defer func() { _ = h.Close(ctx) }()

	path := writeWASM(t, "minimal.wasm", minimalWASM)
	if err := os.WriteFile(strings.TrimSuffix(path, ".wasm")+".json", []byte(`{"permissions":["exec","file_read"]}`), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := h.Load(ctx, path); err != nil {
		t.Fatalf("Load: %v", err)
	}
	ext := h.extensions[0]
	if !ext.HasPermission(sdk.PermExec) || !ext.HasPermission(sdk.PermFileRead) {
		t.Error("expected exec and file_read permissions from JSON manifest")
	}
	if ext.HasPermission(sdk.PermFileWrite) {
		t.Error("should not have undeclared file_write permission")
	}
}

func TestHost_Load_YAMLManifestPermissions(t *testing.T) {
	ctx := context.Background()
	h := NewHost(nil)
	defer func() { _ = h.Close(ctx) }()

	path := writeWASM(t, "minimal.wasm", minimalWASM)
	if err := os.WriteFile(strings.TrimSuffix(path, ".wasm")+".yaml", []byte("permissions:\n  - network_read\n  - file_write\n"), 0o600); err != nil {
		t.Fatalf("write yaml manifest: %v", err)
	}
	if err := h.Load(ctx, path); err != nil {
		t.Fatalf("Load: %v", err)
	}
	ext := h.extensions[0]
	if !ext.HasPermission(sdk.PermNetworkRead) || !ext.HasPermission(sdk.PermFileWrite) {
		t.Error("expected network_read and file_write permissions from YAML manifest")
	}
}

func TestHost_Load_MalformedManifest_SilentlyDenied(t *testing.T) {
	ctx := context.Background()
	h := NewHost(nil)
	defer func() { _ = h.Close(ctx) }()

	path := writeWASM(t, "minimal.wasm", minimalWASM)
	if err := os.WriteFile(strings.TrimSuffix(path, ".wasm")+".json", []byte(`{invalid json`), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := h.Load(ctx, path); err != nil {
		t.Fatalf("Load: %v", err)
	}
	ext := h.extensions[0]
	// Malformed manifest -> zero permissions.
	if ext.HasPermission(sdk.PermExec) {
		t.Error("malformed manifest should yield no permissions")
	}
}

func TestHost_Load_UnknownPermission_Ignored(t *testing.T) {
	ctx := context.Background()
	h := NewHost(nil)
	defer func() { _ = h.Close(ctx) }()

	path := writeWASM(t, "minimal.wasm", minimalWASM)
	if err := os.WriteFile(strings.TrimSuffix(path, ".wasm")+".json", []byte(`{"permissions":["exec","read","network_read"]}`), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := h.Load(ctx, path); err != nil {
		t.Fatalf("Load: %v", err)
	}
	ext := h.extensions[0]
	// "read" is not a canonical permission; it must be ignored, while exec and network_read are kept.
	if !ext.HasPermission(sdk.PermExec) || !ext.HasPermission(sdk.PermNetworkRead) {
		t.Error("expected exec and network_read permissions")
	}
	if ext.HasPermission("read") {
		t.Error("unknown permission 'read' should have been dropped")
	}
}

func TestHost_Load_NoManifest_ZeroPermissions(t *testing.T) {
	ctx := context.Background()
	h := NewHost(nil)
	defer func() { _ = h.Close(ctx) }()

	path := writeWASM(t, "minimal.wasm", minimalWASM)
	if err := h.Load(ctx, path); err != nil {
		t.Fatalf("Load: %v", err)
	}
	ext := h.extensions[0]
	if ext.HasPermission(sdk.PermExec) || ext.HasPermission(sdk.PermFileRead) {
		t.Error("extension without manifest should have zero permissions")
	}
}

func TestHost_DispatchEvent_NotSubscribed(t *testing.T) {
	ctx := context.Background()

	var onEventCalled bool
	h := NewHost(nil)
	defer func() { _ = h.Close(ctx) }()

	path := writeWASM(t, "minimal.wasm", minimalWASM)
	if err := h.Load(ctx, path); err != nil {
		t.Fatalf("Load: %v", err)
	}

	// minimalWASM _init does not subscribe to anything.
	// So dispatching session_start should NOT call _on_event.
	// We verify by checking that no responses come back and no errors.
	_ = onEventCalled

	evt := sdk.Event{Type: sdk.EventSessionStart}
	responses, err := h.DispatchEvent(ctx, evt)
	if err != nil {
		t.Fatalf("DispatchEvent: %v", err)
	}
	if len(responses) != 0 {
		t.Errorf("expected 0 responses (unsubscribed), got %d", len(responses))
	}
}

func TestHost_DispatchEvent_Subscribed(t *testing.T) {
	ctx := context.Background()
	h := NewHost(nil)
	defer func() { _ = h.Close(ctx) }()

	path := writeWASM(t, "minimal.wasm", minimalWASM)
	if err := h.Load(ctx, path); err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Manually subscribe the extension to session_start.
	h.extensions[0].subscriptions[sdk.EventSessionStart] = true

	evt := sdk.Event{Type: sdk.EventSessionStart}
	responses, err := h.DispatchEvent(ctx, evt)
	if err != nil {
		t.Fatalf("DispatchEvent: %v", err)
	}
	// _on_event returns 0 (no response ptr), so we get an empty EventResponse.
	if len(responses) != 1 {
		t.Errorf("expected 1 response (subscribed), got %d", len(responses))
	}
}

func TestHost_Subscribe_ViaHostCall(t *testing.T) {
	ctx := context.Background()
	h := NewHost(nil)
	defer func() { _ = h.Close(ctx) }()

	path := writeWASM(t, "minimal.wasm", minimalWASM)
	if err := h.Load(ctx, path); err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Simulate what an extension would do via host_call: subscribe to session_start.
	ext := h.extensions[0]
	req := sdk.HostCallRequest{
		Method: sdk.MethodSubscribe,
		Params: []byte(`{"event":"session_start"}`),
	}
	resp := h.handleSubscribe(ext, req)
	if resp.Error != "" {
		t.Fatalf("handleSubscribe: %s", resp.Error)
	}
	if !ext.subscriptions[sdk.EventSessionStart] {
		t.Error("expected session_start to be subscribed")
	}
}

func TestHost_Store_SetGet(t *testing.T) {
	ctx := context.Background()
	h := NewHost(nil)
	defer func() { _ = h.Close(ctx) }()

	path := writeWASM(t, "minimal.wasm", minimalWASM)
	if err := h.Load(ctx, path); err != nil {
		t.Fatalf("Load: %v", err)
	}

	ext := h.extensions[0]

	// Test store_set.
	setResp := h.handleStoreSet(ext, sdk.HostCallRequest{
		Method: sdk.MethodStoreSet,
		Params: []byte(`{"key":"foo","value":"bar"}`),
	})
	if setResp.Error != "" {
		t.Fatalf("store_set: %s", setResp.Error)
	}

	// Test store_get.
	getResp := h.handleStoreGet(ext, sdk.HostCallRequest{
		Method: sdk.MethodStoreGet,
		Params: []byte(`{"key":"foo"}`),
	})
	if getResp.Error != "" {
		t.Fatalf("store_get: %s", getResp.Error)
	}
	if string(getResp.Result) != `{"value":"bar"}` {
		t.Errorf("store_get result: got %s, want %s", getResp.Result, `{"value":"bar"}`)
	}
}

func TestHost_Store_GetMiss(t *testing.T) {
	ctx := context.Background()
	h := NewHost(nil)
	defer func() { _ = h.Close(ctx) }()

	path := writeWASM(t, "minimal.wasm", minimalWASM)
	if err := h.Load(ctx, path); err != nil {
		t.Fatalf("Load: %v", err)
	}

	ext := h.extensions[0]
	resp := h.handleStoreGet(ext, sdk.HostCallRequest{
		Method: sdk.MethodStoreGet,
		Params: []byte(`{"key":"missing"}`),
	})
	if resp.Error == "" {
		t.Error("expected error for missing key, got none")
	}
}

func TestHost_RegisterTool_DuplicateRejected(t *testing.T) {
	ctx := context.Background()
	h := NewHost(nil)
	defer func() { _ = h.Close(ctx) }()

	path := writeWASM(t, "minimal.wasm", minimalWASM)
	if err := h.Load(ctx, path); err != nil {
		t.Fatalf("Load: %v", err)
	}

	toolJSON := []byte(`{"name":"search","description":"search the web","input_schema":{}}`)

	resp1 := h.handleRegisterTool(nil, sdk.HostCallRequest{
		Method: sdk.MethodRegisterTool,
		Params: toolJSON,
	})
	if resp1.Error != "" {
		t.Fatalf("first register_tool: %s", resp1.Error)
	}

	resp2 := h.handleRegisterTool(nil, sdk.HostCallRequest{
		Method: sdk.MethodRegisterTool,
		Params: toolJSON,
	})
	if resp2.Error == "" {
		t.Fatal("expected error for duplicate tool registration, got none")
	}
}

func TestHost_Callbacks_SetStatus(t *testing.T) {
	ctx := context.Background()
	h := NewHost(nil)
	defer func() { _ = h.Close(ctx) }()

	var gotKey, gotValue string
	h.SetUIBridge(&testUIBridge{onSetStatus: func(k, v string) {
		gotKey = k
		gotValue = v
	}})

	path := writeWASM(t, "minimal.wasm", minimalWASM)
	if err := h.Load(ctx, path); err != nil {
		t.Fatalf("Load: %v", err)
	}

	ext := h.extensions[0]
	resp := h.routeHostCall(ctx, ext.module, ext, sdk.HostCallRequest{
		Method: sdk.MethodSetStatus,
		Params: []byte(`{"key":"status","value":"ok"}`),
	})
	if resp.Error != "" {
		t.Fatalf("set_status: %s", resp.Error)
	}
	if gotKey != "status" || gotValue != "ok" {
		t.Errorf("OnSetStatus: got key=%q value=%q, want key=%q value=%q", gotKey, gotValue, "status", "ok")
	}
}

func TestHost_Reload(t *testing.T) {
	ctx := context.Background()
	h := NewHost(nil)
	defer func() { _ = h.Close(ctx) }()

	path := writeWASM(t, "minimal.wasm", minimalWASM)
	if err := h.Load(ctx, path); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(h.extensions) != 1 {
		t.Fatalf("before reload: expected 1 extension, got %d", len(h.extensions))
	}

	// Reload with the same path.
	if err := h.Reload(ctx, []string{path}); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if len(h.extensions) != 1 {
		t.Errorf("after reload: expected 1 extension, got %d", len(h.extensions))
	}
}

func TestHost_Multiple_Extensions(t *testing.T) {
	ctx := context.Background()
	h := NewHost(nil)
	defer func() { _ = h.Close(ctx) }()

	// Load the same WASM twice under different names.
	path1 := writeWASM(t, "ext1.wasm", minimalWASM)
	path2 := writeWASM(t, "ext2.wasm", minimalWASM)

	if err := h.Load(ctx, path1); err != nil {
		t.Fatalf("Load ext1: %v", err)
	}
	if err := h.Load(ctx, path2); err != nil {
		t.Fatalf("Load ext2: %v", err)
	}
	if len(h.extensions) != 2 {
		t.Errorf("expected 2 extensions, got %d", len(h.extensions))
	}

	// Subscribe both to session_start.
	h.extensions[0].subscriptions[sdk.EventSessionStart] = true
	h.extensions[1].subscriptions[sdk.EventSessionStart] = true

	evt := sdk.Event{Type: sdk.EventSessionStart}
	responses, err := h.DispatchEvent(ctx, evt)
	if err != nil {
		t.Fatalf("DispatchEvent: %v", err)
	}
	if len(responses) != 2 {
		t.Errorf("expected 2 responses, got %d", len(responses))
	}
}

func TestHost_EchoWASM_SkipIfMissing(t *testing.T) {
	// This test uses the real echo.wasm built from testdata/echo/main.go with TinyGo.
	// Skip if the file doesn't exist.
	echoPath := filepath.Join("testdata", "echo.wasm")
	if _, err := os.Stat(echoPath); os.IsNotExist(err) {
		t.Skip("echo.wasm not found (build with: tinygo build -o testdata/echo.wasm -target wasi ./testdata/echo/)")
	}

	ctx := context.Background()
	h := NewHost(nil)
	defer func() { _ = h.Close(ctx) }()

	if err := h.Load(ctx, echoPath); err != nil {
		t.Fatalf("Load echo.wasm: %v", err)
	}
	if len(h.extensions) != 1 {
		t.Fatalf("expected 1 extension, got %d", len(h.extensions))
	}
}

// --- Permission model tests ---

func TestHost_Permission_TrustedLeastPrivilege(t *testing.T) {
	ctx := context.Background()
	h := NewHost(nil)
	defer func() { _ = h.Close(ctx) }()

	// A trusted built-in loaded without explicit permissions gets ZERO granted.
	// Trusted is an ordering/audit flag, not a permission grant.
	if err := h.LoadBytes(ctx, "trusted.wasm", minimalWASM, true); err != nil {
		t.Fatalf("LoadBytes trusted: %v", err)
	}

	ext := h.extensions[0]
	if !ext.trusted {
		t.Fatal("expected extension loaded with trusted=true to be trusted")
	}

	// Least privilege: with no declared permissions, nothing is granted.
	for _, perm := range []sdk.Permission{
		sdk.PermExec, sdk.PermFileOpen, sdk.PermFileRead,
		sdk.PermFileWrite, sdk.PermNetworkRead, sdk.PermNetworkWrite,
	} {
		if ext.HasPermission(perm) {
			t.Errorf("trusted extension with no declared permissions should NOT have %s", perm)
		}
	}
}

func TestHost_Permission_TrustedScopedDeclared(t *testing.T) {
	ctx := context.Background()
	h := NewHost(nil)
	defer func() { _ = h.Close(ctx) }()

	// A trusted built-in granted only file_write must NOT also get exec or
	// network access — this is the guardrail for built-ins.
	if err := h.LoadBytes(ctx, "logging.wasm", minimalWASM, true, sdk.PermFileWrite); err != nil {
		t.Fatalf("LoadBytes trusted scoped: %v", err)
	}

	ext := h.extensions[0]
	if !ext.HasPermission(sdk.PermFileWrite) {
		t.Error("trusted built-in should have granted file_write")
	}
	for _, denied := range []sdk.Permission{
		sdk.PermExec, sdk.PermFileRead, sdk.PermNetworkRead, sdk.PermNetworkWrite, sdk.PermUI,
	} {
		if ext.HasPermission(denied) {
			t.Errorf("file_write-only built-in must NOT have %s", denied)
		}
	}
}

func TestHost_Permission_UntrustedDeclared(t *testing.T) {
	ctx := context.Background()
	h := NewHost(nil)
	defer func() { _ = h.Close(ctx) }()

	if err := h.LoadBytes(ctx, "untrusted.wasm", minimalWASM, false); err != nil {
		t.Fatalf("LoadBytes untrusted: %v", err)
	}

	ext := h.extensions[0]
	if ext.trusted {
		t.Fatal("expected extension loaded with trusted=false to NOT be trusted")
	}

	// Grant file_read manually (simulating a manifest).
	ext.permissions[sdk.PermFileRead] = true

	if !ext.HasPermission(sdk.PermFileRead) {
		t.Error("extension should have file_read permission")
	}
	if ext.HasPermission(sdk.PermFileWrite) {
		t.Error("extension should NOT have file_write permission")
	}
}

func TestHost_Permission_RequestPermission_Granted(t *testing.T) {
	ctx := context.Background()
	h := NewHost(nil)
	defer func() { _ = h.Close(ctx) }()

	if err := h.LoadBytes(ctx, "ext.wasm", minimalWASM, false); err != nil {
		t.Fatalf("LoadBytes: %v", err)
	}

	ext := h.extensions[0]
	ext.permissions[sdk.PermFileRead] = true

	resp := h.routeHostCall(ctx, ext.module, ext, sdk.HostCallRequest{
		Method: sdk.MethodRequestPermission,
		Params: []byte(`{"permission":"file_read"}`),
	})
	if resp.Error != "" {
		t.Fatalf("request_permission file_read: %s", resp.Error)
	}
}

func TestHost_Permission_RequestPermission_Denied(t *testing.T) {
	ctx := context.Background()
	h := NewHost(nil)
	defer func() { _ = h.Close(ctx) }()

	if err := h.LoadBytes(ctx, "ext.wasm", minimalWASM, false); err != nil {
		t.Fatalf("LoadBytes: %v", err)
	}

	ext := h.extensions[0]
	// No permissions granted.

	resp := h.routeHostCall(ctx, ext.module, ext, sdk.HostCallRequest{
		Method: sdk.MethodRequestPermission,
		Params: []byte(`{"permission":"file_write"}`),
	})
	if resp.Error == "" {
		t.Fatal("expected permission denied error, got none")
	}
}

func TestHost_FormatMarkdown_Renders(t *testing.T) {
	ctx := context.Background()
	h := NewHost(nil)
	defer func() { _ = h.Close(ctx) }()

	var got string
	h.SetCapabilities(&testCapabilityProvider{onFormatMarkdown: func(s string) string {
		got = s
		return "RENDERED"
	}})

	if err := h.LoadBytes(ctx, "chat.wasm", minimalWASM, false, sdk.PermUI); err != nil {
		t.Fatalf("LoadBytes: %v", err)
	}
	ext := h.extensions[0]
	resp := h.routeHostCall(ctx, ext.module, ext, sdk.HostCallRequest{
		Method: sdk.MethodFormatMarkdown,
		Params: []byte(`{"text":"# hi\n\ncode block\n"}`),
	})
	if resp.Error != "" {
		t.Fatalf("format_markdown: %s", resp.Error)
	}
	if got != "# hi\n\ncode block\n" {
		t.Fatalf("renderer should receive raw markdown, got %q", got)
	}
	var out struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(resp.Result, &out); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if out.Text != "RENDERED" {
		t.Fatalf("expected rendered text, got %q", out.Text)
	}
}

func TestHost_FormatMarkdown_PermissionDenied(t *testing.T) {
	ctx := context.Background()
	h := NewHost(nil)
	defer func() { _ = h.Close(ctx) }()

	if err := h.LoadBytes(ctx, "chat.wasm", minimalWASM, false); err != nil {
		t.Fatalf("LoadBytes: %v", err)
	}
	ext := h.extensions[0]
	resp := h.routeHostCall(ctx, ext.module, ext, sdk.HostCallRequest{
		Method: sdk.MethodFormatMarkdown,
		Params: []byte(`{"text":"# hi"}`),
	})
	if resp.Error == "" || !strings.Contains(resp.Error, "permission denied") {
		t.Fatalf("expected permission denied error, got %q", resp.Error)
	}
}

// --- LoadBytes tests ---

func TestHost_LoadBytes_TrustedExtension(t *testing.T) {
	ctx := context.Background()
	h := NewHost(nil)
	defer func() { _ = h.Close(ctx) }()

	if err := h.LoadBytes(ctx, "builtin.wasm", minimalWASM, true); err != nil {
		t.Fatalf("LoadBytes: %v", err)
	}
	if len(h.extensions) != 1 {
		t.Fatalf("expected 1 extension, got %d", len(h.extensions))
	}
	if !h.extensions[0].trusted {
		t.Error("built-in extension should be trusted")
	}
}

func TestHost_LoadBytes_UntrustedExtension(t *testing.T) {
	ctx := context.Background()
	h := NewHost(nil)
	defer func() { _ = h.Close(ctx) }()

	if err := h.LoadBytes(ctx, "user.wasm", minimalWASM, false); err != nil {
		t.Fatalf("LoadBytes: %v", err)
	}
	if len(h.extensions) != 1 {
		t.Fatalf("expected 1 extension, got %d", len(h.extensions))
	}
	if h.extensions[0].trusted {
		t.Error("user extension should not be trusted by default")
	}
}

// --- Tool override tests ---

func TestHost_RegisterTool_OverrideAllowed(t *testing.T) {
	ctx := context.Background()
	h := NewHost(nil)
	defer func() { _ = h.Close(ctx) }()

	path := writeWASM(t, "minimal.wasm", minimalWASM)
	if err := h.Load(ctx, path); err != nil {
		t.Fatalf("Load: %v", err)
	}

	// First registration.
	resp1 := h.handleRegisterTool(nil, sdk.HostCallRequest{
		Method: sdk.MethodRegisterTool,
		Params: []byte(`{"name":"search","description":"v1","input_schema":{}}`),
	})
	if resp1.Error != "" {
		t.Fatalf("first register_tool: %s", resp1.Error)
	}

	// Second registration with override: true should succeed.
	resp2 := h.handleRegisterTool(nil, sdk.HostCallRequest{
		Method: sdk.MethodRegisterTool,
		Params: []byte(`{"name":"search","description":"v2","input_schema":{},"override":true}`),
	})
	if resp2.Error != "" {
		t.Fatalf("override register_tool: %s", resp2.Error)
	}

	// Verify the new description is stored.
	h.mu.RLock()
	tool := h.registeredTools["search"]
	h.mu.RUnlock()
	if tool.Description != "v2" {
		t.Errorf("expected tool description %q after override, got %q", "v2", tool.Description)
	}
}

// --- ExecuteTool tests ---

func TestHost_ExecuteTool_CallbackInvoked(t *testing.T) {
	ctx := context.Background()
	h := NewHost(nil)
	defer func() { _ = h.Close(ctx) }()

	// Register a tool with a callback.
	const toolCallID = "call-1"
	const toolName = "echo"
	const toolResult = "hello"

	resultCh := make(chan string, 1)
	h.SetUIBridge(&testUIBridge{onToolResult: func(id, result string, isError bool) {
		resultCh <- result
	}})

	// Register the tool.
	h.mu.Lock()
	h.registeredTools[toolName] = sdk.Tool{Name: toolName, Description: "echoes input"}
	h.mu.Unlock()

	// ExecuteTool should block until the extension calls tool_result.
	// We simulate the extension calling tool_result asynchronously.
	go func() {
		// Simulate the extension returning the result via host_call tool_result.
		h.handleToolResult(sdk.HostCallRequest{
			Method: sdk.MethodToolResult,
			Params: []byte(`{"tool_call_id":"` + toolCallID + `","result":"` + toolResult + `","is_error":false}`),
		})
	}()

	resp, err := h.ExecuteTool(ctx, "test-agent", toolCallID, toolName, []byte(`{}`))
	if err != nil {
		t.Fatalf("ExecuteTool: %v", err)
	}
	if resp.Result != toolResult {
		t.Errorf("ExecuteTool result: got %q, want %q", resp.Result, toolResult)
	}
	if resp.IsError {
		t.Error("expected IsError=false")
	}
}

func TestHost_ExecuteTool_ErrorResult(t *testing.T) {
	ctx := context.Background()
	h := NewHost(nil)
	defer func() { _ = h.Close(ctx) }()

	const toolCallID = "call-err"
	const toolName = "broken"

	h.mu.Lock()
	h.registeredTools[toolName] = sdk.Tool{Name: toolName}
	h.mu.Unlock()

	go func() {
		h.handleToolResult(sdk.HostCallRequest{
			Method: sdk.MethodToolResult,
			Params: []byte(`{"tool_call_id":"` + toolCallID + `","result":"something went wrong","is_error":true}`),
		})
	}()

	resp, err := h.ExecuteTool(ctx, "test-agent", toolCallID, toolName, []byte(`{}`))
	if err != nil {
		t.Fatalf("ExecuteTool: %v", err)
	}
	if !resp.IsError {
		t.Error("expected IsError=true")
	}
	if resp.Result != "something went wrong" {
		t.Errorf("ExecuteTool error result: got %q", resp.Result)
	}
}

func TestHost_ExecuteTool_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	h := NewHost(nil)
	defer func() { _ = h.Close(context.Background()) }()

	const toolCallID = "call-cancel"
	const toolName = "slow"

	h.mu.Lock()
	h.registeredTools[toolName] = sdk.Tool{Name: toolName}
	h.mu.Unlock()

	// Cancel context before extension returns result.
	go func() {
		cancel()
	}()

	_, err := h.ExecuteTool(ctx, "test-agent", toolCallID, toolName, []byte(`{}`))
	if err == nil {
		t.Fatal("expected error on context cancellation, got nil")
	}
}

func TestHost_ExecuteTool_AfterToolCallDispatched(t *testing.T) {
	ctx := context.Background()
	h := NewHost(nil)
	defer func() { _ = h.Close(ctx) }()

	const toolCallID = "call-after"
	const toolName = "mytool"
	const toolResultStr = "done"

	// Track whether AfterToolCall was invoked (proves EventAfterToolCall path ran).
	afterCh := make(chan struct{}, 1)
	h.SetUIBridge(&testUIBridge{onAfterToolCall: func(agentID, id, name, result string, isError bool) {
		if agentID == "test-agent" && id == toolCallID && name == toolName && result == toolResultStr && !isError {
			afterCh <- struct{}{}
		}
	}})

	h.mu.Lock()
	h.registeredTools[toolName] = sdk.Tool{Name: toolName}
	h.mu.Unlock()

	// Simulate the extension returning the result asynchronously.
	go func() {
		h.handleToolResult(sdk.HostCallRequest{
			Method: sdk.MethodToolResult,
			Params: []byte(`{"tool_call_id":"` + toolCallID + `","result":"` + toolResultStr + `","is_error":false}`),
		})
	}()

	resp, err := h.ExecuteTool(ctx, "test-agent", toolCallID, toolName, []byte(`{}`))
	if err != nil {
		t.Fatalf("ExecuteTool: %v", err)
	}
	if resp.Result != toolResultStr {
		t.Errorf("ExecuteTool result: got %q, want %q", resp.Result, toolResultStr)
	}

	// AfterToolCall must have been invoked (proves EventAfterToolCall dispatch happened).
	select {
	case <-afterCh:
		// Good.
	default:
		t.Error("expected AfterToolCall to be called after ExecuteTool completed")
	}
}

func TestHost_OnAfterToolCall_Callback(t *testing.T) {
	ctx := context.Background()
	h := NewHost(nil)
	defer func() { _ = h.Close(ctx) }()

	const toolCallID = "call-cb"
	const toolName = "cbool"
	const toolResultStr = "callback-result"

	type callArgs struct {
		agentID string
		id      string
		name    string
		result  string
		isError bool
	}
	gotCh := make(chan callArgs, 1)
	h.SetUIBridge(&testUIBridge{onAfterToolCall: func(agentID, id, name, result string, isError bool) {
		gotCh <- callArgs{agentID, id, name, result, isError}
	}})

	h.mu.Lock()
	h.registeredTools[toolName] = sdk.Tool{Name: toolName}
	h.mu.Unlock()

	go func() {
		h.handleToolResult(sdk.HostCallRequest{
			Method: sdk.MethodToolResult,
			Params: []byte(`{"tool_call_id":"` + toolCallID + `","result":"` + toolResultStr + `","is_error":true}`),
		})
	}()

	_, err := h.ExecuteTool(ctx, "test-agent", toolCallID, toolName, []byte(`{}`))
	if err != nil {
		t.Fatalf("ExecuteTool: %v", err)
	}

	select {
	case got := <-gotCh:
		if got.agentID != "test-agent" {
			t.Errorf("OnAfterToolCall agentID: got %q, want %q", got.agentID, "test-agent")
		}
		if got.id != toolCallID {
			t.Errorf("OnAfterToolCall toolCallID: got %q, want %q", got.id, toolCallID)
		}
		if got.name != toolName {
			t.Errorf("OnAfterToolCall toolName: got %q, want %q", got.name, toolName)
		}
		if got.result != toolResultStr {
			t.Errorf("OnAfterToolCall result: got %q, want %q", got.result, toolResultStr)
		}
		if !got.isError {
			t.Error("OnAfterToolCall isError: got false, want true")
		}
	default:
		t.Fatal("OnAfterToolCall was not called")
	}
}

func TestHost_RegisteredTools_WithOwner(t *testing.T) {
	ctx := context.Background()
	h := NewHost(nil)
	defer func() { _ = h.Close(ctx) }()

	path := writeWASM(t, "minimal.wasm", minimalWASM)
	if err := h.Load(ctx, path); err != nil {
		t.Fatalf("Load: %v", err)
	}
	ext := h.extensions[0]

	resp := h.handleRegisterTool(ext, sdk.HostCallRequest{
		Method: sdk.MethodRegisterTool,
		Params: []byte(`{"name":"owned_tool","description":"owned","input_schema":{}}`),
	})
	if resp.Error != "" {
		t.Fatalf("handleRegisterTool: %s", resp.Error)
	}

	infos := h.RegisteredTools()
	var found *RegisteredToolInfo
	for i := range infos {
		if infos[i].Tool.Name == "owned_tool" {
			found = &infos[i]
			break
		}
	}
	if found == nil {
		t.Fatal("expected owned_tool in RegisteredTools output")
		return
	}
	if found.OwnerName != ext.name {
		t.Errorf("OwnerName: got %q, want %q", found.OwnerName, ext.name)
	}
}

func TestHost_GetRegisteredTools(t *testing.T) {
	ctx := context.Background()
	h := NewHost(nil)
	defer func() { _ = h.Close(ctx) }()

	path := writeWASM(t, "minimal.wasm", minimalWASM)
	if err := h.Load(ctx, path); err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Register two tools.
	h.handleRegisterTool(nil, sdk.HostCallRequest{
		Method: sdk.MethodRegisterTool,
		Params: []byte(`{"name":"tool_a","description":"a","input_schema":{}}`),
	})
	h.handleRegisterTool(nil, sdk.HostCallRequest{
		Method: sdk.MethodRegisterTool,
		Params: []byte(`{"name":"tool_b","description":"b","input_schema":{}}`),
	})

	tools := h.GetRegisteredTools()
	if len(tools) != 2 {
		t.Fatalf("expected 2 registered tools, got %d", len(tools))
	}

	names := make(map[string]bool)
	for _, tool := range tools {
		names[tool.Name] = true
	}
	if !names["tool_a"] || !names["tool_b"] {
		t.Error("expected tool_a and tool_b to be in registered tools")
	}
}

// --- Agent management handler tests ---

func TestHost_HandleAgentSpawn_CallbackInvoked(t *testing.T) {
	ctx := context.Background()
	h := NewHost(nil)
	defer func() { _ = h.Close(ctx) }()

	type spawnArgs struct {
		id, name, systemPrompt, modelName, initialPrompt string
		thinkingBudget                                   int
	}
	got := make(chan spawnArgs, 1)
	h.SetAgentBridge(&testAgentBridge{onSpawn: func(_ context.Context, req SpawnRequest) error {
		got <- spawnArgs{req.ID, req.Name, req.SystemPrompt, req.ModelName, req.InitialPrompt, req.ThinkingBudget}
		return nil
	}})

	path := writeWASM(t, "minimal.wasm", minimalWASM)
	if err := h.Load(ctx, path); err != nil {
		t.Fatalf("Load: %v", err)
	}
	ext := h.extensions[0]

	resp := h.routeHostCall(ctx, ext.module, ext, sdk.HostCallRequest{
		Method: sdk.MethodAgentSpawn,
		Params: []byte(`{"id":"a1","name":"worker","system_prompt":"you are helpful","model_name":"claude-3"}`),
	})
	if resp.Error != "" {
		t.Fatalf("agent_spawn: %s", resp.Error)
	}

	select {
	case args := <-got:
		if args.id != "a1" || args.name != "worker" || args.systemPrompt != "you are helpful" ||
			args.modelName != "claude-3" {
			t.Errorf(
				"AgentBridge.Spawn got %+v, want id=a1 name=worker systemPrompt='you are helpful' modelName=claude-3",
				args,
			)
		}
	default:
		t.Fatal("AgentBridge.Spawn was not called")
	}
}

func TestHost_HandleAgentSpawn_NilCallback(t *testing.T) {
	ctx := context.Background()
	h := NewHost(nil)
	defer func() { _ = h.Close(ctx) }()

	path := writeWASM(t, "minimal.wasm", minimalWASM)
	if err := h.Load(ctx, path); err != nil {
		t.Fatalf("Load: %v", err)
	}
	ext := h.extensions[0]

	resp := h.routeHostCall(ctx, ext.module, ext, sdk.HostCallRequest{
		Method: sdk.MethodAgentSpawn,
		Params: []byte(`{"id":"a1","name":"worker","system_prompt":"sp","model_name":"m"}`),
	})
	if resp.Error == "" {
		t.Fatal("expected error when OnAgentSpawn is nil")
	}
}

func TestHost_HandleAgentClose_CallbackInvoked(t *testing.T) {
	ctx := context.Background()
	h := NewHost(nil)
	defer func() { _ = h.Close(ctx) }()

	closedID := make(chan string, 1)
	h.SetAgentBridge(&testAgentBridge{onClose: func(id string) error {
		closedID <- id
		return nil
	}})

	path := writeWASM(t, "minimal.wasm", minimalWASM)
	if err := h.Load(ctx, path); err != nil {
		t.Fatalf("Load: %v", err)
	}
	ext := h.extensions[0]

	resp := h.routeHostCall(ctx, ext.module, ext, sdk.HostCallRequest{
		Method: sdk.MethodAgentClose,
		Params: []byte(`{"id":"a1"}`),
	})
	if resp.Error != "" {
		t.Fatalf("agent_close: %s", resp.Error)
	}

	select {
	case id := <-closedID:
		if id != "a1" {
			t.Errorf("AgentBridge.Close got id=%q, want a1", id)
		}
	default:
		t.Fatal("AgentBridge.Close was not called")
	}
}

func TestHost_HandleAgentSendMessage_CallbackInvoked(t *testing.T) {
	ctx := context.Background()
	h := NewHost(nil)
	defer func() { _ = h.Close(ctx) }()

	type msgArgs struct {
		id  string
		msg sdk.Message
	}
	got := make(chan msgArgs, 1)
	h.SetAgentBridge(&testAgentBridge{onSendMessage: func(id string, msg sdk.Message) error {
		got <- msgArgs{id, msg}
		return nil
	}})

	path := writeWASM(t, "minimal.wasm", minimalWASM)
	if err := h.Load(ctx, path); err != nil {
		t.Fatalf("Load: %v", err)
	}
	ext := h.extensions[0]

	resp := h.routeHostCall(ctx, ext.module, ext, sdk.HostCallRequest{
		Method: sdk.MethodAgentSendMessage,
		Params: []byte(`{"id":"a1","message":"hello agent"}`),
	})
	if resp.Error != "" {
		t.Fatalf("agent_send_message: %s", resp.Error)
	}

	select {
	case args := <-got:
		if args.id != "a1" || args.msg.Content != "hello agent" {
			t.Errorf(
				"AgentBridge.SendMessage got id=%q msg.Content=%q, want id=a1 message='hello agent'",
				args.id,
				args.msg.Content,
			)
		}
	default:
		t.Fatal("AgentBridge.SendMessage was not called")
	}
}

func TestHost_HandleAgentList_ReturnsAgents(t *testing.T) {
	ctx := context.Background()
	h := NewHost(nil)
	defer func() { _ = h.Close(ctx) }()

	h.SetAgentBridge(&testAgentBridge{onList: func() ([]AgentInfo, error) {
		return []AgentInfo{
			{ID: "a1", Name: "worker", IsRunning: true, PendingMessages: 2},
			{ID: "a2", Name: "reviewer"},
		}, nil
	}})

	path := writeWASM(t, "minimal.wasm", minimalWASM)
	if err := h.Load(ctx, path); err != nil {
		t.Fatalf("Load: %v", err)
	}
	ext := h.extensions[0]

	resp := h.routeHostCall(ctx, ext.module, ext, sdk.HostCallRequest{
		Method: sdk.MethodAgentList,
	})
	if resp.Error != "" {
		t.Fatalf("agent_list: %s", resp.Error)
	}
	if len(resp.Result) == 0 {
		t.Fatal("expected non-empty result from agent_list")
	}
	var out struct {
		Agents []AgentInfo `json:"agents"`
	}
	if err := json.Unmarshal(resp.Result, &out); err != nil {
		t.Fatalf("unmarshal agent_list result: %v", err)
	}
	if len(out.Agents) != 2 {
		t.Fatalf("agent_list returned %d agents, want 2", len(out.Agents))
	}
	if !out.Agents[0].IsRunning {
		t.Error("agent_list did not preserve is_running")
	}
	if out.Agents[0].PendingMessages != 2 {
		t.Errorf("pending_messages = %d, want 2", out.Agents[0].PendingMessages)
	}
}

func TestHost_HandleAgentTokenCount_ReturnsCount(t *testing.T) {
	ctx := context.Background()
	h := NewHost(nil)
	defer func() { _ = h.Close(ctx) }()

	h.SetAgentBridge(&testAgentBridge{onTokenCount: func() int64 { return 42 }})

	path := writeWASM(t, "minimal.wasm", minimalWASM)
	if err := h.Load(ctx, path); err != nil {
		t.Fatalf("Load: %v", err)
	}
	ext := h.extensions[0]

	resp := h.routeHostCall(ctx, ext.module, ext, sdk.HostCallRequest{
		Method: sdk.MethodAgentTokenCount,
	})
	if resp.Error != "" {
		t.Fatalf("agent_token_count: %s", resp.Error)
	}
	if len(resp.Result) == 0 {
		t.Fatal("expected non-empty result from agent_token_count")
	}
}

// --- Team management handler tests ---

func TestHost_HandleTeamCreate_CallbackInvoked(t *testing.T) {
	ctx := context.Background()
	h := NewHost(nil)
	defer func() { _ = h.Close(ctx) }()

	type createArgs struct{ id, name string }
	got := make(chan createArgs, 1)
	h.SetTeamBridge(&testTeamBridge{onCreate: func(id, name string) error {
		got <- createArgs{id, name}
		return nil
	}})

	path := writeWASM(t, "minimal.wasm", minimalWASM)
	if err := h.Load(ctx, path); err != nil {
		t.Fatalf("Load: %v", err)
	}
	ext := h.extensions[0]

	resp := h.routeHostCall(ctx, ext.module, ext, sdk.HostCallRequest{
		Method: sdk.MethodTeamCreate,
		Params: []byte(`{"id":"t1","name":"alpha"}`),
	})
	if resp.Error != "" {
		t.Fatalf("team_create: %s", resp.Error)
	}

	select {
	case args := <-got:
		if args.id != "t1" || args.name != "alpha" {
			t.Errorf("TeamBridge.Create got %+v, want id=t1 name=alpha", args)
		}
	default:
		t.Fatal("TeamBridge.Create was not called")
	}
}

func TestHost_HandleTeamClose_CallbackInvoked(t *testing.T) {
	ctx := context.Background()
	h := NewHost(nil)
	defer func() { _ = h.Close(ctx) }()

	closedID := make(chan string, 1)
	h.SetTeamBridge(&testTeamBridge{onClose: func(_ context.Context, id string) error {
		closedID <- id
		return nil
	}})

	path := writeWASM(t, "minimal.wasm", minimalWASM)
	if err := h.Load(ctx, path); err != nil {
		t.Fatalf("Load: %v", err)
	}
	ext := h.extensions[0]

	resp := h.routeHostCall(ctx, ext.module, ext, sdk.HostCallRequest{
		Method: sdk.MethodTeamClose,
		Params: []byte(`{"id":"t1"}`),
	})
	if resp.Error != "" {
		t.Fatalf("team_close: %s", resp.Error)
	}

	select {
	case id := <-closedID:
		if id != "t1" {
			t.Errorf("TeamBridge.Close got id=%q, want t1", id)
		}
	default:
		t.Fatal("TeamBridge.Close was not called")
	}
}

func TestHost_HandleTeamAddMember_CallbackInvoked(t *testing.T) {
	ctx := context.Background()
	h := NewHost(nil)
	defer func() { _ = h.Close(ctx) }()

	type addArgs struct{ teamID, agentID string }
	got := make(chan addArgs, 1)
	h.SetTeamBridge(&testTeamBridge{onAddMember: func(teamID, agentID string) error {
		got <- addArgs{teamID, agentID}
		return nil
	}})

	path := writeWASM(t, "minimal.wasm", minimalWASM)
	if err := h.Load(ctx, path); err != nil {
		t.Fatalf("Load: %v", err)
	}
	ext := h.extensions[0]

	resp := h.routeHostCall(ctx, ext.module, ext, sdk.HostCallRequest{
		Method: sdk.MethodTeamAddMember,
		Params: []byte(`{"team_id":"t1","agent_id":"a1"}`),
	})
	if resp.Error != "" {
		t.Fatalf("team_add_member: %s", resp.Error)
	}

	select {
	case args := <-got:
		if args.teamID != "t1" || args.agentID != "a1" {
			t.Errorf("TeamBridge.AddMember got %+v, want teamID=t1 agentID=a1", args)
		}
	default:
		t.Fatal("TeamBridge.AddMember was not called")
	}
}

func TestHost_HandleTeamRemoveMember_CallbackInvoked(t *testing.T) {
	ctx := context.Background()
	h := NewHost(nil)
	defer func() { _ = h.Close(ctx) }()

	type removeArgs struct{ teamID, agentID string }
	got := make(chan removeArgs, 1)
	h.SetTeamBridge(&testTeamBridge{onRemoveMember: func(teamID, agentID string) error {
		got <- removeArgs{teamID, agentID}
		return nil
	}})

	path := writeWASM(t, "minimal.wasm", minimalWASM)
	if err := h.Load(ctx, path); err != nil {
		t.Fatalf("Load: %v", err)
	}
	ext := h.extensions[0]

	resp := h.routeHostCall(ctx, ext.module, ext, sdk.HostCallRequest{
		Method: sdk.MethodTeamRemoveMember,
		Params: []byte(`{"team_id":"t1","agent_id":"a1"}`),
	})
	if resp.Error != "" {
		t.Fatalf("team_remove_member: %s", resp.Error)
	}

	select {
	case args := <-got:
		if args.teamID != "t1" || args.agentID != "a1" {
			t.Errorf("TeamBridge.RemoveMember got %+v, want teamID=t1 agentID=a1", args)
		}
	default:
		t.Fatal("TeamBridge.RemoveMember was not called")
	}
}

func TestHost_HandleTeamGetInfo_CallbackInvoked(t *testing.T) {
	ctx := context.Background()
	h := NewHost(nil)
	defer func() { _ = h.Close(ctx) }()

	h.SetTeamBridge(&testTeamBridge{onGetMembers: func(teamID string) ([]string, error) {
		if teamID == "team-1" {
			return []string{"agent-a", "agent-b"}, nil
		}
		return nil, fmt.Errorf("team not found: %s", teamID)
	}})

	path := writeWASM(t, "minimal.wasm", minimalWASM)
	if err := h.Load(ctx, path); err != nil {
		t.Fatalf("Load: %v", err)
	}
	ext := h.extensions[0]

	resp := h.routeHostCall(ctx, ext.module, ext, sdk.HostCallRequest{
		Method: sdk.MethodTeamGetInfo,
		Params: []byte(`{"team_id":"team-1"}`),
	})
	if resp.Error != "" {
		t.Fatalf("team_get_info: %s", resp.Error)
	}

	var result map[string]any
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if result["team_id"] != "team-1" {
		t.Errorf("team_id: got %v, want team-1", result["team_id"])
	}
	members, ok := result["members"].([]any)
	if !ok || len(members) != 2 {
		t.Errorf("members: got %v, want [agent-a agent-b]", result["members"])
	}
}

func TestHost_HandleTeamGetInfo_UnknownTeam_ReturnsError(t *testing.T) {
	ctx := context.Background()
	h := NewHost(nil)
	defer func() { _ = h.Close(ctx) }()

	h.SetTeamBridge(&testTeamBridge{onGetMembers: func(teamID string) ([]string, error) {
		return nil, fmt.Errorf("team not found: %s", teamID)
	}})

	path := writeWASM(t, "minimal.wasm", minimalWASM)
	if err := h.Load(ctx, path); err != nil {
		t.Fatalf("Load: %v", err)
	}
	ext := h.extensions[0]

	resp := h.routeHostCall(ctx, ext.module, ext, sdk.HostCallRequest{
		Method: sdk.MethodTeamGetInfo,
		Params: []byte(`{"team_id":"ghost"}`),
	})
	if resp.Error == "" {
		t.Fatal("expected error for unknown team, got nil")
	}
}

func TestHost_HandleTeamGetInfo_MissingTeamID_ReturnsError(t *testing.T) {
	ctx := context.Background()
	h := NewHost(nil)
	defer func() { _ = h.Close(ctx) }()

	h.SetTeamBridge(&testTeamBridge{onGetMembers: func(teamID string) ([]string, error) {
		return nil, nil
	}})

	path := writeWASM(t, "minimal.wasm", minimalWASM)
	if err := h.Load(ctx, path); err != nil {
		t.Fatalf("Load: %v", err)
	}
	ext := h.extensions[0]

	resp := h.routeHostCall(ctx, ext.module, ext, sdk.HostCallRequest{
		Method: sdk.MethodTeamGetInfo,
		Params: []byte(`{}`),
	})
	if resp.Error == "" {
		t.Fatal("expected error for missing team_id, got nil")
	}
}

func TestHost_HandleTeamList_CallbackInvoked(t *testing.T) {
	ctx := context.Background()
	h := NewHost(nil)
	defer func() { _ = h.Close(ctx) }()

	h.SetTeamBridge(&testTeamBridge{onList: func() ([]string, error) {
		return []string{"team-alpha", "team-beta"}, nil
	}})

	path := writeWASM(t, "minimal.wasm", minimalWASM)
	if err := h.Load(ctx, path); err != nil {
		t.Fatalf("Load: %v", err)
	}
	ext := h.extensions[0]

	resp := h.routeHostCall(ctx, ext.module, ext, sdk.HostCallRequest{
		Method: sdk.MethodTeamList,
	})
	if resp.Error != "" {
		t.Fatalf("team_list: %s", resp.Error)
	}

	var result map[string][]string
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	teams := result["teams"]
	if len(teams) != 2 {
		t.Errorf("teams: got %v, want [team-alpha team-beta]", teams)
	}
}

func TestHost_HandleTeamList_EmptyList(t *testing.T) {
	ctx := context.Background()
	h := NewHost(nil)
	defer func() { _ = h.Close(ctx) }()

	h.SetTeamBridge(&testTeamBridge{onList: func() ([]string, error) {
		return []string{}, nil
	}})

	path := writeWASM(t, "minimal.wasm", minimalWASM)
	if err := h.Load(ctx, path); err != nil {
		t.Fatalf("Load: %v", err)
	}
	ext := h.extensions[0]

	resp := h.routeHostCall(ctx, ext.module, ext, sdk.HostCallRequest{
		Method: sdk.MethodTeamList,
	})
	if resp.Error != "" {
		t.Fatalf("team_list: %s", resp.Error)
	}

	var result map[string][]string
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if len(result["teams"]) != 0 {
		t.Errorf("expected empty teams, got %v", result["teams"])
	}
}

func TestHost_HandleTeamGetInfo_NilCallback_ReturnsError(t *testing.T) {
	ctx := context.Background()
	h := NewHost(nil)
	defer func() { _ = h.Close(ctx) }()
	// OnTeamGetInfo not set

	path := writeWASM(t, "minimal.wasm", minimalWASM)
	if err := h.Load(ctx, path); err != nil {
		t.Fatalf("Load: %v", err)
	}
	ext := h.extensions[0]

	resp := h.routeHostCall(ctx, ext.module, ext, sdk.HostCallRequest{
		Method: sdk.MethodTeamGetInfo,
		Params: []byte(`{"team_id":"t1"}`),
	})
	if resp.Error == "" {
		t.Fatal("expected error when callback not set")
	}
}

func TestHost_HandleTeamList_NilCallback_ReturnsError(t *testing.T) {
	ctx := context.Background()
	h := NewHost(nil)
	defer func() { _ = h.Close(ctx) }()
	// OnTeamList not set

	path := writeWASM(t, "minimal.wasm", minimalWASM)
	if err := h.Load(ctx, path); err != nil {
		t.Fatalf("Load: %v", err)
	}
	ext := h.extensions[0]

	resp := h.routeHostCall(ctx, ext.module, ext, sdk.HostCallRequest{
		Method: sdk.MethodTeamList,
	})
	if resp.Error == "" {
		t.Fatal("expected error when callback not set")
	}
}

func TestHost_HandleTeamGetInfo_ReturnsMembers(t *testing.T) {
	ctx := context.Background()
	h := NewHost(nil)
	defer func() { _ = h.Close(ctx) }()

	h.SetTeamBridge(&testTeamBridge{onGetMembers: func(teamID string) ([]string, error) {
		return []string{"a1", "a2"}, nil
	}})

	path := writeWASM(t, "minimal.wasm", minimalWASM)
	if err := h.Load(ctx, path); err != nil {
		t.Fatalf("Load: %v", err)
	}
	ext := h.extensions[0]

	resp := h.routeHostCall(ctx, ext.module, ext, sdk.HostCallRequest{
		Method: sdk.MethodTeamGetInfo,
		Params: []byte(`{"team_id":"t1"}`),
	})
	if resp.Error != "" {
		t.Fatalf("team_get_info: %s", resp.Error)
	}

	var result map[string]any
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	members, ok := result["members"].([]any)
	if !ok {
		t.Fatalf("members field not a list: %v", result["members"])
	}
	if len(members) != 2 {
		t.Fatalf("expected 2 members, got %d", len(members))
	}
	ids := map[string]bool{}
	for _, m := range members {
		ids[m.(string)] = true
	}
	if !ids["a1"] || !ids["a2"] {
		t.Errorf("members: got %v, want [a1 a2]", members)
	}
}

func TestHost_HandleTeamList_ReturnsTeams(t *testing.T) {
	ctx := context.Background()
	h := NewHost(nil)
	defer func() { _ = h.Close(ctx) }()

	h.SetTeamBridge(&testTeamBridge{onList: func() ([]string, error) {
		return []string{"t1", "t2"}, nil
	}})

	path := writeWASM(t, "minimal.wasm", minimalWASM)
	if err := h.Load(ctx, path); err != nil {
		t.Fatalf("Load: %v", err)
	}
	ext := h.extensions[0]

	resp := h.routeHostCall(ctx, ext.module, ext, sdk.HostCallRequest{
		Method: sdk.MethodTeamList,
	})
	if resp.Error != "" {
		t.Fatalf("team_list: %s", resp.Error)
	}

	var result map[string][]string
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	teams := result["teams"]
	if len(teams) != 2 {
		t.Fatalf("expected 2 teams, got %d: %v", len(teams), teams)
	}
	ids := map[string]bool{}
	for _, id := range teams {
		ids[id] = true
	}
	if !ids["t1"] || !ids["t2"] {
		t.Errorf("teams: got %v, want [t1 t2]", teams)
	}
}

// --- Bug fix tests ---

func TestHost_HandleTeamCreate_ReturnsTeamID(t *testing.T) {
	// Bug 4 fix: handleTeamCreate must return {"team_id":"...","status":"created"}
	// not an empty response.
	ctx := context.Background()
	h := NewHost(nil)
	defer func() { _ = h.Close(ctx) }()

	h.SetTeamBridge(&testTeamBridge{onCreate: func(id, name string) error { return nil }})

	path := writeWASM(t, "minimal.wasm", minimalWASM)
	if err := h.Load(ctx, path); err != nil {
		t.Fatalf("Load: %v", err)
	}
	ext := h.extensions[0]

	resp := h.routeHostCall(ctx, ext.module, ext, sdk.HostCallRequest{
		Method: sdk.MethodTeamCreate,
		Params: []byte(`{"id":"team-xyz","name":"my team"}`),
	})
	if resp.Error != "" {
		t.Fatalf("team_create: %s", resp.Error)
	}

	var result map[string]string
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("unmarshal response: %v (raw: %s)", err, resp.Result)
	}
	if result["team_id"] != "team-xyz" {
		t.Errorf("team_id: got %q, want %q", result["team_id"], "team-xyz")
	}
	if result["status"] != "created" {
		t.Errorf("status: got %q, want %q", result["status"], "created")
	}
}

func TestHost_HandleAgentRun_CallbackInvoked(t *testing.T) {
	// Bug 3 fix: agent_run host_call must invoke OnAgentRun with the correct agent ID.
	ctx := context.Background()
	h := NewHost(nil)
	defer func() { _ = h.Close(ctx) }()

	var calledWith string
	h.SetAgentBridge(&testAgentBridge{onRun: func(id string) error {
		calledWith = id
		return nil
	}})

	path := writeWASM(t, "minimal.wasm", minimalWASM)
	if err := h.Load(ctx, path); err != nil {
		t.Fatalf("Load: %v", err)
	}
	ext := h.extensions[0]

	resp := h.routeHostCall(ctx, ext.module, ext, sdk.HostCallRequest{
		Method: sdk.MethodAgentRun,
		Params: []byte(`{"id":"worker-1"}`),
	})
	if resp.Error != "" {
		t.Fatalf("agent_run: %s", resp.Error)
	}
	if calledWith != "worker-1" {
		t.Errorf("AgentBridge.Run called with %q, want %q", calledWith, "worker-1")
	}
}

func TestHost_HandleAgentRun_NilCallback_ReturnsError(t *testing.T) {
	ctx := context.Background()
	h := NewHost(nil)
	defer func() { _ = h.Close(ctx) }()

	// OnAgentRun not set — should return a clear error, not panic.
	path := writeWASM(t, "minimal.wasm", minimalWASM)
	if err := h.Load(ctx, path); err != nil {
		t.Fatalf("Load: %v", err)
	}
	ext := h.extensions[0]

	resp := h.routeHostCall(ctx, ext.module, ext, sdk.HostCallRequest{
		Method: sdk.MethodAgentRun,
		Params: []byte(`{"id":"agent-x"}`),
	})
	if resp.Error == "" {
		t.Fatal("expected error when OnAgentRun is nil, got empty")
	}
}

func TestHost_CapabilityProvider_Exec_AcceptsContext(t *testing.T) {
	called := false
	h := NewHost(nil)
	ctx := context.Background()
	defer func() { _ = h.Close(ctx) }()

	path := writeWASM(t, "minimal.wasm", minimalWASM)
	if err := h.Load(ctx, path); err != nil {
		t.Fatalf("Load: %v", err)
	}
	ext := h.extensions[0]
	ext.permissions[sdk.PermExec] = true // grant exec

	h.SetCapabilities(
		&testCapabilityProvider{
			onExec: func(_ctx context.Context, _command, _dir string, _onLine func(string)) (string, error) {
				called = true
				return "ok", nil
			},
		},
	)

	resp := h.routeHostCall(ctx, ext.module, ext, sdk.HostCallRequest{
		Method: sdk.MethodExec,
		Params: []byte(`{"command":"echo test","dir":""}`),
	})
	if resp.Error != "" {
		t.Fatalf("exec: %s", resp.Error)
	}
	if !called {
		t.Fatal("CapabilityProvider.Exec was not called")
	}
}

func TestHost_HTTPGet_PermissionDenied(t *testing.T) {
	ctx := context.Background()
	h := NewHost(nil)
	defer func() { _ = h.Close(ctx) }()

	path := writeWASM(t, "minimal.wasm", minimalWASM)
	if err := h.Load(ctx, path); err != nil {
		t.Fatalf("Load: %v", err)
	}
	ext := h.extensions[0]
	ext.trusted = false // enforce permission check; no network_read granted

	h.SetCapabilities(&testCapabilityProvider{})

	resp := h.routeHostCall(ctx, ext.module, ext, sdk.HostCallRequest{
		Method: sdk.MethodHTTPGet,
		Params: []byte(`{"url":"http://example.com"}`),
	})
	if resp.Error == "" {
		t.Fatal("expected permission denied for http_get without network_read")
	}
}

// TestHost_Guardrail_FileWriteOnlyExtension exercises the least-privilege
// guardrail for built-ins: an extension granted only file_write (like the
// logging built-in) must be denied exec, read_file, http_post, and http_get —
// a file-writing extension must not gain network or exec access.
func TestHost_Guardrail_FileWriteOnlyExtension(t *testing.T) {
	ctx := context.Background()
	h := NewHost(nil)
	defer func() { _ = h.Close(ctx) }()

	// Loaded as a trusted built-in with only file_write declared (least privilege).
	if err := h.LoadBytes(ctx, "logging.wasm", minimalWASM, true, sdk.PermFileWrite); err != nil {
		t.Fatalf("LoadBytes: %v", err)
	}
	ext := h.extensions[0]
	h.SetCapabilities(&testCapabilityProvider{})

	// append_file must succeed (file_write granted).
	if resp := h.routeHostCall(ctx, ext.module, ext, sdk.HostCallRequest{
		Method: sdk.MethodAppendFile,
		Params: []byte(`{"path":"/tmp/log","content":"x"}`),
	}); resp.Error != "" {
		t.Fatalf("append_file should be permitted with file_write: %s", resp.Error)
	}

	// Exec, read_file, http_post, http_get must all be denied.
	for _, tc := range []struct {
		name   string
		method string
		params string
	}{
		{"exec", sdk.MethodExec, `{"command":"ls"}`},
		{"read_file", sdk.MethodReadFile, `{"path":"/tmp/secret"}`},
		{"http_post", sdk.MethodHTTPPost, `{"url":"http://example.com","body":"x"}`},
		{"http_get", sdk.MethodHTTPGet, `{"url":"http://example.com"}`},
	} {
		resp := h.routeHostCall(ctx, ext.module, ext, sdk.HostCallRequest{
			Method: tc.method,
			Params: []byte(tc.params),
		})
		if resp.Error == "" {
			t.Errorf("file_write-only extension must be denied %s", tc.name)
		}
	}
}

// TestHost_Guardrail_FileReadWriteExtension has no network or exec access: an
// extension granted file_read+file_write must be denied http_post, http_get, exec.
func TestHost_Guardrail_FileReadWriteExtension(t *testing.T) {
	ctx := context.Background()
	h := NewHost(nil)
	defer func() { _ = h.Close(ctx) }()

	if err := h.LoadBytes(ctx, "file.wasm", minimalWASM, true, sdk.PermFileRead, sdk.PermFileWrite); err != nil {
		t.Fatalf("LoadBytes: %v", err)
	}
	ext := h.extensions[0]
	h.SetCapabilities(&testCapabilityProvider{})

	// read_file and append_file permitted.
	for _, tc := range []struct {
		name   string
		method string
		params string
	}{
		{"read_file", sdk.MethodReadFile, `{"path":"/tmp/secret"}`},
		{"append_file", sdk.MethodAppendFile, `{"path":"/tmp/log","content":"x"}`},
	} {
		resp := h.routeHostCall(ctx, ext.module, ext, sdk.HostCallRequest{
			Method: tc.method,
			Params: []byte(tc.params),
		})
		if resp.Error != "" {
			t.Errorf("%s should be permitted with file_read+file_write: %s", tc.name, resp.Error)
		}
	}

	// exec, http_post, http_get denied.
	for _, tc := range []struct {
		name   string
		method string
		params string
	}{
		{"exec", sdk.MethodExec, `{"command":"ls"}`},
		{"http_post", sdk.MethodHTTPPost, `{"url":"http://example.com","body":"x"}`},
		{"http_get", sdk.MethodHTTPGet, `{"url":"http://example.com"}`},
	} {
		resp := h.routeHostCall(ctx, ext.module, ext, sdk.HostCallRequest{
			Method: tc.method,
			Params: []byte(tc.params),
		})
		if resp.Error == "" {
			t.Errorf("file extension must be denied %s", tc.name)
		}
	}
}

func TestHost_HTTPGet_DispatchInvoked(t *testing.T) {
	ctx := context.Background()
	h := NewHost(nil)
	defer func() { _ = h.Close(ctx) }()

	path := writeWASM(t, "minimal.wasm", minimalWASM)
	if err := h.Load(ctx, path); err != nil {
		t.Fatalf("Load: %v", err)
	}
	ext := h.extensions[0]
	ext.permissions[sdk.PermNetworkRead] = true // grant network_read

	called := false
	h.SetCapabilities(
		&testCapabilityProvider{
			onHTTPGet: func(url string, headers map[string]string) (int, []byte, error) {
				called = true
				return 200, []byte("<html>ok</html>"), nil
			},
		},
	)

	resp := h.routeHostCall(ctx, ext.module, ext, sdk.HostCallRequest{
		Method: sdk.MethodHTTPGet,
		Params: []byte(`{"url":"http://example.com","headers":{"Accept":"text/html"}}`),
	})
	if resp.Error != "" {
		t.Fatalf("http_get: %s", resp.Error)
	}
	if !called {
		t.Fatal("CapabilityProvider.HTTPGet was not called")
	}
}

func TestHost_HTTPGet_NetworkReadGranted(t *testing.T) {
	ctx := context.Background()
	h := NewHost(nil)
	defer func() { _ = h.Close(ctx) }()

	path := writeWASM(t, "minimal.wasm", minimalWASM)
	if err := h.Load(ctx, path); err != nil {
		t.Fatalf("Load: %v", err)
	}
	ext := h.extensions[0]
	ext.trusted = false
	ext.permissions[sdk.PermNetworkRead] = true

	called := false
	h.SetCapabilities(
		&testCapabilityProvider{
			onHTTPGet: func(url string, headers map[string]string) (int, []byte, error) {
				called = true
				return 200, []byte("body"), nil
			},
		},
	)

	resp := h.routeHostCall(ctx, ext.module, ext, sdk.HostCallRequest{
		Method: sdk.MethodHTTPGet,
		Params: []byte(`{"url":"http://example.com"}`),
	})
	if resp.Error != "" {
		t.Fatalf("http_get: %s", resp.Error)
	}
	if !called {
		t.Fatal("CapabilityProvider.HTTPGet was not called despite network_read permission")
	}
}

func TestHost_UIMethods_Dispatch(t *testing.T) {
	h := NewHost(nil)
	ctx := context.Background()
	defer func() { _ = h.Close(ctx) }()

	path := writeWASM(t, "minimal.wasm", minimalWASM)
	if err := h.Load(ctx, path); err != nil {
		t.Fatalf("Load: %v", err)
	}
	ext := h.extensions[0]
	ext.permissions[sdk.PermUI] = true // grant ui

	ui := &testUIBridge{}
	h.SetUIBridge(ui)

	if resp := h.routeHostCall(ctx, ext.module, ext, sdk.HostCallRequest{
		Method: sdk.MethodUICreateArea,
		Params: []byte(`{"area":{"id":"chat","placement":"main"}}`),
	}); resp.Error != "" {
		t.Fatalf("ui_create_area: %s", resp.Error)
	}
	if resp := h.routeHostCall(ctx, ext.module, ext, sdk.HostCallRequest{
		Method: sdk.MethodUIPatch,
		Params: []byte(`{"area":"chat","ops":[{"op":"set_root","node":{"id":"r","type":"text","text":"hi"}}]}`),
	}); resp.Error != "" {
		t.Fatalf("ui_patch: %s", resp.Error)
	}
	if resp := h.routeHostCall(ctx, ext.module, ext, sdk.HostCallRequest{
		Method: sdk.MethodUIRemoveArea,
		Params: []byte(`{"area":"chat"}`),
	}); resp.Error != "" {
		t.Fatalf("ui_remove_area: %s", resp.Error)
	}

	if len(ui.createdAreas) != 1 || ui.createdAreas[0] != "chat" {
		t.Fatalf("CreateArea not called: %v", ui.createdAreas)
	}
	if len(ui.patchedAreas) != 1 || ui.patchedAreas[0] != "chat" {
		t.Fatalf("PatchUI not called: %v", ui.patchedAreas)
	}
	if len(ui.removedAreas) != 1 || ui.removedAreas[0] != "chat" {
		t.Fatalf("RemoveArea not called: %v", ui.removedAreas)
	}

	// ui_update_area must route to UIBridge.UpdateArea.
	if resp := h.routeHostCall(ctx, ext.module, ext, sdk.HostCallRequest{
		Method: sdk.MethodUIUpdateArea,
		Params: []byte(`{"id":"chat","max_height":"5"}`),
	}); resp.Error != "" {
		t.Fatalf("ui_update_area: %s", resp.Error)
	}
}

func TestHost_UIMethods_PermissionDenied(t *testing.T) {
	h := NewHost(nil)
	ctx := context.Background()
	defer func() { _ = h.Close(ctx) }()

	path := writeWASM(t, "minimal.wasm", minimalWASM)
	if err := h.Load(ctx, path); err != nil {
		t.Fatalf("Load: %v", err)
	}
	ext := h.extensions[0]
	ext.trusted = false // enforce permission check; no ui permission granted

	h.SetUIBridge(&testUIBridge{})

	for _, tc := range []struct {
		name   string
		method string
		params string
	}{
		{"ui_patch", sdk.MethodUIPatch, `{"area":"chat","ops":[]}`},
		{"ui_create_area", sdk.MethodUICreateArea, `{"area":{"id":"x","placement":"main"}}`},
		{"ui_remove_area", sdk.MethodUIRemoveArea, `{"area":"x"}`},
		{"ui_update_area", sdk.MethodUIUpdateArea, `{"id":"x","max_height":"5"}`},
	} {
		resp := h.routeHostCall(ctx, ext.module, ext, sdk.HostCallRequest{
			Method: tc.method,
			Params: []byte(tc.params),
		})
		if resp.Error == "" {
			t.Fatalf("%s: expected permission denied", tc.name)
		}
	}
}

func TestSessionPreview_NormalizesWhitespace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	content := `{"type":"session","id":"x","timestamp":"2026-01-01T00:00:00Z","cwd":"/"}
{"type":"message","role":"user","content":"  \n  hello there  "}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got := sessionPreview(path); got != "hello there" {
		t.Errorf("sessionPreview = %q, want %q", got, "hello there")
	}

	// Whitespace-only user message yields no preview.
	content = `{"type":"session","id":"x","timestamp":"2026-01-01T00:00:00Z","cwd":"/"}
{"type":"message","role":"user","content":"\n\n\n"}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got := sessionPreview(path); got != "" {
		t.Errorf("sessionPreview = %q, want empty", got)
	}
}
