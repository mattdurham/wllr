// wllrsdk.go — wllr extension SDK for Go/WASM
// Copy this file into your extension directory. It provides all host bindings
// and lifecycle scaffolding. Your extension only needs main.go + this file.
//
//go:build wasip1

package main

import (
	"encoding/json"
	"fmt"
	"unsafe"
)

// ═══════════════════════════════════════════════════════════════════════════
//   WASM ↔ Host memory bridge
// ═══════════════════════════════════════════════════════════════════════════

//export _alloc
func _alloc(size uint32) uint32 {
	buf := make([]byte, size)
	return uint32(uintptr(unsafe.Pointer(&buf[0])))
}

//export _free
func _free(ptr uint32) {
	// Go's GC handles this; no-op
}

// hostReadString reads a string from the given ptr/len in WASM linear memory
func hostReadString(ptr, length uint32) string {
	if length == 0 {
		return ""
	}
	buf := unsafe.Slice((*byte)(unsafe.Pointer(uintptr(ptr))), length)
	return string(buf)
}

// hostWriteString writes a Go string into WASM linear memory and returns (ptr, len)
func hostWriteString(s string) (uint32, uint32) {
	if s == "" {
		return 0, 0
	}
	buf := []byte(s)
	ptr := uint32(uintptr(unsafe.Pointer(&buf[0])))
	return ptr, uint32(len(buf))
}

// ═══════════════════════════════════════════════════════════════════════════
//   Host call imports
// ═══════════════════════════════════════════════════════════════════════════

//go:wasmimport wllr host_call
func hostCall(methodPtr, methodLen, payloadPtr, payloadLen, outPtr, outLen uint32) uint32

func callHost(method string, payload any) (json.RawMessage, error) {
	var payloadBytes []byte
	if payload != nil {
		var err error
		payloadBytes, err = json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("marshal payload: %w", err)
		}
	}

	methodP, methodL := hostWriteString(method)
	payloadP, payloadL := hostWriteString(string(payloadBytes))

	var outPtr, outLen uint32
	code := hostCall(methodP, methodL, payloadP, payloadL,
		uint32(uintptr(unsafe.Pointer(&outPtr))),
		uint32(uintptr(unsafe.Pointer(&outLen))))

	result := hostReadString(outPtr, outLen)

	if code != 0 {
		return nil, fmt.Errorf("host_call error (code %d): %s", code, result)
	}

	return json.RawMessage(result), nil
}

// ═══════════════════════════════════════════════════════════════════════════
//   Lifecycle + event dispatch
// ═══════════════════════════════════════════════════════════════════════════

type (
	ToolCallHandler       func(callID, name string, input json.RawMessage) (result string, isError bool)
	CommandHandler        func(args string)
	SessionStartHandler   func(sessionID string)
	BeforeAgentHandler    func(prompt string) string
	MessageEndHandler     func(role, content string)
	PickerHandler         func(selected string)
)

var (
	toolHandlers          []ToolCallHandler
	commandHandlers       = make(map[string]CommandHandler)
	sessionStartHandlers  []SessionStartHandler
	beforeAgentHandlers   []BeforeAgentHandler
	messageEndHandlers    []MessageEndHandler
	pickerHandlers        = make(map[string]PickerHandler) // keyed by pickerID
)

//export _init
func _init() uint32 {
	// Go init() functions have already run
	return 0
}

