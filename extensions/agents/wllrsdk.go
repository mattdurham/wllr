//go:build wasip1

// wllrsdk.go — copy this file into your extension directory.
//
// It provides the complete wllr WASM extension API: the ABI boilerplate,
// all host_call wrappers, and an event-handler registration system.
// Your extension only needs to register handlers and provide func main(){}.
//
// Minimal example:
//
//	package main
//
//	import "encoding/json"
//
//	func init() {
//	    RegisterTool("greet", "Say hello", json.RawMessage(`{"type":"object","properties":{}}`))
//	    OnToolCall(func(callID, name string, input json.RawMessage) (string, bool) {
//	        if name != "greet" { return "", false }
//	        return `"Hello from my extension!"`, false
//	    })
//	}
//
//	func main() {}
package main

import (
	"encoding/json"
	"fmt"
	"unsafe"
)

// ─── Host imports ─────────────────────────────────────────────────────────────

//go:wasmimport env host_log
func _sdkHostLog(level, ptr, length uint32)

//go:wasmimport env host_call
func _sdkHostCall(reqPtr, reqLen, respPtrPtr, respLenPtr uint32) uint32

// ─── Memory management ────────────────────────────────────────────────────────

var _sdkPinned = map[uintptr][]byte{}

//go:wasmexport _alloc
func _sdkAlloc(size int32) int32 {
	if size <= 0 {
		return 0
	}
	buf := make([]byte, size)
	ptr := uintptr(unsafe.Pointer(&buf[0]))
	_sdkPinned[ptr] = buf
	return int32(ptr)
}

//go:wasmexport _free
func _sdkFree(ptr int32) {
	delete(_sdkPinned, uintptr(ptr))
}

// ─── Required exports ─────────────────────────────────────────────────────────

//go:wasmexport _init
func _sdkInit() int32 {
	// Subscribe to every event that has a registered handler.
	for evt := range _sdkHandlers {
		_sdkCall("subscribe", map[string]string{"event": evt})
	}
	// Run deferred init hooks (RegisterTool, RegisterCommand calls).
	for _, fn := range _sdkInitHooks {
		fn()
	}
	return 0
}

//go:wasmexport _on_event
func _sdkOnEvent(ptr, length int32) int32 {
	data := unsafe.Slice((*byte)(unsafe.Pointer(uintptr(ptr))), length)
	var evt struct {
		Type    string          `json:"type"`
		Payload json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(data, &evt); err != nil {
		return 0
	}
	for _, fn := range _sdkHandlers[evt.Type] {
		fn(evt.Payload)
	}
	return 0
}

// ─── Internal registry ────────────────────────────────────────────────────────

var (
	_sdkHandlers  = map[string][]func(json.RawMessage){}
	_sdkInitHooks []func()
)

func _sdkOn(event string, fn func(json.RawMessage)) {
	_sdkHandlers[event] = append(_sdkHandlers[event], fn)
}

// ─── Tool and command registration ───────────────────────────────────────────

// RegisterTool registers a tool the LLM can call.
// inputSchema is a JSON Schema object describing the tool's parameters.
func RegisterTool(name, description string, inputSchema json.RawMessage) {
	RegisterToolWithOutput(name, description, inputSchema, nil)
}

// RegisterToolWithOutput registers a tool with input and output JSON schemas.
// outputSchema documents the tool result returned via tool_result.
func RegisterToolWithOutput(name, description string, inputSchema, outputSchema json.RawMessage) {
	_sdkInitHooks = append(_sdkInitHooks, func() {
		params := map[string]any{
			"name":         name,
			"description":  description,
			"input_schema": inputSchema,
		}
		if len(outputSchema) > 0 {
			params["output_schema"] = outputSchema
		}
		_sdkCall("register_tool", params)
	})
}

// RegisterCommand registers a slash command the user can type.
func RegisterCommand(name, description string) {
	_sdkInitHooks = append(_sdkInitHooks, func() {
		_sdkCall("register_command", map[string]string{
			"name":        name,
			"description": description,
		})
	})
}

// RegisterCommandInstant registers a slash command that is dispatched instantly,
// bypassing the WASM dispatch queue. Use for commands that should respond without delay.
func RegisterCommandInstant(name, description string) {
	_sdkInitHooks = append(_sdkInitHooks, func() {
		_sdkCall("register_command", map[string]any{
			"name":        name,
			"description": description,
			"instant":     true,
		})
	})
}

// ─── Event handler registration ───────────────────────────────────────────────

// OnToolCall registers a handler for tool calls.
// Return a non-empty result string to respond; return ("", false) to pass through.
// Return ("", true) to signal an error with no message.
func OnToolCall(fn func(callID, toolName string, input json.RawMessage) (result string, isError bool)) {
	_sdkOn("before_tool_call", func(payload json.RawMessage) {
		var p struct {
			ToolCallID string          `json:"tool_call_id"`
			ToolName   string          `json:"tool_name"`
			Input      json.RawMessage `json:"input"`
		}
		if err := json.Unmarshal(payload, &p); err != nil {
			return
		}
		result, isError := fn(p.ToolCallID, p.ToolName, p.Input)
		if result == "" && !isError {
			return // not our tool, let it pass through
		}
		ToolResult(p.ToolCallID, result, isError)
	})
}

// OnCommand registers a handler for a specific slash command.
func OnCommand(name string, fn func(args []string)) {
	_sdkOn("on_command", func(payload json.RawMessage) {
		var p struct {
			Name string   `json:"name"`
			Args []string `json:"args"`
		}
		if err := json.Unmarshal(payload, &p); err != nil || p.Name != name {
			return
		}
		fn(p.Args)
	})
}

// OnSessionStart registers a handler called when a new session begins.
func OnSessionStart(fn func()) {
	_sdkOn("session_start", func(_ json.RawMessage) { fn() })
}

// OnShutdown registers a handler called when the host is shutting down.
func OnShutdown(fn func(reason string)) {
	_sdkOn("shutdown", func(payload json.RawMessage) {
		var p struct {
			Reason string `json:"reason"`
		}
		if err := json.Unmarshal(payload, &p); err == nil {
			fn(p.Reason)
		}
	})
}

// OnBeforeAgentStart registers a handler called before the agent processes
// each user message. prompt is the user's message text.
func OnBeforeAgentStart(fn func(prompt string, queued bool)) {
	_sdkOn("before_agent_start", func(payload json.RawMessage) {
		var p struct {
			Prompt string `json:"prompt"`
			Queued bool   `json:"queued"`
		}
		if err := json.Unmarshal(payload, &p); err == nil && p.Prompt != "" {
			fn(p.Prompt, p.Queued)
		}
	})
}

// OnMessageEnd registers a handler called when a message completes streaming.
// role is "user" or "assistant".
func OnMessageEnd(fn func(role, content string)) {
	_sdkOn("message_end", func(payload json.RawMessage) {
		var p struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}
		if err := json.Unmarshal(payload, &p); err == nil && p.Content != "" {
			fn(p.Role, p.Content)
		}
	})
}

