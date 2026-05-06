//go:build wasip1

// Package main is the env built-in extension for the bob coding harness.
// It registers a "get_env" tool that returns environment variables via the host.
package main

import (
	"encoding/json"
	"unsafe"
)

//go:wasmimport env host_log
func hostLog(level, ptr, length uint32)

//go:wasmimport env host_call
func hostCall(reqPtr, reqLen, respPtrPtr, respLenPtr uint32) uint32

var pinned = map[uintptr][]byte{}

//go:wasmexport _alloc
func extensionAlloc(size int32) int32 {
	if size <= 0 {
		return 0
	}
	buf := make([]byte, size)
	ptr := uintptr(unsafe.Pointer(&buf[0]))
	pinned[ptr] = buf
	return int32(ptr)
}

//go:wasmexport _free
func extensionFree(ptr int32) {
	delete(pinned, uintptr(ptr))
}

//go:wasmexport _init
func extensionInit() int32 {
	schema := `{"type":"object","properties":{"name":{"type":"string","description":"Specific env var name to look up (optional — omit to get all)"}}}`
	return registerTool("get_env", "Read environment variables from the host system", schema)
}

//go:wasmexport _on_event
func extensionOnEvent(ptr, length int32) int32 {
	data := unsafe.Slice((*byte)(unsafe.Pointer(uintptr(ptr))), length)

	var evt struct {
		Type    string          `json:"type"`
		Payload json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(data, &evt); err != nil {
		return 0
	}
	if evt.Type == "before_tool_call" {
		onBeforeToolCall(evt.Payload)
	}
	return 0
}

type beforeToolCallPayload struct {
	ToolCallID string          `json:"tool_call_id"`
	ToolName   string          `json:"tool_name"`
	Input      json.RawMessage `json:"input"`
}

func onBeforeToolCall(raw json.RawMessage) {
	var p beforeToolCallPayload
	if err := json.Unmarshal(raw, &p); err != nil || p.ToolName != "get_env" {
		return
	}

	var input struct {
		Name string `json:"name"`
	}
	json.Unmarshal(p.Input, &input)

	result := hostCallWithResponse("get_env", map[string]string{"name": input.Name})
	if result == "" {
		sendToolResult(p.ToolCallID, "get_env: no response from host", true)
		return
	}

	// Unwrap HostCallResponse envelope: {"result":{"value":"..."},"error":"..."}
	var envelope struct {
		Result json.RawMessage `json:"result"`
		Error  string          `json:"error"`
	}
	if err := json.Unmarshal([]byte(result), &envelope); err != nil {
		sendToolResult(p.ToolCallID, result, false)
		return
	}
	if envelope.Error != "" {
		sendToolResult(p.ToolCallID, envelope.Error, true)
		return
	}
	var resp struct {
		Value string `json:"value"`
	}
	if err := json.Unmarshal(envelope.Result, &resp); err != nil {
		sendToolResult(p.ToolCallID, string(envelope.Result), false)
		return
	}
	sendToolResult(p.ToolCallID, resp.Value, false)
}

func registerTool(name, desc, inputSchema string) int32 {
	type toolParams struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		InputSchema json.RawMessage `json:"input_schema"`
	}
	rc := hostCallJSON("register_tool", toolParams{Name: name, Description: desc, InputSchema: json.RawMessage(inputSchema)})
	if rc != 0 {
		return rc
	}
	return hostCallJSON("subscribe", map[string]string{"event": "before_tool_call"})
}

func sendToolResult(toolCallID, result string, isError bool) {
	hostCallJSON("tool_result", map[string]any{"tool_call_id": toolCallID, "result": result, "is_error": isError})
}

func hostCallWithResponse(method string, params any) string {
	type request struct {
		Method string `json:"method"`
		Params any    `json:"params,omitempty"`
	}
	reqBytes, err := json.Marshal(request{Method: method, Params: params})
	if err != nil {
		return ""
	}
	reqBuf := make([]byte, len(reqBytes))
	copy(reqBuf, reqBytes)
	reqPtr := uintptr(unsafe.Pointer(&reqBuf[0]))

	var respPtr, respLen uint32
	hostCall(
		uint32(reqPtr), uint32(len(reqBuf)),
		uint32(uintptr(unsafe.Pointer(&respPtr))),
		uint32(uintptr(unsafe.Pointer(&respLen))),
	)
	if respPtr == 0 || respLen == 0 {
		return ""
	}
	resp := make([]byte, respLen)
	mem := (*[1 << 28]byte)(unsafe.Pointer(uintptr(respPtr)))
	copy(resp, mem[:respLen])
	delete(pinned, uintptr(respPtr))
	return string(resp)
}

func hostCallJSON(method string, params any) int32 {
	type request struct {
		Method string `json:"method"`
		Params any    `json:"params,omitempty"`
	}
	reqBytes, err := json.Marshal(request{Method: method, Params: params})
	if err != nil {
		return 1
	}
	reqBuf := make([]byte, len(reqBytes))
	copy(reqBuf, reqBytes)
	reqPtr := uintptr(unsafe.Pointer(&reqBuf[0]))
	var respPtr, respLen uint32
	rc := hostCall(
		uint32(reqPtr), uint32(len(reqBuf)),
		uint32(uintptr(unsafe.Pointer(&respPtr))),
		uint32(uintptr(unsafe.Pointer(&respLen))),
	)
	if respPtr != 0 {
		delete(pinned, uintptr(respPtr))
	}
	return int32(rc)
}


func main() {}