//export _on_event
func _on_event(typePtr, typeLen, payloadPtr, payloadLen uint32) uint32 {
	eventType := hostReadString(typePtr, typeLen)
	payloadStr := hostReadString(payloadPtr, payloadLen)

	switch eventType {
	case "tool_call":
		var ev struct {
			CallID string          `json:"call_id"`
			Name   string          `json:"name"`
			Input  json.RawMessage `json:"input"`
		}
		if err := json.Unmarshal([]byte(payloadStr), &ev); err != nil {
			Log("error", fmt.Sprintf("tool_call unmarshal: %v", err))
			return 1
		}
		for _, h := range toolHandlers {
			if result, isErr := h(ev.CallID, ev.Name, ev.Input); result != "" {
				ToolResult(ev.CallID, result, isErr)
				return 0
			}
		}

	case "command":
		var ev struct {
			Name string `json:"name"`
			Args string `json:"args"`
		}
		if err := json.Unmarshal([]byte(payloadStr), &ev); err != nil {
			Log("error", fmt.Sprintf("command unmarshal: %v", err))
			return 1
		}
		if h, ok := commandHandlers[ev.Name]; ok {
			h(ev.Args)
		}

	case "session_start":
		var ev struct {
			SessionID string `json:"session_id"`
		}
		if err := json.Unmarshal([]byte(payloadStr), &ev); err != nil {
			Log("error", fmt.Sprintf("session_start unmarshal: %v", err))
			return 1
		}
		for _, h := range sessionStartHandlers {
			h(ev.SessionID)
		}

	case "before_agent_start":
		var ev struct {
			Prompt string `json:"prompt"`
		}
		if err := json.Unmarshal([]byte(payloadStr), &ev); err != nil {
			Log("error", fmt.Sprintf("before_agent_start unmarshal: %v", err))
			return 1
		}
		for _, h := range beforeAgentHandlers {
			newPrompt := h(ev.Prompt)
			if newPrompt != ev.Prompt {
				ev.Prompt = newPrompt
			}
		}

	case "message_end":
		var ev struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}
		if err := json.Unmarshal([]byte(payloadStr), &ev); err != nil {
			Log("error", fmt.Sprintf("message_end unmarshal: %v", err))
			return 1
		}
		for _, h := range messageEndHandlers {
			h(ev.Role, ev.Content)
		}

	case "picker_result":
		var ev struct {
			PickerID string `json:"picker_id"`
			Selected string `json:"selected"`
		}
		if err := json.Unmarshal([]byte(payloadStr), &ev); err != nil {
			Log("error", fmt.Sprintf("picker_result unmarshal: %v", err))
			return 1
		}
		if h, ok := pickerHandlers[ev.PickerID]; ok {
			h(ev.Selected)
			delete(pickerHandlers, ev.PickerID)
		}
	}

	return 0
}

// ═══════════════════════════════════════════════════════════════════════════
//   Public SDK
// ═══════════════════════════════════════════════════════════════════════════

func RegisterTool(name, description string, schema json.RawMessage) {
	callHost("register_tool", map[string]any{
		"name":        name,
		"description": description,
		"schema":      schema,
	})
}

func RegisterCommand(name, description string) {
	callHost("register_command", map[string]any{
		"name":        name,
		"description": description,
	})
}

func OnToolCall(h ToolCallHandler) {
	toolHandlers = append(toolHandlers, h)
}

func OnCommand(name string, h CommandHandler) {
	commandHandlers[name] = h
}

func OnSessionStart(h SessionStartHandler) {
	sessionStartHandlers = append(sessionStartHandlers, h)
}

func OnBeforeAgentStart(h BeforeAgentHandler) {
	beforeAgentHandlers = append(beforeAgentHandlers, h)
}

func OnMessageEnd(h MessageEndHandler) {
	messageEndHandlers = append(messageEndHandlers, h)
}

func ToolResult(callID, result string, isError bool) {
	callHost("tool_result", map[string]any{
		"call_id":  callID,
		"result":   result,
		"is_error": isError,
	})
}

func Modal(text string) {
	callHost("modal", map[string]any{"text": text})
}

func Notify(text string) {
	callHost("notify", map[string]any{"text": text})
}

func SetStatus(key, value string) {
	callHost("set_status", map[string]any{"key": key, "value": value})
}

func SetSystemPrompt(prompt string) {
	callHost("set_system_prompt", map[string]any{"prompt": prompt})
}

func AppendSystemPrompt(text string) {
	callHost("append_system_prompt", map[string]any{"text": text})
}

func ShowPicker(title string, items []string, callback PickerHandler) {
	pickerID := fmt.Sprintf("picker_%d", len(pickerHandlers))
	pickerHandlers[pickerID] = callback
	callHost("show_picker", map[string]any{
		"picker_id": pickerID,
		"title":     title,
		"items":     items,
	})
}

func AgentResetHistory(messages []map[string]string) {
	callHost("agent_reset_history", map[string]any{"messages": messages})
}

func Exec(command, dir string) (string, error) {
	res, err := callHost("exec", map[string]any{"command": command, "dir": dir})
	if err != nil {
		return "", err
	}
	var out struct {
		Output string `json:"output"`
	}
	if err := json.Unmarshal(res, &out); err != nil {
		return "", err
	}
	return out.Output, nil
}

func GetEnv(name string) string {
	res, err := callHost("get_env", map[string]any{"name": name})
	if err != nil {
		return ""
	}
	var out struct {
		Value string `json:"value"`
	}
	json.Unmarshal(res, &out)
	return out.Value
}

func StoreSet(key, value string) {
	callHost("store_set", map[string]any{"key": key, "value": value})
}

func StoreGet(key string) string {
	res, err := callHost("store_get", map[string]any{"key": key})
	if err != nil {
		return ""
	}
	var out struct {
		Value string `json:"value"`
	}
	json.Unmarshal(res, &out)
	return out.Value
}

func Log(level, msg string) {
	callHost("log", map[string]any{"level": level, "message": msg})
}

func Logf(level, format string, args ...any) {
	Log(level, fmt.Sprintf(format, args...))
}