// OnBeforeToolCall registers a raw handler for the before_tool_call event.
// Prefer OnToolCall for most cases.
func OnBeforeToolCall(fn func(payload json.RawMessage)) {
	_sdkOn("before_tool_call", fn)
}

// OnAfterToolCall registers a handler called after a tool call completes.
func OnAfterToolCall(fn func(callID, toolName, result string, isError bool)) {
	_sdkOn("after_tool_call", func(payload json.RawMessage) {
		var p struct {
			ToolCallID string `json:"tool_call_id"`
			ToolName   string `json:"tool_name"`
			Result     string `json:"result"`
			IsError    bool   `json:"is_error"`
		}
		if err := json.Unmarshal(payload, &p); err == nil {
			fn(p.ToolCallID, p.ToolName, p.Result, p.IsError)
		}
	})
}

// ─── Host API ─────────────────────────────────────────────────────────────────

// ToolResult sends the result of a tool call back to the host.
// Call this from an OnToolCall handler or from OnBeforeToolCall.
func ToolResult(callID, result string, isError bool) {
	_sdkCall("tool_result", map[string]any{
		"tool_call_id": callID,
		"result":       result,
		"is_error":     isError,
	})
}

// Modal displays text in a fullscreen modal overlay (read-only, scrollable).
func Modal(text string) {
	_sdkCall("modal", map[string]string{"text": text})
}

// Notify displays a brief notification in the chat area.
func Notify(text string) {
	_sdkCall("notify", map[string]string{"text": text})
}

// SetStatus sets a keyed value in the status bar (e.g. "my-ext", "ready").
func SetStatus(key, value string) {
	_sdkCall("set_status", map[string]string{"key": key, "value": value})
}

// SetSystemPrompt replaces the base system prompt for all agents.
func SetSystemPrompt(prompt string) {
	_sdkCall("set_system_prompt", map[string]string{"prompt": prompt})
}

// AppendSystemPrompt appends text to the base system prompt.
func AppendSystemPrompt(text string) {
	_sdkCall("append_system_prompt", map[string]string{"text": text})
}

// ShowPicker opens an interactive TUI list picker.
// When the user selects an item, the host fires EventOnCommand{name: callback, args: [item.ID]}.
// Register a handler with OnCommand(callback, fn) to receive the selection.
func ShowPicker(title string, items []PickerItem, callback string) {
	_sdkCall("show_picker", map[string]any{
		"title":    title,
		"items":    items,
		"callback": callback,
	})
}

