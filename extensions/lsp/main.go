//go:build wasip1

// Package main is the lsp extension for wllr.
// It provides tools to detect installed LSP servers and query language
// information via shell commands.
package main

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"unsafe"
)

// ─── WASM ABI ────────────────────────────────────────────────────────────────

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
	hostCallJSON("subscribe", map[string]string{"event": "before_tool_call"})

	tools := []struct {
		name   string
		desc   string
		schema string
	}{
		{
			"lsp_detect",
			"Detect installed LSP servers on the system",
			`{"type":"object","properties":{}}`,
		},
		{
			"lsp_start",
			"Check whether an LSP server is available for a language and return the command to start it",
			`{"type":"object","properties":{"language":{"type":"string","description":"Language name (go, python, rust, typescript, etc.)"},"file":{"type":"string","description":"File path to auto-detect language from (optional)"}},"required":[]}`,
		},
		{
			"lsp_diagnostics",
			"Run a language-specific diagnostic check on a file (e.g. go vet, pylint)",
			`{"type":"object","properties":{"file":{"type":"string","description":"File path to check"}},"required":["file"]}`,
		},
	}

	for _, t := range tools {
		hostCallJSON("register_tool", map[string]any{
			"name":         t.name,
			"description":  t.desc,
			"input_schema": json.RawMessage(t.schema),
		})
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
	if evt.Type != "before_tool_call" {
		return 0
	}

	var p struct {
		ToolCallID string          `json:"tool_call_id"`
		ToolName   string          `json:"tool_name"`
		Input      json.RawMessage `json:"input"`
	}
	if err := json.Unmarshal(evt.Payload, &p); err != nil {
		return 0
	}

	var input map[string]any
	_ = json.Unmarshal(p.Input, &input)

	var result string
	var isError bool

	switch p.ToolName {
	case "lsp_detect":
		result = detectServers()
	case "lsp_start":
		result, isError = startServer(input)
	case "lsp_diagnostics":
		result, isError = runDiagnostics(input)
	default:
		return 0 // not our tool
	}

	hostCallJSON("tool_result", map[string]any{
		"tool_call_id": p.ToolCallID,
		"result":       result,
		"is_error":     isError,
	})
	return 0
}

// ─── Tool implementations ─────────────────────────────────────────────────────

func detectServers() string {
	type entry struct {
		Language string `json:"language"`
		Command  string `json:"command"`
		Found    bool   `json:"found"`
	}
	var servers []entry
	for lang, cmd := range lspCommands {
		_, err := exec.LookPath(strings.Fields(cmd)[0])
		servers = append(servers, entry{
			Language: lang,
			Command:  cmd,
			Found:    err == nil,
		})
	}
	data, _ := json.MarshalIndent(map[string]any{"servers": servers}, "", "  ")
	return string(data)
}

func startServer(input map[string]any) (string, bool) {
	lang := stringVal(input, "language")
	if lang == "" {
		if file := stringVal(input, "file"); file != "" {
			lang = detectLanguage(file)
		}
	}
	if lang == "" {
		return "provide 'language' or 'file' parameter", true
	}
	cmd, ok := lspCommands[lang]
	if !ok {
		return fmt.Sprintf("no LSP server configured for language: %s", lang), true
	}
	_, err := exec.LookPath(strings.Fields(cmd)[0])
	if err != nil {
		return fmt.Sprintf("LSP server not installed for %s — install %s first", lang, cmd), true
	}
	return fmt.Sprintf(`{"language":%q,"command":%q,"status":"available — start with: %s"}`, lang, cmd, cmd), false
}

func runDiagnostics(input map[string]any) (string, bool) {
	file := stringVal(input, "file")
	if file == "" {
		return "file parameter required", true
	}
	lang := detectLanguage(file)

	var cmd *exec.Cmd
	switch lang {
	case "go":
		cmd = exec.Command("go", "vet", file)
	case "python":
		cmd = exec.Command("python3", "-m", "py_compile", file)
	case "rust":
		cmd = exec.Command("rustc", "--edition=2021", "--error-format=json", file)
	default:
		return fmt.Sprintf(`{"file":%q,"language":%q,"note":"no diagnostic command configured for this language"}`, file, lang), false
	}

	out, err := cmd.CombinedOutput()
	output := strings.TrimSpace(string(out))
	if err != nil {
		if output == "" {
			output = err.Error()
		}
		return fmt.Sprintf(`{"file":%q,"language":%q,"ok":false,"output":%q}`, file, lang, output), false
	}
	if output == "" {
		output = "no issues found"
	}
	return fmt.Sprintf(`{"file":%q,"language":%q,"ok":true,"output":%q}`, file, lang, output), false
}

// ─── Language / command tables ────────────────────────────────────────────────

var lspCommands = map[string]string{
	"go":         "gopls",
	"python":     "pylsp",
	"javascript": "typescript-language-server --stdio",
	"typescript": "typescript-language-server --stdio",
	"rust":       "rust-analyzer",
	"c":          "clangd",
	"cpp":        "clangd",
	"java":       "jdtls",
	"ruby":       "solargraph stdio",
	"php":        "intelephense --stdio",
	"csharp":     "omnisharp",
	"lua":        "lua-language-server",
	"bash":       "bash-language-server start",
	"json":       "vscode-json-languageserver --stdio",
	"yaml":       "yaml-language-server --stdio",
	"html":       "html-languageserver --stdio",
	"css":        "css-languageserver --stdio",
}

var extToLang = map[string]string{
	".go":   "go",
	".py":   "python",
	".js":   "javascript",
	".jsx":  "javascript",
	".ts":   "typescript",
	".tsx":  "typescript",
	".rs":   "rust",
	".c":    "c",
	".h":    "c",
	".cpp":  "cpp",
	".cc":   "cpp",
	".cxx":  "cpp",
	".hpp":  "cpp",
	".java": "java",
	".rb":   "ruby",
	".php":  "php",
	".cs":   "csharp",
	".lua":  "lua",
	".sh":   "bash",
	".json": "json",
	".yaml": "yaml",
	".yml":  "yaml",
	".html": "html",
	".css":  "css",
}

func detectLanguage(filename string) string {
	return extToLang[strings.ToLower(filepath.Ext(filename))]
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

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
	buf := make([]byte, len(reqBytes))
	copy(buf, reqBytes)
	ptr := uintptr(unsafe.Pointer(&buf[0]))
	var respPtr, respLen uint32
	hostCall(
		uint32(ptr), uint32(len(buf)),
		uint32(uintptr(unsafe.Pointer(&respPtr))),
		uint32(uintptr(unsafe.Pointer(&respLen))),
	)
	if respPtr != 0 {
		delete(pinned, uintptr(respPtr))
	}
}

func main() {}
