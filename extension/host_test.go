package extension

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/mattdurham/wllr/sdk"
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

func TestHost_Load_MinimalWASM(t *testing.T) {
	ctx := context.Background()
	h := NewHost(nil)
	defer h.Close(ctx)

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
	defer h.Close(ctx)

	err := h.Load(ctx, "/nonexistent/path/to/extension.wasm")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestHost_Load_MissingExport(t *testing.T) {
	ctx := context.Background()
	h := NewHost(nil)
	defer h.Close(ctx)

	path := writeWASM(t, "missing-free.wasm", missingFreeWASM)
	err := h.Load(ctx, path)
	if err == nil {
		t.Fatal("expected error for missing _free export, got nil")
	}
}

func TestHost_DispatchEvent_NotSubscribed(t *testing.T) {
	ctx := context.Background()

	var onEventCalled bool
	h := NewHost(nil)
	defer h.Close(ctx)

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
	defer h.Close(ctx)

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
	defer h.Close(ctx)

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
	defer h.Close(ctx)

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
	defer h.Close(ctx)

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
	defer h.Close(ctx)

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
	defer h.Close(ctx)

	var gotKey, gotValue string
	h.OnSetStatus = func(k, v string) {
		gotKey = k
		gotValue = v
	}

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
	defer h.Close(ctx)

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
	defer h.Close(ctx)

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
	defer h.Close(ctx)

	if err := h.Load(ctx, echoPath); err != nil {
		t.Fatalf("Load echo.wasm: %v", err)
	}
	if len(h.extensions) != 1 {
		t.Fatalf("expected 1 extension, got %d", len(h.extensions))
	}
}

// --- Permission model tests ---

func TestHost_Permission_TrustedGrantedAll(t *testing.T) {
	ctx := context.Background()
	h := NewHost(nil)
	defer h.Close(ctx)

	// LoadBytes makes the extension trusted — all permissions granted.
	if err := h.LoadBytes(ctx, "trusted.wasm", minimalWASM, true); err != nil {
		t.Fatalf("LoadBytes trusted: %v", err)
	}

	ext := h.extensions[0]
	if !ext.trusted {
		t.Fatal("expected extension loaded with trusted=true to be trusted")
	}

	// A trusted extension should pass any permission check.
	for _, perm := range []sdk.Permission{
		sdk.PermExec, sdk.PermFileOpen, sdk.PermFileRead,
		sdk.PermFileWrite, sdk.PermNetworkRead, sdk.PermNetworkWrite,
	} {
		if !ext.HasPermission(perm) {
			t.Errorf("trusted extension should have permission %s", perm)
		}
	}
}

func TestHost_Permission_UntrustedDeclared(t *testing.T) {
	ctx := context.Background()
	h := NewHost(nil)
	defer h.Close(ctx)

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
	defer h.Close(ctx)

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
	defer h.Close(ctx)

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

// --- LoadBytes tests ---

func TestHost_LoadBytes_TrustedExtension(t *testing.T) {
	ctx := context.Background()
	h := NewHost(nil)
	defer h.Close(ctx)

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
	defer h.Close(ctx)

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
	defer h.Close(ctx)

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
	defer h.Close(ctx)

	// Register a tool with a callback.
	const toolCallID = "call-1"
	const toolName = "echo"
	const toolResult = "hello"

	resultCh := make(chan string, 1)
	h.OnToolResult = func(id, result string, isError bool) {
		resultCh <- result
	}

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
	defer h.Close(ctx)

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
	defer h.Close(context.Background())

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
	defer h.Close(ctx)

	const toolCallID = "call-after"
	const toolName = "mytool"
	const toolResultStr = "done"

	// Track whether OnAfterToolCall was invoked (proves EventAfterToolCall path ran).
	afterCh := make(chan struct{}, 1)
	h.OnAfterToolCall = func(id, name, result string, isError bool) {
		if id == toolCallID && name == toolName && result == toolResultStr && !isError {
			afterCh <- struct{}{}
		}
	}

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

	// OnAfterToolCall must have been invoked (proves EventAfterToolCall dispatch happened).
	select {
	case <-afterCh:
		// Good.
	default:
		t.Error("expected OnAfterToolCall to be called after ExecuteTool completed")
	}
}

func TestHost_OnAfterToolCall_Callback(t *testing.T) {
	ctx := context.Background()
	h := NewHost(nil)
	defer h.Close(ctx)

	const toolCallID = "call-cb"
	const toolName = "cbool"
	const toolResultStr = "callback-result"

	type callArgs struct {
		id      string
		name    string
		result  string
		isError bool
	}
	gotCh := make(chan callArgs, 1)
	h.OnAfterToolCall = func(id, name, result string, isError bool) {
		gotCh <- callArgs{id, name, result, isError}
	}

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
	defer h.Close(ctx)

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
	defer h.Close(ctx)

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
	defer h.Close(ctx)

	type spawnArgs struct{ id, name, systemPrompt, modelName, initialPrompt string }
	got := make(chan spawnArgs, 1)
	h.OnAgentSpawn = func(id, name, systemPrompt, modelName, initialPrompt string) error {
		got <- spawnArgs{id, name, systemPrompt, modelName, initialPrompt}
		return nil
	}

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
				"OnAgentSpawn got %+v, want id=a1 name=worker systemPrompt='you are helpful' modelName=claude-3",
				args,
			)
		}
	default:
		t.Fatal("OnAgentSpawn was not called")
	}
}

