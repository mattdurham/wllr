//go:build wasip1

// Package main is the context built-in extension for the wllr coding harness.
// On session_start it reads AGENTS.md (falling back to CLAUDE.md) from
// ~/.wllr/ and the current working directory, then injects the combined
// content as the agent system prompt via set_system_prompt.
//
// Lookup order (first match wins for each scope):
//
//	Global: ~/.wllr/AGENTS.md → ~/.wllr/CLAUDE.md
//	CWD:    ./AGENTS.md        → ./CLAUDE.md
//
// Both global and CWD content are combined (CWD appended after global).
package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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
	return hostCallJSON("subscribe", map[string]string{"event": "session_start"})
}

//go:wasmexport _on_event
func extensionOnEvent(ptr, length int32) int32 {
	data := unsafe.Slice((*byte)(unsafe.Pointer(uintptr(ptr))), length)
	var evt struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &evt); err != nil {
		return 0
	}
	if evt.Type == "session_start" {
		onSessionStart()
	}
	return 0
}

func onSessionStart() {
	var parts []string

	// Global context: ~/.wllr/AGENTS.md or CLAUDE.md
	if content := readFirst(globalPaths()); content != "" {
		parts = append(parts, content)
	}

	// CWD context: ./AGENTS.md or CLAUDE.md
	if content := readFirst([]string{"AGENTS.md", "CLAUDE.md"}); content != "" {
		parts = append(parts, content)
	}

	if len(parts) == 0 {
		return
	}

	prompt := strings.Join(parts, "\n\n---\n\n")
	logMsg(1, "context: loaded system prompt ("+itoa(len(prompt))+" bytes)")

	type params struct {
		Prompt string `json:"prompt"`
	}
	hostCallJSON("set_system_prompt", params{Prompt: prompt})
}

func globalPaths() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return []string{
		filepath.Join(home, ".wllr", "AGENTS.md"),
		filepath.Join(home, ".wllr", "CLAUDE.md"),
	}
}

func readFirst(paths []string) string {
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err == nil && len(data) > 0 {
			logMsg(1, "context: loaded "+p)
			return strings.TrimSpace(string(data))
		}
	}
	return ""
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
