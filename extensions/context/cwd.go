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

// promptComponent mirrors the host's sdk.SystemPromptComponent for the guest
// side: one labeled slice of the final system prompt, reported via
// set_system_prompt_components so the /context breakdown can attribute the
// prompt's cost to its sources.
type promptComponent struct {
	Source string `json:"source"`
	Chars  int    `json:"chars"`
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

func readFirst(paths []string) (string, string) {
	for _, path := range paths {
		if data, err := os.ReadFile(path); err == nil && len(data) > 0 {
			Log(1, "prompt: loaded "+path)
			return strings.TrimSpace(string(data)), path
		}
	}
	return "", ""
}

func findAndReadContextFile() (string, string) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", ""
	}
	for dir := cwd; ; dir = filepath.Dir(dir) {
		for _, filename := range []string{"AGENTS.md", "CLAUDE.md"} {
			path := filepath.Join(dir, filename)
			if data, readErr := os.ReadFile(path); readErr == nil && len(data) > 0 {
				Log(1, "prompt: loaded "+path)
				return strings.TrimSpace(string(data)), path
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", ""
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

Use the edit_file tool for targeted edits to an existing single file. This is the required first choice for source, test, configuration, and documentation changes: provide exact oldText/newText replacements and let the tool validate and apply them atomically. Do not use sed, perl, Python, awk, ed, cat >, tee, shell redirection, or other shell/script techniques to modify files. Use rg or read_file for inspection only. Use write_file only when creating a new file or intentionally replacing an entire file. Use exec for commands that execute or inspect work, not as a substitute editor. If edit_file is unavailable, stop and report that limitation rather than silently switching to a shell editing command. apply_patch is a Codex-side editing capability and is not a wllr runtime command; use edit_file inside wllr.`

type promptConfig struct {
	Override string   `json:"prompt_override"`
	Files    []string `json:"prompt_files"`
}

// promptPart is one join unit of the final system prompt: the text plus the
// source label reported to the host. The prompt extension glues the dynamic
// tools/commands section onto the base part with "\n\n" (historical byte
// layout); every later part joins with "\n\n---\n\n".
type promptPart struct {
	source string
	text   string
}

func buildPrompt(tools []promptTool, commands []promptCommand) string {
	prompt, _ := buildPromptParts(tools, commands)
	return prompt
}

// buildPromptParts assembles the system prompt and, alongside it, the labeled
// decomposition of what was included. Components are one entry per label; the
// dynamic tools/commands label shares the base's join unit, so the components'
// chars sum trails the prompt's real length only by the join separators.
func buildPromptParts(tools []promptTool, commands []promptCommand) (string, []promptComponent) {
	var cfg promptConfig
	if raw := ConfigReadGroup("wllr"); raw != nil {
		_ = json.Unmarshal(raw, &cfg)
	}
	return buildPromptWithConfigParts(tools, commands, cfg)
}

func buildPromptWithConfig(tools []promptTool, commands []promptCommand, cfg promptConfig) string {
	prompt, _ := buildPromptWithConfigParts(tools, commands, cfg)
	return prompt
}

func buildPromptWithConfigParts(
	tools []promptTool,
	commands []promptCommand,
	cfg promptConfig,
) (string, []promptComponent) {
	var parts []promptPart
	var components []promptComponent
	// addPart appends a join unit and its attribution label.
	addPart := func(source, text string) {
		if text == "" {
			return
		}
		parts = append(parts, promptPart{source: source, text: text})
		components = append(components, promptComponent{Source: source, Chars: len(text)})
	}
	// addLabel attributes text to its own label but glues it onto the most
	// recent join unit, preserving the historical base+dynamic byte layout.
	addLabel := func(source, text string) {
		if text == "" {
			return
		}
		if len(parts) > 0 {
			parts[len(parts)-1].text += "\n\n" + text
		} else {
			parts = append(parts, promptPart{source: source, text: text})
		}
		components = append(components, promptComponent{Source: source, Chars: len(text)})
	}

	base := builtInPrompt
	baseSource := "built-in rules"
	if strings.TrimSpace(cfg.Override) != "" {
		base = strings.TrimSpace(cfg.Override)
		baseSource = "prompt_override"
	}
	addPart(baseSource, base)
	addLabel("tools & commands", dynamicPrompt(tools, commands))
	for _, path := range cfg.Files {
		addPart("file:"+expandPromptPath(path), readPromptFile(path))
	}
	if content, path := readFirst(globalPaths()); content != "" {
		addPart("file:"+path, content)
	}
	if content, path := findAndReadContextFile(); content != "" {
		addPart("file:"+path, content)
	}
	if cwd, err := os.Getwd(); err == nil && cwd != "" {
		addPart("cwd note", cwdNote(cwd))
	}
	texts := make([]string, 0, len(parts))
	for _, part := range parts {
		texts = append(texts, part.text)
	}
	return strings.TrimSpace(strings.Join(texts, "\n\n---\n\n")), components
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
			b.WriteString(
				"\n\n### Code Intelligence\n\n- For coding work, LSP tools are the primary tools for diagnostics, linting, code navigation, finding references, and refactor reconnaissance.\n- At the start of repo/code work, call lsp_capabilities unless you already know the available LSP backends and output contracts from this session.\n- Before broad grep, rg, find, or large read_file sweeps, use lsp_symbols, lsp_definition, or lsp_references when the question is about code structure, definitions, call sites, or usages.\n- Before renames or shared API refactors, use lsp_refactor_preview; use exec/manual search as a fallback when LSP output is unavailable, incomplete, or unrelated.",
			)
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
