package main

// This file holds host-testable CWD injection logic.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type sessionPromptPayload struct {
	Tools    []promptTool    `json:"tools"`
	Commands []promptCommand `json:"commands"`
}
type promptTool struct {
	Name string `json:"name"`
}
type promptCommand struct {
	Name string `json:"name"`
	Desc string `json:"description"`
}

func cwdNote(cwd string) string {
	return "You are operating in the current working directory: " + cwd
}

func globalPaths() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return []string{filepath.Join(home, ".wllr", "AGENTS.md"), filepath.Join(home, ".wllr", "CLAUDE.md")}
}

func readFirst(paths []string) string {
	for _, path := range paths {
		if data, err := os.ReadFile(path); err == nil && len(data) > 0 {
			Log(1, "prompt: loaded "+path)
			return strings.TrimSpace(string(data))
		}
	}
	return ""
}

func findAndReadContextFile() string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	for dir := cwd; ; dir = filepath.Dir(dir) {
		for _, filename := range []string{"AGENTS.md", "CLAUDE.md"} {
			path := filepath.Join(dir, filename)
			if data, readErr := os.ReadFile(path); readErr == nil && len(data) > 0 {
				Log(1, "prompt: loaded "+path)
				return strings.TrimSpace(string(data))
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
	}
}

const builtInPrompt = `## Action Rules

You are an action-taking agent. Before each tool call write one short sentence explaining your decision or what you found — then immediately call the tool. Never write reasoning as comments inside shell commands; write it as text the user can read.

**The failure mode to avoid:** writing "Let me start", "Now I'll", "Next I'll", or "I'll write..." — then stopping. That announces an action without taking it. If you plan to do something, do it in the same response.

**The correct pattern:** one sentence of reasoning → tool call → one sentence summarizing the result → next tool call. Keep going until the task is fully done or you need input.

**Never end a turn by describing your next action.** Either call the tool now, or tell the user the task is complete.

### Project Scope

Treat the current working directory where wllr was launched as the project root. By default, scope file reads, searches, edits, tests, and shell commands to that directory and its descendants. Prefer relative paths and omit exec.dir so commands run in the current project. Do not inspect parent directories, home directories, sibling repositories, or unrelated folders unless the user explicitly asks or the task requires it. When a question is about the current project, investigate it from the current directory first.

### Editing Files

Use the edit_file tool for source-code edits: provide exact oldText/newText replacements and let the tool validate and apply them atomically. Do not use sed, perl, Python, or shell redirection to modify files. Use rg or read_file for inspection only. apply_patch is a Codex-side editing capability and is not a wllr runtime command; use edit_file inside wllr.`

type promptConfig struct {
	Override string   `json:"prompt_override"`
	Files    []string `json:"prompt_files"`
}

func buildPrompt(tools []promptTool, commands []promptCommand) string {
	var cfg promptConfig
	if raw := ConfigReadGroup("wllr"); raw != nil {
		_ = json.Unmarshal(raw, &cfg)
	}
	return buildPromptWithConfig(tools, commands, cfg)
}

func buildPromptWithConfig(tools []promptTool, commands []promptCommand, cfg promptConfig) string {
	base := builtInPrompt
	if dynamic := dynamicPrompt(tools, commands); dynamic != "" {
		base += "\n\n" + dynamic
	}
	if strings.TrimSpace(cfg.Override) != "" {
		base = strings.TrimSpace(cfg.Override)
		if dynamic := dynamicPrompt(tools, commands); dynamic != "" {
			base += "\n\n" + dynamic
		}
	}
	for _, path := range cfg.Files {
		if text := readPromptFile(path); text != "" {
			base += "\n\n---\n\n" + text
		}
	}
	parts := []string{base}
	if content := readFirst(globalPaths()); content != "" {
		parts = append(parts, content)
	}
	if content := findAndReadContextFile(); content != "" {
		parts = append(parts, content)
	}
	if cwd, err := os.Getwd(); err == nil && cwd != "" {
		parts = append(parts, cwdNote(cwd))
	}
	return strings.TrimSpace(strings.Join(parts, "\n\n---\n\n"))
}

func readPromptFile(path string) string {
	path = expandPromptPath(path)
	data, err := os.ReadFile(path)
	if err != nil {
		Logf(2, "prompt: could not read %s: %v", path, err)
		return ""
	}
	return strings.TrimSpace(string(data))
}

func expandPromptPath(path string) string {
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

func dynamicPrompt(tools []promptTool, commands []promptCommand) string {
	var b strings.Builder
	if len(tools) > 0 {
		names := make([]string, 0, len(tools))
		for _, tool := range tools {
			names = append(names, tool.Name)
		}
		sort.Strings(names)
		b.WriteString("Available tools: " + strings.Join(names, ", "))
		if hasCodeIntelligence(tools) {
			b.WriteString("\n\n### Code Intelligence\n\n- For coding work, LSP tools are the primary tools for diagnostics, linting, code navigation, finding references, and refactor reconnaissance.\n- At the start of repo/code work, call lsp_capabilities unless you already know the available LSP backends and output contracts from this session.\n- Before broad grep, rg, find, or large read_file sweeps, use lsp_symbols, lsp_definition, or lsp_references when the question is about code structure, definitions, call sites, or usages.\n- Before renames or shared API refactors, use lsp_refactor_preview; use exec/manual search as a fallback when LSP output is unavailable, incomplete, or unrelated.")
		}
	}
	if len(commands) > 0 {
		b.WriteString("\n\n### Slash commands\n\n")
		for _, command := range commands {
			desc := command.Desc
			if desc == "" {
				desc = "(no description)"
			}
			b.WriteString("- **/" + command.Name + "** — " + desc + "\n")
		}
	}
	return strings.TrimSpace(b.String())
}

func hasCodeIntelligence(tools []promptTool) bool {
	for _, tool := range tools {
		switch tool.Name {
		case "lsp_diagnostics", "lsp_lint", "lsp_definition", "lsp_references":
			return true
		}
	}
	return false
}
