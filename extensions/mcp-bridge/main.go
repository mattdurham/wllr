//go:build wasip1

// Package main is the mcp-bridge extension for the wllr coding harness.
// It loads MCP server configs, spawns MCP servers via stdio, discovers their
// tools, registers them with wllr, and proxies tool calls to the appropriate server.
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

// mcpServerConfig defines a single MCP server in the extension config.

// mcpConfig is the top-level config structure.

// mcpTool represents a tool discovered from an MCP server.

// mcpServerState tracks a running MCP server process.

// process ID returned by host

var (
	config  mcpConfig
	servers = make(map[string]*mcpServerState)
)

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
	// Load config from config_read host call
	if err := loadConfig(); err != nil {
		logMsg(3, "mcp-bridge: failed to load config: "+err.Error())
		return 1
	}

	// Subscribe to session_start to spawn servers
	if rc := hostCallJSON("subscribe", map[string]string{"event": "session_start"}); rc != 0 {
		return rc
	}

	// Subscribe to shutdown to clean up servers
	if rc := hostCallJSON("subscribe", map[string]string{"event": "shutdown"}); rc != 0 {
		return rc
	}

	// Subscribe to before_tool_call to intercept MCP tool calls
	if rc := hostCallJSON("subscribe", map[string]string{"event": "before_tool_call"}); rc != 0 {
		return rc
	}

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
	case "session_start":
		onSessionStart()
	case "shutdown":
		onShutdown()
	case "before_tool_call":
		onBeforeToolCall(evt.Payload)
	}
	return 0
}

func loadConfig() error {
	result := hostCallWithResponse("config_read", nil)
	if result == "" {
		// No config - that's ok, just no MCP servers
		logMsg(1, "mcp-bridge: no config found, no MCP servers will be loaded")
		return nil
	}

	var envelope struct {
		Result json.RawMessage `json:"result"`
		Error  string          `json:"error"`
	}
	if err := json.Unmarshal([]byte(result), &envelope); err != nil {
		return err
	}
	if envelope.Error != "" {
		return &jsonError{envelope.Error}
	}

	if err := json.Unmarshal(envelope.Result, &config); err != nil {
		return err
	}

	logMsg(1, "mcp-bridge: loaded config with "+itoa(len(config.Servers))+" servers")
	return nil
}

func onSessionStart() {
	if len(config.Servers) == 0 {
		return
	}

	logMsg(1, "mcp-bridge: spawning "+itoa(len(config.Servers))+" MCP servers")

	// Spawn each MCP server
	for name, cfg := range config.Servers {
		if err := spawnServer(name, cfg); err != nil {
			logMsg(3, "mcp-bridge: failed to spawn "+name+": "+err.Error())
			continue
		}
		logMsg(1, "mcp-bridge: spawned "+name)
	}

	// Discover tools from all servers
	for name, state := range servers {
		if err := discoverTools(state); err != nil {
			logMsg(3, "mcp-bridge: failed to discover tools from "+name+": "+err.Error())
			continue
		}
		logMsg(1, "mcp-bridge: discovered "+itoa(len(state.Tools))+" tools from "+name)

		// Register each tool
		for _, tool := range state.Tools {
			if err := registerTool(name, tool); err != nil {
				logMsg(2, "mcp-bridge: failed to register tool "+tool.Name+" from "+name+": "+err.Error())
			}
		}
	}
}

func onShutdown() {
	logMsg(1, "mcp-bridge: shutting down MCP servers")
	for name, state := range servers {
		if err := shutdownServer(state); err != nil {
			logMsg(2, "mcp-bridge: error shutting down "+name+": "+err.Error())
		}
	}
	servers = make(map[string]*mcpServerState)
}

func onBeforeToolCall(raw json.RawMessage) {
	var p beforeToolCallPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return
	}

	// Find which server owns this tool
	var serverName string
	var tool *mcpTool
	for name, state := range servers {
		for i := range state.Tools {
			if state.Tools[i].Name == p.ToolName {
				serverName = name
				tool = &state.Tools[i]
				break
			}
		}
		if tool != nil {
			break
		}
	}

	if tool == nil {
		// Not an MCP tool, ignore
		return
	}

	// Call the tool on the MCP server
	result, err := callMCPTool(servers[serverName], p.ToolName, p.Input)
	if err != nil {
		sendToolResult(p.ToolCallID, "mcp-bridge: "+err.Error(), true)
		return
	}

	sendToolResult(p.ToolCallID, result, false)
}

// MCP protocol functions (stubs for now - will implement in next steps)

func spawnServer(name string, cfg mcpServerConfig) error {
	// TODO: Call host_call("mcp_spawn", ...) to spawn subprocess
	// For now, stub
	servers[name] = &mcpServerState{
		Name:   name,
		Config: cfg,
		PID:    "stub-pid",
	}
	return nil
}

func shutdownServer(state *mcpServerState) error {
	// TODO: Call host_call("mcp_close", ...)
	return nil
}

func discoverTools(state *mcpServerState) error {
	// TODO: Send MCP initialize, then tools/list
	// For now, stub
	state.Tools = []mcpTool{}
	return nil
}

func registerTool(serverName string, tool mcpTool) error {
	type toolParams struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		InputSchema json.RawMessage `json:"input_schema"`
	}
	result := hostCallWithResponse("register_tool", toolParams{
		Name:        tool.Name,
		Description: tool.Description + " (via MCP server: " + serverName + ")",
		InputSchema: tool.InputSchema,
	})

	var envelope struct {
		Error string `json:"error"`
	}
	if result != "" {
		json.Unmarshal([]byte(result), &envelope)
	}
	if envelope.Error != "" {
		return &jsonError{envelope.Error}
	}
	return nil
}

func callMCPTool(state *mcpServerState, toolName string, input json.RawMessage) (string, error) {
	// TODO: Send tools/call JSON-RPC to server via host_call("mcp_send", ...)
	// and read response via host_call("mcp_read", ...)
	return "{}", nil
}

// Utility functions

func (e *jsonError) Error() string {
	return e.msg
}

func sendToolResult(toolCallID, result string, isError bool) {
	hostCallJSON("tool_result", map[string]any{
		"tool_call_id": toolCallID,
		"result":       result,
		"is_error":     isError,
	})
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

func logMsg(level int, msg string) {
	b := []byte(msg)
	if len(b) == 0 {
		return
	}
	hostLog(uint32(level), uint32(uintptr(unsafe.Pointer(&b[0]))), uint32(len(b)))
}

func itoa(n int) string {
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

func main() {}
