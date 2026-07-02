package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type backendInfo struct {
	Language          string   `json:"language"`
	Extensions        []string `json:"extensions"`
	DiagnosticCommand string   `json:"diagnostic_command,omitempty"`
	LSPServer         string   `json:"lsp_server,omitempty"`
}

func capabilities() string {
	languages := make([]string, 0, len(lspCommands))
	for lang := range lspCommands {
		languages = append(languages, lang)
	}
	sort.Strings(languages)

	backends := make([]backendInfo, 0, len(languages))
	for _, lang := range languages {
		backends = append(backends, backendInfo{
			Language:          lang,
			Extensions:        extensionsForLanguage(lang),
			DiagnosticCommand: diagnosticCommands[lang],
			LSPServer:         lspCommands[lang],
		})
	}

	data, _ := json.MarshalIndent(map[string]any{
		"tools": []string{
			"lsp_diagnostics: run file-scoped diagnostics",
			"lsp_lint: run project or file validation",
			"lsp_symbols: list likely symbols in a file",
			"lsp_definition: find likely definition sites",
			"lsp_references: find likely symbol references",
			"lsp_refactor_preview: preview references before rename/refactor edits",
		},
		"backends": backends,
		"note":     "These tools provide agent-facing code intelligence. Apply edits with normal file-editing tools after reviewing results.",
	}, "", "  ")
	return string(data)
}

func runLint(input map[string]any) (string, bool) {
	path := stringVal(input, "path")
	if path == "" {
		path = stringVal(input, "file")
	}
	if path == "" {
		path = "."
	}

	if strings.HasSuffix(path, ".go") || fileExists(filepath.Join(path, "go.mod")) || fileExists(filepath.Join(dirForPath(path), "go.mod")) {
		out, err := runCommand("go test ./...", dirForPath(path))
		return commandJSON("lint", path, "go test ./...", out, err), false
	}

	if fileExists(path) && filepath.Ext(path) != "" {
		return runDiagnostics(map[string]any{"file": path})
	}

	return diagnosticJSON(path, "", true, "no lint backend configured for this path"), false
}

func runDiagnostics(input map[string]any) (string, bool) {
	file := stringVal(input, "file")
	if file == "" {
		return `{"ok":false,"error":"file parameter required"}`, true
	}

	lang := detectLanguage(file)
	if lang == "" {
		return diagnosticJSON(file, "", true, "unsupported or unknown file type"), false
	}

	cmd, ok := diagnosticCommands[lang]
	if !ok {
		return diagnosticJSON(file, lang, true, "no diagnostic command configured for this language"), false
	}

	parts := strings.Fields(cmd)
	if len(parts) == 0 {
		return diagnosticJSON(file, lang, true, "no diagnostic command configured for this language"), false
	}
	command := strings.Join(append(parts, shellQuote(file)), " ")
	out, err := runCommand(command, "")
	return commandJSON("diagnostics", file, command, out, err), false
}

func listSymbols(input map[string]any) (string, bool) {
	file := stringVal(input, "file")
	if file == "" {
		return `{"ok":false,"error":"file parameter required"}`, true
	}

	pattern := definitionPattern(detectLanguage(file), "")
	out, err := runCommand(fmt.Sprintf("rg -n --no-heading %s %s", shellQuote(pattern), shellQuote(file)), "")
	return searchJSON("symbols", file, pattern, out, err), false
}

func findDefinitions(input map[string]any) (string, bool) {
	symbol := stringVal(input, "symbol")
	if symbol == "" {
		return `{"ok":false,"error":"symbol parameter required"}`, true
	}

	path := pathOrDot(input)
	pattern := definitionPattern(detectLanguage(path), symbol)
	out, err := runCommand(fmt.Sprintf("rg -n --no-heading %s %s", shellQuote(pattern), shellQuote(path)), "")
	return searchJSON("definition", path, pattern, out, err), false
}

func findReferences(input map[string]any) (string, bool) {
	symbol := stringVal(input, "symbol")
	if symbol == "" {
		return `{"ok":false,"error":"symbol parameter required"}`, true
	}

	path := pathOrDot(input)
	pattern := symbolPattern(symbol)
	out, err := runCommand(fmt.Sprintf("rg -n --no-heading %s %s", shellQuote(pattern), shellQuote(path)), "")
	return searchJSON("references", path, pattern, out, err), false
}