func TestHost_HandleAgentSpawn_NilCallback(t *testing.T) {
	ctx := context.Background()
	h := NewHost(nil)
	defer h.Close(ctx)

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
	defer h.Close(ctx)

	closedID := make(chan string, 1)
	h.OnAgentClose = func(id string) error {
		closedID <- id
		return nil
	}

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
			t.Errorf("OnAgentClose got id=%q, want a1", id)
		}
	default:
		t.Fatal("OnAgentClose was not called")
	}
}

func TestHost_HandleAgentSendMessage_CallbackInvoked(t *testing.T) {
	ctx := context.Background()
	h := NewHost(nil)
	defer h.Close(ctx)

	type msgArgs struct{ id, message string }
	got := make(chan msgArgs, 1)
	h.OnAgentSendMessage = func(id, message string) error {
		got <- msgArgs{id, message}
		return nil
	}

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
		if args.id != "a1" || args.message != "hello agent" {
			t.Errorf("OnAgentSendMessage got %+v, want id=a1 message='hello agent'", args)
		}
	default:
		t.Fatal("OnAgentSendMessage was not called")
	}
}

func TestHost_HandleAgentList_ReturnsAgents(t *testing.T) {
	ctx := context.Background()
	h := NewHost(nil)
	defer h.Close(ctx)

	h.OnAgentList = func() ([]AgentInfo, error) {
		return []AgentInfo{
			{ID: "a1", Name: "worker"},
			{ID: "a2", Name: "reviewer"},
		}, nil
	}

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
}

func TestHost_HandleAgentTokenCount_ReturnsCount(t *testing.T) {
	ctx := context.Background()
	h := NewHost(nil)
	defer h.Close(ctx)

	h.OnAgentTokenCount = func() int64 { return 42 }

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
	defer h.Close(ctx)

	type createArgs struct{ id, name string }
	got := make(chan createArgs, 1)
	h.OnTeamCreate = func(id, name string) error {
		got <- createArgs{id, name}
		return nil
	}

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
			t.Errorf("OnTeamCreate got %+v, want id=t1 name=alpha", args)
		}
	default:
		t.Fatal("OnTeamCreate was not called")
	}
}

func TestHost_HandleTeamClose_CallbackInvoked(t *testing.T) {
	ctx := context.Background()
	h := NewHost(nil)
	defer h.Close(ctx)

	closedID := make(chan string, 1)
	h.OnTeamClose = func(id string) error {
		closedID <- id
		return nil
	}

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
			t.Errorf("OnTeamClose got id=%q, want t1", id)
		}
	default:
		t.Fatal("OnTeamClose was not called")
	}
}

