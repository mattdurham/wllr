//go:build wasip1

// Package main is the lsp-bridge extension for wllr.
// It provides native process spawning and stdio handling for LSP servers.
package main

import (
	"encoding/json"
	"strings"
	"sync"
	"unsafe"
)

//go:wasmimport env host_log
func hostLog(level, ptr, length uint32)

//go:wasmimport env host_call
func hostCall(reqPtr, reqLen, respPtrPtr, respLenPtr uint32) uint32

var pinned = map[uintptr][]byte{}

type LSPServerState struct {
	Name         string   `json:"name"`
	Command      string   `json:"command"`
	Args         []string `json:"args"`
	PID          int32    `json:"pid"`
	StdinPID     int32    `json:"-"`
	StdoutPID    int32    `json:"-"`
	RequestID    int      `json:"request_id"`
	Initialized  bool     `json:"initialized"`
	WorkspaceURI string   `json:"workspace_uri,omitempty"`
	mu           sync.Mutex
}

type ServerState struct {
	servers map[string]*LSPServerState
	mu      sync.RWMutex
}

var state = &ServerState{servers: make(map[string]*LSPServerState)}

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
	logMsg(1, "lsp-bridge: initialized")
	return 0
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

	switch evt.Type {
	case "before_tool_call":
		handleBeforeToolCall(data)
	case "session_start":
		logMsg(1, "lsp-bridge: session started")
	case "shutdown":
		logMsg(1, "lsp-bridge: shutdown")
		state.mu.Lock()
		for _, srv := range state.servers {
			if srv.PID != 0 {
				spawnClose(srv.PID)
			}
		}
		state.servers = make(map[string]*LSPServerState)
		logMsg(1, "lsp-bridge: shutdown complete")
	}
	return 0
}

