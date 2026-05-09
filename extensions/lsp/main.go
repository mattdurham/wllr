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
)

func init() {
	RegisterTool(
		"lsp_detect",
		"Detect installed LSP servers on the system",
		json.RawMessage(`{"type":"object","properties":{}}`),
	)
	RegisterTool(
		"lsp_start",
		"Check whether an LSP server is available for a language and return the command to start it",
		json.RawMessage(`{"type":"object","properties":{"language":{"type":"string","description":"Language name (go, python, rust, typescript, etc.)"},"file":{"type":"string","description":"File path to auto-detect language from (optional)"}},"required":[]}`),
	)
	RegisterTool(
		"lsp_diagnostics",
		"Run a language-specific diagnostic check on a file (e.g. go vet, pylint)",
		json.RawMessage(`{"type":"object","properties":{"file":{"type":"string","description":"File path to check"}},"required":["file"]}`),
	)

	OnToolCall(func(callID, toolName string, input json.RawMessage) (string, bool) {
		var m map[string]any
		_ = json.Unmarshal(input, &m)

		switch toolName {
		case "lsp_detect":
			return detectServers(), false
		case "lsp_start":
			return startServer(m)
		case "lsp_diagnostics":
			return runDiagnostics(m)
		default:
			return "", false
		}
	})
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

func stringVal(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return v
}

func main() {}
