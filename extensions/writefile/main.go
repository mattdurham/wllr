//go:build wasip1

// Package main is the writefile built-in extension for the bob coding harness.
//
// Build:
//
//	GOOS=wasip1 GOARCH=wasm go build -o writefile.wasm .
//
// What it does:
//   - Registers a "write_file" tool that writes content to a file.
//   - Trusted (built-in): all permissions are pre-granted; no manifest needed.
package main

import (
	"encoding/json"
	"os"
	"unsafe"
)

// ---- Host imports -----------------------------------------------------------

//go:wasmimport env host_log
func hostLog(level, ptr, length uint32)

//go:wasmimport env host_call
func hostCall(reqPtr, reqLen, respPtrPtr, respLenPtr uint32) uint32

// ---- Memory management ------------------------------------------------------

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

// ---- Required exports -------------------------------------------------------

//go:wasmexport _init
func extensionInit() int32 {
	schema := `{"type":"object","properties":{"path":{"type":"string","description":"Path of the file to write"},"content":{"type":"string","description":"Content to write to the file"}},"required":["path","content"]}`
	return registerTool("write_file", "Write content to a file on the filesystem", schema)
}

//go:wasmexport _on_event
func extensionOnEvent(ptr, length int32) int32 {
	data := unsafe.Slice((*byte)(unsafe.Pointer(uintptr(ptr))), length)

	var evt struct {
		Type    string          `json:"type"`
		Payload json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(data, &evt); err != nil {
		logMsg(2, "writefile: unmarshal event: "+err.Error())
		return 0
	}

	switch evt.Type {
	case "before_tool_call":
		onBeforeToolCall(evt.Payload)
	}
	return 0
}

// ---- Event handlers ---------------------------------------------------------

type beforeToolCallPayload struct {
	ToolCallID string          `json:"tool_call_id"`
	ToolName   string          `json:"tool_name"`
	Input      json.RawMessage `json:"input"`
}

func onBeforeToolCall(raw json.RawMessage) {
	var p beforeToolCallPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		logMsg(2, "writefile: unmarshal before_tool_call payload: "+err.Error())
		return
	}
	if p.ToolName != "write_file" {
		return
	}

	var input struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(p.Input, &input); err != nil {
		sendToolResult(p.ToolCallID, "invalid input: "+err.Error(), true)
		return
	}
	if input.Path == "" {
		sendToolResult(p.ToolCallID, "path is required", true)
		return
	}

	if err := os.WriteFile(input.Path, []byte(input.Content), 0o644); err != nil {
		sendToolResult(p.ToolCallID, "write_file error: "+err.Error(), true)
		return
	}
	sendToolResult(p.ToolCallID, "written "+input.Path, false)
}

// ---- Host call helpers ------------------------------------------------------

func registerTool(name, desc, inputSchema string) int32 {
	type toolParams struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		InputSchema json.RawMessage `json:"input_schema"`
	}
	params := toolParams{
		Name:        name,
		Description: desc,
		InputSchema: json.RawMessage(inputSchema),
	}
	rc := hostCallJSON("register_tool", params)
	if rc != 0 {
		logMsg(2, "writefile: register_tool failed")
	}
	// Subscribe to before_tool_call events.
	if rc2 := subscribe("before_tool_call"); rc2 != 0 {
		logMsg(2, "writefile: subscribe before_tool_call failed")
	}
	return rc
}

func sendToolResult(toolCallID, result string, isError bool) {
	type params struct {
		ToolCallID string `json:"tool_call_id"`
		Result     string `json:"result"`
		IsError    bool   `json:"is_error"`
	}
	hostCallJSON("tool_result", params{ToolCallID: toolCallID, Result: result, IsError: isError})
}

func subscribe(eventType string) int32 {
	type params struct {
		Event string `json:"event"`
	}
	return hostCallJSON("subscribe", params{Event: eventType})
}

func logMsg(level int, msg string) {
	b := []byte(msg)
	if len(b) == 0 {
		return
	}
	hostLog(uint32(level), uint32(uintptr(unsafe.Pointer(&b[0]))), uint32(len(b)))
}

func hostCallJSON(method string, params any) int32 {
	type request struct {
		Method string `json:"method"`
		Params any    `json:"params,omitempty"`
	}
	reqBytes, err := json.Marshal(request{Method: method, Params: params})
	if err != nil {
		logMsg(3, "writefile: marshal host_call request: "+err.Error())
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