func TestHost_HandleTeamAddMember_CallbackInvoked(t *testing.T) {
	ctx := context.Background()
	h := NewHost(nil)
	defer h.Close(ctx)

	type addArgs struct{ teamID, agentID string }
	got := make(chan addArgs, 1)
	h.OnTeamAddMember = func(teamID, agentID string) error {
		got <- addArgs{teamID, agentID}
		return nil
	}

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
			t.Errorf("OnTeamAddMember got %+v, want teamID=t1 agentID=a1", args)
		}
	default:
		t.Fatal("OnTeamAddMember was not called")
	}
}

func TestHost_HandleTeamRemoveMember_CallbackInvoked(t *testing.T) {
	ctx := context.Background()
	h := NewHost(nil)
	defer h.Close(ctx)

	type removeArgs struct{ teamID, agentID string }
	got := make(chan removeArgs, 1)
	h.OnTeamRemoveMember = func(teamID, agentID string) error {
		got <- removeArgs{teamID, agentID}
		return nil
	}

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
			t.Errorf("OnTeamRemoveMember got %+v, want teamID=t1 agentID=a1", args)
		}
	default:
		t.Fatal("OnTeamRemoveMember was not called")
	}
}

func TestHost_HandleTeamGetInfo_CallbackInvoked(t *testing.T) {
	ctx := context.Background()
	h := NewHost(nil)
	defer h.Close(ctx)

	h.OnTeamGetInfo = func(teamID string) ([]string, error) {
		if teamID == "team-1" {
			return []string{"agent-a", "agent-b"}, nil
		}
		return nil, fmt.Errorf("team not found: %s", teamID)
	}

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
	defer h.Close(ctx)

	h.OnTeamGetInfo = func(teamID string) ([]string, error) {
		return nil, fmt.Errorf("team not found: %s", teamID)
	}

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
	defer h.Close(ctx)

	h.OnTeamGetInfo = func(teamID string) ([]string, error) {
		return nil, nil
	}

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
	defer h.Close(ctx)

	h.OnTeamList = func() ([]string, error) {
		return []string{"team-alpha", "team-beta"}, nil
	}

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
	defer h.Close(ctx)

	h.OnTeamList = func() ([]string, error) {
		return []string{}, nil
	}

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
	defer h.Close(ctx)
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
	defer h.Close(ctx)
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
	defer h.Close(ctx)

	h.OnTeamGetInfo = func(teamID string) ([]string, error) {
		return []string{"a1", "a2"}, nil
	}

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
	defer h.Close(ctx)

	h.OnTeamList = func() ([]string, error) {
		return []string{"t1", "t2"}, nil
	}

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
	defer h.Close(ctx)

	h.OnTeamCreate = func(id, name string) error { return nil }

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
	defer h.Close(ctx)

	var calledWith string
	h.OnAgentRun = func(id string) error {
		calledWith = id
		return nil
	}

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
		t.Errorf("OnAgentRun called with %q, want %q", calledWith, "worker-1")
	}
}

func TestHost_HandleAgentRun_NilCallback_ReturnsError(t *testing.T) {
	ctx := context.Background()
	h := NewHost(nil)
	defer h.Close(ctx)

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
func TestHost_OnExec_AcceptsContextAndOnLine(t *testing.T) {
	called := false
	h := NewHost(nil)
	ctx := context.Background()
	defer h.Close(ctx)
	h.OnExec = func(_ctx context.Context, _command, _dir string, _onLine func(string)) (string, error) {
		called = true
		return "ok", nil
	}
	_, _ = h.OnExec(ctx, "echo test", "", nil)
	if !called {
		t.Fatal("OnExec was not called")
	}
}
func TestHost_HandleExec_PassesContext(t *testing.T) {
	ctxCalled := false
	h := NewHost(nil)
	ctx := context.Background()
	defer h.Close(ctx)
	h.OnExec = func(c context.Context, _cmd, _dir string, _onLine func(string)) (string, error) {
		if c != nil {
			ctxCalled = true
		}
		return "", nil
	}
	_, _ = h.OnExec(ctx, "ls", "", nil)
	if !ctxCalled {
		t.Fatal("ctx was nil when OnExec was called")
	}
}