// AgentResetHistory replaces the main agent's conversation history and
// rebuilds the chat view from the supplied messages.
func AgentResetHistory(messages []Message) {
	_sdkCall("agent_reset_history", map[string]any{"messages": messages})
}

// Exec runs a shell command and returns its combined output.
// Requires the exec permission in the extension manifest.
func Exec(command, dir string) (output string, err error) {
	type result struct {
		Output string `json:"output"`
		Error  string `json:"error,omitempty"`
	}
	raw := _sdkCallResult("exec", map[string]string{"command": command, "dir": dir})
	if raw == nil {
		return "", fmt.Errorf("exec: no response")
	}
	var r result
	if e := json.Unmarshal(raw, &r); e != nil {
		return "", e
	}
	if r.Error != "" {
		return r.Output, fmt.Errorf("%s", r.Error)
	}
	return r.Output, nil
}

// GetEnv returns the value of an environment variable.
// Pass an empty name to get all variables as a JSON array.
func GetEnv(name string) (string, error) {
	raw := _sdkCallResult("get_env", map[string]string{"name": name})
	if raw == nil {
		return "", fmt.Errorf("get_env: no response")
	}
	var r struct {
		Value string `json:"value"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return "", err
	}
	return r.Value, nil
}

// FormatMarkdown renders markdown text to ANSI-styled terminal text via the
// host's native renderer. Returns the raw text unchanged if the host rejects
// the call (e.g. permission denied) or returns an empty response.
func FormatMarkdown(text string) string {
	raw := _sdkCallResult("format_markdown", map[string]string{"text": text})
	if raw == nil {
		return text
	}
	var r struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return text
	}
	if r.Text == "" && text != "" {
		return text
	}
	return r.Text
}

// StoreSet stores a key-value pair in the extension's private store.
func StoreSet(key, value string) {
	_sdkCall("store_set", map[string]string{"key": key, "value": value})
}

// StoreGet retrieves a value from the extension's private store.
// Returns ("", false) if the key does not exist.
func StoreGet(key string) (string, bool) {
	raw := _sdkCallResult("store_get", map[string]string{"key": key})
	if raw == nil {
		return "", false
	}
	var r struct {
		Value string `json:"value"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return "", false
	}
	return r.Value, true
}

// Log emits a log line at the given level (0=debug 1=info 2=warn 3=error).
func Log(level int, msg string) {
	b := []byte(msg)
	if len(b) == 0 {
		return
	}
	_sdkHostLog(uint32(level), uint32(uintptr(unsafe.Pointer(&b[0]))), uint32(len(b)))
}

// Logf emits a formatted log line.
func Logf(level int, format string, args ...any) {
	Log(level, fmt.Sprintf(format, args...))
}

// ─── Types ────────────────────────────────────────────────────────────────────

// PickerItem is one entry in a ShowPicker call.

// Message is a chat message for AgentResetHistory.

// "user" or "assistant"

// ─── Internal host_call helpers ───────────────────────────────────────────────

// _sdkCall fires a host_call and discards the response.
func _sdkCall(method string, params any) {
	_sdkCallResult(method, params)
}

// _sdkCallResult fires a host_call and returns the raw response Result field,
// or nil on error or empty response.
func _sdkCallResult(method string, params any) json.RawMessage {
	type request struct {
		Method string `json:"method"`
		Params any    `json:"params,omitempty"`
	}
	reqBytes, err := json.Marshal(request{Method: method, Params: params})
	if err != nil {
		return nil
	}
	buf := make([]byte, len(reqBytes))
	copy(buf, reqBytes)
	ptr := uintptr(unsafe.Pointer(&buf[0]))

	var respPtr, respLen uint32
	_sdkHostCall(
		uint32(ptr), uint32(len(buf)),
		uint32(uintptr(unsafe.Pointer(&respPtr))),
		uint32(uintptr(unsafe.Pointer(&respLen))),
	)

	if respPtr == 0 || respLen == 0 {
		return nil
	}
	respBytes := make([]byte, respLen)
	copy(respBytes, unsafe.Slice((*byte)(unsafe.Pointer(uintptr(respPtr))), respLen))
	delete(_sdkPinned, uintptr(respPtr))

	var resp struct {
		Error  string          `json:"error,omitempty"`
		Result json.RawMessage `json:"result,omitempty"`
	}
	if err := json.Unmarshal(respBytes, &resp); err != nil || resp.Error != "" {
		return nil
	}
	return resp.Result
}

// ─── UI scene graph ───────────────────────────────────────────────────────────
// Declarative, node-based TUI. An extension owns a named "area" and drives it
// with scene-graph patches. Requires the "ui" permission in the manifest.