func refactorPreview(input map[string]any) (string, bool) {
	symbol := stringVal(input, "symbol")
	newName := stringVal(input, "new_name")
	if symbol == "" || newName == "" {
		return `{"ok":false,"error":"symbol and new_name parameters required"}`, true
	}

	path := pathOrDot(input)
	pattern := symbolPattern(symbol)
	out, err := runCommand(fmt.Sprintf("rg -n --no-heading %s %s", shellQuote(pattern), shellQuote(path)), "")
	result := map[string]any{
		"kind":     "refactor_preview",
		"path":     path,
		"symbol":   symbol,
		"new_name": newName,
		"pattern":  pattern,
		"matches":  jsonStringLines(out),
		"ok":       err == nil || strings.TrimSpace(out) != "",
		"note":     "Preview only. Review matches and apply edits with normal file-editing tools.",
	}
	if err != nil && strings.TrimSpace(out) == "" {
		result["error"] = err.Error()
	}
	data, _ := json.MarshalIndent(result, "", "  ")
	return string(data), false
}

func extensionsForLanguage(language string) []string {
	var exts []string
	for ext, lang := range extToLang {
		if lang == language {
			exts = append(exts, ext)
		}
	}
	sort.Strings(exts)
	return exts
}

func detectLanguage(filename string) string {
	return extToLang[strings.ToLower(filepath.Ext(filename))]
}

func stringVal(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return v
}

func pathOrDot(input map[string]any) string {
	path := stringVal(input, "path")
	if path == "" {
		return "."
	}
	return path
}

func symbolPattern(symbol string) string {
	return `\b` + regexp.QuoteMeta(symbol) + `\b`
}

func definitionPattern(language, symbol string) string {
	name := `[A-Za-z_][A-Za-z0-9_]*`
	if symbol != "" {
		name = regexp.QuoteMeta(symbol)
	}

	switch language {
	case "go":
		return `\b(func|type|var|const)\s+` + name + `\b`
	case "python":
		return `^\s*(def|class)\s+` + name + `\b`
	case "rust":
		return `\b(fn|struct|enum|trait|impl|mod|const|static)\s+` + name + `\b`
	case "javascript", "typescript":
		return `\b(function|class|const|let|var|interface|type)\s+` + name + `\b`
	default:
		if symbol != "" {
			return symbolPattern(symbol)
		}
		return `\b(func|function|def|class|type|struct|enum|const|let|var)\s+` + name + `\b`
	}
}

func commandJSON(kind, target, command string, output string, err error) string {
	return diagnosticJSONWithCommand(kind, target, detectLanguage(target), command, output, err)
}

func diagnosticJSON(target, language string, ok bool, output string) string {
	data, _ := json.MarshalIndent(map[string]any{
		"target":   target,
		"language": language,
		"ok":       ok,
		"output":   cleanOutput(output),
	}, "", "  ")
	return string(data)
}

func diagnosticJSONWithCommand(kind, target, language, command, output string, err error) string {
	ok := err == nil
	body := map[string]any{
		"kind":     kind,
		"target":   target,
		"language": language,
		"command":  command,
		"ok":       ok,
		"output":   cleanOutput(output),
	}
	if err != nil {
		body["error"] = err.Error()
		if strings.TrimSpace(output) != "" {
			body["ok"] = false
		}
	}
	data, _ := json.MarshalIndent(body, "", "  ")
	return string(data)
}

func searchJSON(kind, target, pattern, output string, err error) string {
	trimmed := strings.TrimSpace(output)
	body := map[string]any{
		"kind":    kind,
		"target":  target,
		"pattern": pattern,
		"ok":      err == nil || trimmed != "",
		"matches": jsonStringLines(output),
	}
	if err != nil && trimmed == "" {
		body["error"] = err.Error()
	}
	data, _ := json.MarshalIndent(body, "", "  ")
	return string(data)
}

func cleanOutput(output string) string {
	output = strings.TrimSpace(output)
	if output == "" {
		return "no issues found"
	}
	return output
}

func jsonStringLines(output string) []string {
	lines := stringLines(output)
	if len(lines) > 80 {
		return lines[:80]
	}
	return lines
}

func stringLines(output string) []string {
	output = strings.TrimSpace(output)
	if output == "" {
		return []string{}
	}
	return strings.Split(output, "\n")
}

func dirForPath(path string) string {
	if path == "" || path == "." {
		return "."
	}
	if strings.HasSuffix(path, ".go") || filepath.Ext(path) != "" {
		dir := filepath.Dir(path)
		if dir == "" {
			return "."
		}
		return dir
	}
	return path
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

var diagnosticCommands = map[string]string{
	"go":     "go vet",
	"python": "python3 -m py_compile",
	"rust":   "rustc --edition=2021 --error-format=json",
}

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
