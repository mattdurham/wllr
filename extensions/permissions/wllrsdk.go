//go:build wasip1

package main

import (
	"encoding/json"
	"fmt"
	"unsafe"
)

//go:wasmimport env host_log
func hostLog(level uint32, ptr uint32, length uint32)

//go:wasmimport env host_call
func hostCall(reqPtr uint32, reqLen uint32, respPtrPtr uint32, respLenPtr uint32) uint32

// Event represents an event dispatched from the host.

// EventResponse is returned by OnEvent to signal cancellation, blocking, or errors.

// HostCallRequest is the JSON-RPC envelope for host_call.

// HostCallResponse is the JSON-RPC response from host_call.

//go:wasmexport _alloc
func _alloc(size uint32) uint32 {
	buf := make([]byte, size)
	return uint32(uintptr(unsafe.Pointer(&buf[0])))
}

//go:wasmexport _free
func _free(ptr uint32) {
	// No-op: Go GC handles this.
}

//go:wasmexport _init
func _init() int32 {
	// init() is called automatically by Go runtime before _init export is invoked.
	return 0
}

//go:wasmexport _on_event
func _on_event(ptr uint32, length uint32) uint32 {
	data := ptrToBytes(ptr, length)
	var evt Event
	if err := json.Unmarshal(data, &evt); err != nil {
		Logf("error", "_on_event: unmarshal: %v", err)
		return 0
	}

	resp := OnEvent(evt)
	if resp == nil {
		return 0
	}

	respJSON, err := json.Marshal(resp)
	if err != nil {
		Logf("error", "_on_event: marshal response: %v", err)
		return 0
	}

	respPtr := _alloc(uint32(len(respJSON)))
	copy(ptrToBytes(respPtr, uint32(len(respJSON))), respJSON)
	return respPtr
}

// OnEvent is the user-defined event handler. Override this in your extension.
var OnEvent = func(evt Event) *EventResponse { return nil }

func ptrToBytes(ptr, length uint32) []byte {
	return unsafe.Slice((*byte)(unsafe.Pointer(uintptr(ptr))), length)
}

func hostCallJSON(method string, params any) (json.RawMessage, error) {
	paramsJSON, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	req := HostCallRequest{Method: method, Params: paramsJSON}
	reqJSON, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	reqPtr := _alloc(uint32(len(reqJSON)))
	defer _free(reqPtr)
	copy(ptrToBytes(reqPtr, uint32(len(reqJSON))), reqJSON)

	var respPtr, respLen uint32
	status := hostCall(
		reqPtr,
		uint32(len(reqJSON)),
		uint32(uintptr(unsafe.Pointer(&respPtr))),
		uint32(uintptr(unsafe.Pointer(&respLen))),
	)
	if status != 0 {
		return nil, fmt.Errorf("host_call status %d", status)
	}
	if respPtr == 0 {
		return nil, nil
	}

	defer _free(respPtr)
	respJSON := make([]byte, respLen)
	copy(respJSON, ptrToBytes(respPtr, respLen))

	var resp HostCallResponse
	if err := json.Unmarshal(respJSON, &resp); err != nil {
		return nil, err
	}
	if resp.Error != "" {
		return nil, fmt.Errorf("%s", resp.Error)
	}
	return resp.Result, nil
}

// Log writes a log message at the given level.
func Log(level string, msg string) {
	var lvl uint32
	switch level {
	case "debug":
		lvl = 0
	case "info":
		lvl = 1
	case "warn":
		lvl = 2
	case "error":
		lvl = 3
	default:
		lvl = 1
	}
	ptr := _alloc(uint32(len(msg)))
	defer _free(ptr)
	copy(ptrToBytes(ptr, uint32(len(msg))), []byte(msg))
	hostLog(lvl, ptr, uint32(len(msg)))
}

// Logf writes a formatted log message.
func Logf(level string, format string, args ...any) {
	Log(level, fmt.Sprintf(format, args...))
}

// Subscribe registers interest in an event.
func Subscribe(event string) error {
	_, err := hostCallJSON("subscribe", map[string]string{"event": event})
	return err
}

// SetStatus sets a status bar value.
func SetStatus(key, value string) error {
	_, err := hostCallJSON("set_status", map[string]string{"key": key, "value": value})
	return err
}

// ToolResult sends a tool result back to the host.
func ToolResult(toolCallID, result string, isError bool) error {
	_, err := hostCallJSON("tool_result", map[string]any{
		"tool_call_id": toolCallID,
		"result":       result,
		"is_error":     isError,
	})
	return err
}

// ConfigRead reads the extension's configuration from the host.
func ConfigRead() (json.RawMessage, error) {
	return hostCallJSON("config_read", map[string]string{})
}

// GetEnv reads an environment variable from the host.
func GetEnv(name string) (string, error) {
	result, err := hostCallJSON("get_env", map[string]string{"name": name})
	if err != nil {
		return "", err
	}
	var resp struct {
		Value string `json:"value"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return "", err
	}
	return resp.Value, nil
}