// UIProps carries optional style/layout for a UINode. Colour fields name theme
// tokens (e.g. "accent", "muted", "error"), never raw colours.
type UIProps struct {
	Width     string `json:"width,omitempty"`
	Height    string `json:"height,omitempty"`
	Border    string `json:"border,omitempty"`
	Padding   []int  `json:"padding,omitempty"`
	Margin    []int  `json:"margin,omitempty"`
	Align     string `json:"align,omitempty"`
	Fg        string `json:"fg,omitempty"`
	Bg        string `json:"bg,omitempty"`
	Bold      bool   `json:"bold,omitempty"`
	Italic    bool   `json:"italic,omitempty"`
	Underline bool   `json:"underline,omitempty"`
	Faint     bool   `json:"faint,omitempty"`
	Wrap      bool   `json:"wrap,omitempty"`
}

// UINode is one node in a UI scene graph. Type is one of: text, vstack, hstack,
// viewport, spinner, divider.
type UINode struct {
	ID       string   `json:"id"`
	Type     string   `json:"type"`
	Text     string   `json:"text,omitempty"`
	Props    *UIProps `json:"props,omitempty"`
	Children []UINode `json:"children,omitempty"`
}

// UIPatchOp is a single scene-graph mutation. Op is one of: set_root, insert,
// update, remove, append_text.
type UIPatchOp struct {
	Op     string   `json:"op"`
	Parent string   `json:"parent,omitempty"`
	Index  *int     `json:"index,omitempty"`
	ID     string   `json:"id,omitempty"`
	Node   *UINode  `json:"node,omitempty"`
	Props  *UIProps `json:"props,omitempty"`
	Text   string   `json:"text,omitempty"`
}

// UICreateArea registers a UI area owned by this extension.
// placement is one of: "main", "sidebar", "status", "overlay".
// weight is a relative size hint (0 = default).
// minHeight/maxHeight/minWidth/maxWidth are optional sizing constraints;
// each accepts "" (unconstrained), "N" (absolute cells/lines), or "N%"
// (percentage of terminal dimension).
func UICreateArea(id, placement string, weight int, minHeight, maxHeight, minWidth, maxWidth string) {
	area := map[string]any{"id": id, "placement": placement, "weight": weight}
	if minHeight != "" {
		area["min_height"] = minHeight
	}
	if maxHeight != "" {
		area["max_height"] = maxHeight
	}
	if minWidth != "" {
		area["min_width"] = minWidth
	}
	if maxWidth != "" {
		area["max_width"] = maxWidth
	}
	_sdkCall("ui_create_area", map[string]any{"area": area})
}

// UIPatch applies a batch of scene-graph ops to an area, in order, atomically.
func UIPatch(area string, ops ...UIPatchOp) {
	_sdkCall("ui_patch", map[string]any{"area": area, "ops": ops})
}

// UIRemoveArea removes a UI area and its scene graph.
func UIRemoveArea(area string) {
	_sdkCall("ui_remove_area", map[string]string{"area": area})
}

// Node builders.
func UIText(id, text string) UINode { return UINode{ID: id, Type: "text", Text: text} }

func UIVStack(id string, kids ...UINode) UINode {
	return UINode{ID: id, Type: "vstack", Children: kids}
}

func UIHStack(id string, kids ...UINode) UINode {
	return UINode{ID: id, Type: "hstack", Children: kids}
}
func UIDivider(id string) UINode { return UINode{ID: id, Type: "divider"} }

// Op builders.
func OpSetRoot(node UINode) UIPatchOp { return UIPatchOp{Op: "set_root", Node: &node} }

func OpInsert(parent string, node UINode) UIPatchOp {
	return UIPatchOp{Op: "insert", Parent: parent, Node: &node}
}

func OpUpdate(id string, props UIProps) UIPatchOp {
	return UIPatchOp{Op: "update", ID: id, Props: &props}
}
func OpRemove(id string) UIPatchOp           { return UIPatchOp{Op: "remove", ID: id} }
func OpAppendText(id, text string) UIPatchOp { return UIPatchOp{Op: "append_text", ID: id, Text: text} }

func OpReplaceText(id, text string) UIPatchOp {
	return UIPatchOp{Op: "replace_text", ID: id, Text: text}
}

// OnToken registers a handler called with batches of streamed assistant text as
// the agent produces it. agentID identifies the producing agent.
func OnToken(fn func(agentID, text string)) {
	_sdkOn("token", func(payload json.RawMessage) {
		var p struct {
			AgentID string `json:"agent_id"`
			Text    string `json:"text"`
		}
		if err := json.Unmarshal(payload, &p); err == nil {
			fn(p.AgentID, p.Text)
		}
	})
}

// OnNotify registers a handler called when a system notification line is shown
// in the chat. text may begin with "⚠" for warnings/errors.
func OnNotify(fn func(text string)) {
	_sdkOn("notify", func(payload json.RawMessage) {
		var p struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal(payload, &p); err == nil {
			fn(p.Text)
		}
	})
}