func handleBeforeToolCall(data []byte) {
	var evt struct {
		Type    string          `json:"type"`
		Payload json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(data, &evt); err != nil {
		return
	}

	var payload struct {
		ToolCallID string          `json:"tool_call_id"`
		ToolName   string          `json:"tool_name"`
		Input      json.RawMessage `json:"input"`
	}
	if err := json.Unmarshal(evt.Payload, &payload); err != nil {
		return
	}

	// Check if this is an LSP tool
	if !strings.HasPrefix(payload.ToolName, "lsp_") {
		return // Not an LSP tool
	}

	// Route to appropriate handler
	switch payload.ToolName {
	case "lsp_server_start":
		handleLSPServerStart(payload.Input)
	case "lsp_server_stop":
		handleLSPServerStop(payload.Input)
	case "lsp_server_list":
		handleLSPServerList()
	default:
		logMsg(2, "lsp-bridge: unhandled tool "+payload.ToolName)
	}
}

func handleLSPServerStart(input json.RawMessage) {
	var args map[string]any
	if err := json.Unmarshal(input, &args); err != nil {
		logMsg(3, "lsp-bridge: invalid input for lsp_server_start")
		return
	}

	name := stringVal(args, "name")
	command := stringVal(args, "command")

	if name == "" || command == "" {
		logMsg(3, "lsp-bridge: missing name or command")
		return
	}

	var argsList []string
	if rawArgs, ok := args["args"]; ok {
		if rawSlice, ok := rawArgs.([]any); ok {
			for _, arg := range rawSlice {
				if s, ok := arg.(string); ok {
					argsList = append(argsList, s)
				}
			}
		}
	}

	pid, err := spawnProcess(command, argsList)
	if err != nil {
		logMsg(3, "lsp-bridge: failed to spawn "+name+": "+err.Error())
		return
	}

	logMsg(1, "lsp-bridge: spawned "+name+" with PID "+fmtInt32(pid))

	state.mu.Lock()
	state.servers[name] = &LSPServerState{
		Name:      name,
		Command:   command,
		Args:      argsList,
		PID:       pid,
		RequestID: 0,
	}
	state.mu.Unlock()

	type result struct {
		Name    string `json:"name"`
		Command string `json:"command"`
		Status  string `json:"status"`
	}
	resp := result{
		Name:    name,
		Command: command,
		Status:  "started",
	}

	jsonResp, _ := json.Marshal(resp)
	hostCallJSON("tool_result", map[string]any{
		"tool_call_id": stringVal(args, "tool_call_id"),
		"result":       string(jsonResp),
		"is_error":     false,
	})
}

func handleLSPServerStop(input json.RawMessage) {
	var args map[string]any
	if err := json.Unmarshal(input, &args); err != nil {
		logMsg(3, "lsp-bridge: invalid input for lsp_server_stop")
		return
	}

	name := stringVal(args, "name")
	if name == "" {
		logMsg(3, "lsp-bridge: missing name for stop")
		return
	}

	state.mu.Lock()
	srv, exists := state.servers[name]
	state.mu.Unlock()

	if !exists {
		logMsg(3, "lsp-bridge: server "+name+" not found")
		return
	}

	if srv.PID != 0 {
		spawnClose(srv.PID)
		logMsg(1, "lsp-bridge: stopped "+name)
	}

	state.mu.Lock()
	delete(state.servers, name)
	state.mu.Unlock()

	type result struct {
		Name   string `json:"name"`
		Status string `json:"status"`
	}
	resp := result{
		Name:   name,
		Status: "stopped",
	}

	jsonResp, _ := json.Marshal(resp)
	hostCallJSON("tool_result", map[string]any{
		"tool_call_id": stringVal(args, "tool_call_id"),
		"result":       string(jsonResp),
		"is_error":     false,
	})
}

func handleLSPServerList() {
	state.mu.RLock()
	servers := make([]map[string]any, 0, len(state.servers))
	for name, srv := range state.servers {
		srv.mu.Lock()
		servers = append(servers, map[string]any{
			"name":        name,
			"command":     srv.Command,
			"initialized": srv.Initialized,
		})
		srv.mu.Unlock()
	}
	state.mu.RUnlock()

	jsonServers, _ := json.Marshal(servers)
	hostCallJSON("tool_result", map[string]any{
		"tool_call_id": "server_list_" + fmtInt(timeNow()),
		"result":       string(jsonServers),
		"is_error":     false,
	})
}

// Bridge APIs (to be implemented with actual process spawning)

func spawnProcess(command string, args []string) (int32, error) {
	// TODO: Implement actual process spawning
	logMsg(1, "lsp-bridge: spawnProcess stub for "+command)
	return 0, nil
}

func spawnRead(pid int32, bufPtr *byte, len int32) (int32, error) {
	// TODO: Implement read from process
	return 0, nil
}

func spawnWrite(pid int32, bufPtr *byte, len int32) (int32, error) {
	// TODO: Implement write to process
	return 0, nil
}

func spawnClose(pid int32) error {
	// TODO: Implement process cleanup
	logMsg(1, "lsp-bridge: spawnClose stub")
	return nil
}

func stringVal(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return v
}

func hostCallJSON(method string, params any) {
	type request struct {
		Method string `json:"method"`
		Params any    `json:"params,omitempty"`
	}
	reqBytes, err := json.Marshal(request{Method: method, Params: params})
	if err != nil {
		return
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
	if respPtr != 0 {
		delete(pinned, uintptr(respPtr))
	}
}

func logMsg(level int, msg string) {
	b := []byte(msg + "\x00")
	if len(b) == 0 {
		return
	}
	hostLog(uint32(level), uint32(uintptr(unsafe.Pointer(&b[0]))), uint32(len(b)))
}

func fmtInt(n int) string {
	if n == 0 {
		return "0"
	}
	buf := make([]byte, 0, 10)
	for n > 0 {
		buf = append([]byte{byte('0' + n%10)}, buf...)
		n /= 10
	}
	return string(buf)
}

func fmtInt32(n int32) string {
	if n == 0 {
		return "0"
	}
	buf := make([]byte, 0, 10)
	for n > 0 {
		buf = append([]byte{byte('0' + int(n%10))}, buf...)
		n /= 10
	}
	return string(buf)
}

func timeNow() int {
	return 0 // stub
}

func main() {}
