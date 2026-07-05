//go:build wasip1

// Package main is the skills built-in extension for the wllr coding harness.
// On session_start it scans ~/.wllr/skills/ for subdirectories containing
// SKILL.md. Each SKILL.md with user-invocable: true in its YAML frontmatter
// is registered as a slash command. When the command is invoked the skill
// body (frontmatter stripped) is set as the agent system prompt.
//
// Two tools are registered for programmatic skill discovery:
//
//	list_skills — returns a JSON array of all loaded skill metadata
//	get_skill   — returns the body of a named skill
package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"unsafe"
)

// ─── Local host_call for runtime calls (register_command, send_message) ───────
// These need to be fired at event-handler time, not during _init, so we
// can't use the SDK's deferred RegisterCommand. We use a local import alias.

//go:wasmimport env host_call
func _skillsHostCall(reqPtr, reqLen, respPtrPtr, respLenPtr uint32) uint32

func skillsCall(method string, params any) {
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
	_skillsHostCall(
		uint32(ptr), uint32(len(buf)),
		uint32(uintptr(unsafe.Pointer(&respPtr))),
		uint32(uintptr(unsafe.Pointer(&respLen))),
	)
}

// ─── Skill state ──────────────────────────────────────────────────────────────

// skillMeta holds the parsed frontmatter metadata for a skill.

// skillEntry holds both metadata and body for a loaded skill.

// absolute path to the SKILL.md file

// skills maps skill name to its loaded entry.
var skills map[string]skillEntry

// ─── Init ─────────────────────────────────────────────────────────────────────

func init() {
	RegisterToolWithOutput(
		"list_skills",
		"List all loaded skills with their metadata",
		json.RawMessage(`{"type":"object","properties":{}}`),
		json.RawMessage(
			`{"type":"array","items":{"type":"object","properties":{"name":{"type":"string"},"description":{"type":"string"},"category":{"type":"string"}}}}`,
		),
	)
	RegisterToolWithOutput(
		"get_skill",
		"Get the body content of a named skill (frontmatter stripped)",
		json.RawMessage(
			`{"type":"object","properties":{"name":{"type":"string","description":"Skill name (directory name)"}},"required":["name"]}`,
		),
		json.RawMessage(`{"type":"string","description":"Skill body text with frontmatter stripped"}`),
	)

	OnToolCall(func(callID, toolName string, input json.RawMessage) (string, bool) {
		switch toolName {
		case "list_skills":
			return handleListSkills()
		case "get_skill":
			return handleGetSkill(input)
		default:
			return "", false
		}
	})

	OnSessionStart(onSessionStart)

	// All on_command events are funnelled here; we dispatch to /skills or
	// a loaded skill by name.
	OnCommand("skills", onSkillsCommand)
	_sdkOn("on_command", func(payload json.RawMessage) {
		var p struct {
			Name string   `json:"name"`
			Args []string `json:"args"`
		}
		if err := json.Unmarshal(payload, &p); err != nil || p.Name == "skills" {
			return
		}
		entry, ok := skills[p.Name]
		if !ok {
			return
		}
		activateSkill(entry)
	})
}

// ─── Event handlers ───────────────────────────────────────────────────────────

func onSessionStart() {
	skills = make(map[string]skillEntry)

	home, err := os.UserHomeDir()
	if err != nil {
		Logf(2, "skills: could not determine home directory: %v", err)
		return
	}

	skillsDir := filepath.Join(home, ".wllr", "skills")
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		// Skills directory doesn't exist yet — nothing to load.
		return
	}

	loaded := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dirName := entry.Name()
		skillPath := filepath.Join(skillsDir, dirName, "SKILL.md")
		data, err := os.ReadFile(skillPath)
		if err != nil {
			continue
		}

		fm, body := parseFrontmatter(string(data))

		// Use frontmatter "name" as the command name (e.g. "bob:work");
		// fall back to directory name if absent.
		cmdName := dirName
		if fm != nil {
			if v := fm["name"]; v != "" {
				cmdName = v
			}
		}

		meta := skillMeta{Name: cmdName}
		if fm != nil {
			if v := fm["description"]; v != "" {
				meta.Description = v
			}
			if v := fm["category"]; v != "" {
				meta.Category = v
			}
		}
		if meta.Description == "" {
			meta.Description = cmdName + " skill"
		}

		skills[cmdName] = skillEntry{meta: meta, body: body, filePath: skillPath}

		// Only register user-invocable skills as slash commands.
		// These are runtime registrations (not during _init), so we call the
		// host directly rather than using the SDK's deferred RegisterCommand.
		if fm != nil && fm["user-invocable"] == "true" {
			skillsCall("register_command", map[string]string{
				"name":        cmdName,
				"description": meta.Description,
			})
		}

		Logf(1, "skills: loaded skill %s", cmdName)
		loaded++
	}

	// Register /skills command after loading.
	skillsCall("register_command", map[string]string{
		"name":        "skills",
		"description": "List loaded skills",
	})

	if loaded > 0 {
		Logf(1, "skills: loaded %d skill(s)", loaded)
		appendSkillsToPrompt()
	}
}

// appendSkillsToPrompt adds an <available_skills> XML block to the system
// prompt, matching pi's format so the LLM knows to use read_file to load
// a skill when the task matches its description.
func appendSkillsToPrompt() {
	if len(skills) == 0 {
		return
	}
	text := "\n\nThe following skills provide specialized instructions for specific tasks.\n" +
		"Use the read_file tool to load a skill's file when the task matches its description.\n" +
		"When a skill file references a relative path, resolve it against the skill directory " +
		"(parent of SKILL.md) and use that absolute path in tool commands.\n\n" +
		"<available_skills>"
	for _, entry := range skills {
		text += "\n  <skill>"
		text += "\n    <name>" + entry.meta.Name + "</name>"
		text += "\n    <description>" + entry.meta.Description + "</description>"
		text += "\n    <location>" + entry.filePath + "</location>"
		text += "\n  </skill>"
	}
	text += "\n</available_skills>"
	AppendSystemPrompt(text)
}

func onSkillsCommand(_ []string) {
	if len(skills) == 0 {
		Modal("No skills loaded.\n\nAdd SKILL.md files to ~/.wllr/skills/<name>/")
		return
	}
	var lines []string
	for _, entry := range skills {
		line := "/" + entry.meta.Name
		if entry.meta.Description != "" {
			line += "\n  " + entry.meta.Description
		}
		lines = append(lines, line)
	}
	text := "Skills\n" + strings.Repeat("─", 40) + "\n\n"
	for _, l := range lines {
		text += l + "\n\n"
	}
	Modal(strings.TrimRight(text, "\n"))
}

func activateSkill(entry skillEntry) {
	// Inject the skill as a user message using the pi Agent Skills XML format:
	//   <skill name="..." location="...">
	//   [content]
	//   </skill>
	// The LLM reads the skill instructions from the block and acts accordingly.
	// This preserves the AGENTS.md system prompt rather than replacing it.
	baseDir := filepath.Dir(entry.filePath)
	skillMsg := "<skill name=\"" + entry.meta.Name + "\" location=\"" + entry.filePath + "\">\n" +
		"References are relative to " + baseDir + ".\n\n" +
		entry.body + "\n</skill>"
	type msgParams struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	skillsCall("send_message", msgParams{Role: "user", Content: skillMsg})
	Logf(1, "skills: activated skill %s", entry.meta.Name)
}

// ─── Tool handlers ────────────────────────────────────────────────────────────

func handleListSkills() (string, bool) {
	metas := make([]skillMeta, 0, len(skills))
	for _, entry := range skills {
		metas = append(metas, entry.meta)
	}
	out, err := json.Marshal(metas)
	if err != nil {
		return "list_skills: marshal error", true
	}
	return string(out), false
}

func handleGetSkill(input json.RawMessage) (string, bool) {
	var req struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(input, &req); err != nil || req.Name == "" {
		return "get_skill: name is required", true
	}
	entry, ok := skills[req.Name]
	if !ok {
		return "get_skill: skill not found: " + req.Name, true
	}
	return entry.body, false
}

// ─── Frontmatter parsing ──────────────────────────────────────────────────────

// parseFrontmatter parses a SKILL.md file and returns the frontmatter fields
// as a map[string]string plus the body text (everything after the closing ---).
// If the file does not start with ---, returns nil map and the full content as body.
func parseFrontmatter(content string) (map[string]string, string) {
	if !strings.HasPrefix(content, "---\n") {
		return nil, strings.TrimSpace(content)
	}

	// Find the closing ---
	rest := content[4:] // skip opening ---\n
	end := strings.Index(rest, "\n---\n")
	if end < 0 {
		// Check for trailing --- at end of file.
		trimmed := strings.TrimRight(rest, "\n")
		if strings.HasSuffix(trimmed, "\n---") {
			idx := strings.LastIndex(rest, "\n---")
			fm := parseFrontmatterFields(rest[:idx])
			return fm, ""
		}
		return nil, strings.TrimSpace(content)
	}

	fmText := rest[:end]
	body := strings.TrimSpace(rest[end+5:]) // skip \n---\n
	fm := parseFrontmatterFields(fmText)
	return fm, body
}

// parseFrontmatterFields parses simple "key: value" lines into a map.
// Quoted string values have surrounding quotes stripped.
func parseFrontmatterFields(text string) map[string]string {
	fields := make(map[string]string)
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.Index(line, ":")
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		// Strip surrounding quotes from string values.
		if len(val) >= 2 && val[0] == '"' && val[len(val)-1] == '"' {
			val = val[1 : len(val)-1]
		}
		fields[key] = val
	}
	return fields
}

func main() {}
