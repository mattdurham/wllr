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

// skillMeta holds the parsed frontmatter metadata for a skill.
type skillMeta struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Category    string `json:"category"`
}

// skillEntry holds both metadata and body for a loaded skill.
type skillEntry struct {
	meta     skillMeta
	body     string
	filePath string // absolute path to the SKILL.md file
}

// skills maps skill directory name to its loaded entry.
var skills map[string]skillEntry

//go:wasmexport _init
func extensionInit() int32 {
	skills = make(map[string]skillEntry)

	if rc := hostCallJSON("subscribe", map[string]string{"event": "session_start"}); rc != 0 {
		return rc
	}
	if rc := hostCallJSON("subscribe", map[string]string{"event": "on_command"}); rc != 0 {
		return rc
	}

	// Register list_skills tool.
	if rc := registerTool(
		"list_skills",
		"List all loaded skills with their metadata",
		`{"type":"object","properties":{}}`,
	); rc != 0 {
		return rc
	}

	// Register get_skill tool.
	if rc := registerTool(
		"get_skill",
		"Get the body content of a named skill (frontmatter stripped)",
		`{"type":"object","properties":{"name":{"type":"string","description":"Skill name (directory name)"}},"required":["name"]}`,
	); rc != 0 {
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
	case "on_command":
		onCommand(evt.Payload)
	case "before_tool_call":
		onBeforeToolCall(evt.Payload)
	}
	return 0
}

func onSessionStart() {
	home, err := os.UserHomeDir()
	if err != nil {
		logMsg(2, "skills: could not determine home directory: "+err.Error())
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

		meta := skillMeta{
			Name: cmdName,
		}
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
		if fm != nil && fm["user-invocable"] == "true" {
			type cmdParams struct {
				Name        string `json:"name"`
				Description string `json:"description"`
			}
			hostCallJSON("register_command", cmdParams{Name: cmdName, Description: meta.Description})
		}

		logMsg(1, "skills: loaded skill "+cmdName)
		loaded++
	}

	// Register /skills command after loading so OnRegisterCommand is wired.
	type cmdParams struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	hostCallJSON("register_command", cmdParams{Name: "skills", Description: "List loaded skills"})

	if loaded > 0 {
		logMsg(1, "skills: loaded "+itoa(loaded)+" skill(s)")
	}
}

func onCommand(raw json.RawMessage) {
	var payload struct {
		Name string   `json:"name"`
		Args []string `json:"args"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return
	}

	// /skills lists all loaded skill names and descriptions.
	if payload.Name == "skills" {
		if len(skills) == 0 {
			hostCallJSON("modal", map[string]string{"text": "No skills loaded.\n\nAdd SKILL.md files to ~/.wllr/skills/<name>/"})
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
		hostCallJSON("modal", map[string]string{"text": strings.TrimRight(text, "\n")})
		return
	}

	entry, ok := skills[payload.Name]
	if !ok {
		return
	}

	// Inject the skill as a user message using the pi Agent Skills XML format:
	//   <skill name="..." location="...">
	//   [content]
	//   </skill>
	// The LLM reads the skill instructions from the block and acts accordingly.
	// This preserves the AGENTS.md system prompt rather than replacing it.
	skillMsg := "<skill name=\"" + entry.meta.Name + "\" location=\"" + entry.filePath + "\">\n" +
		entry.body + "\n</skill>"
	type msgParams struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	hostCallJSON("send_message", msgParams{Role: "user", Content: skillMsg})
	logMsg(1, "skills: activated skill "+payload.Name)
}

type beforeToolCallPayload struct {
	ToolCallID string          `json:"tool_call_id"`
	ToolName   string          `json:"tool_name"`
	Input      json.RawMessage `json:"input"`
}

func onBeforeToolCall(raw json.RawMessage) {
	var p beforeToolCallPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return
	}
	switch p.ToolName {
	case "list_skills":
		handleListSkills(p)
	case "get_skill":
		handleGetSkill(p)
	}
}

func handleListSkills(p beforeToolCallPayload) {
	metas := make([]skillMeta, 0, len(skills))
	for _, entry := range skills {
		metas = append(metas, entry.meta)
	}
	out, err := json.Marshal(metas)
	if err != nil {
		sendToolResult(p.ToolCallID, "list_skills: marshal error", true)
		return
	}
	sendToolResult(p.ToolCallID, string(out), false)
}

func handleGetSkill(p beforeToolCallPayload) {
	var input struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(p.Input, &input); err != nil || input.Name == "" {
		sendToolResult(p.ToolCallID, "get_skill: name is required", true)
		return
	}
	entry, ok := skills[input.Name]
	if !ok {
		sendToolResult(p.ToolCallID, "get_skill: skill not found: "+input.Name, true)
		return
	}
	sendToolResult(p.ToolCallID, entry.body, false)
}

func registerTool(name, desc, inputSchema string) int32 {
	type toolParams struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		InputSchema json.RawMessage `json:"input_schema"`
	}
	rc := hostCallJSON("register_tool", toolParams{
		Name:        name,
		Description: desc,
		InputSchema: json.RawMessage(inputSchema),
	})
	if rc != 0 {
		return rc
	}
	return hostCallJSON("subscribe", map[string]string{"event": "before_tool_call"})
}

func sendToolResult(toolCallID, result string, isError bool) {
	hostCallJSON("tool_result", map[string]any{
		"tool_call_id": toolCallID,
		"result":       result,
		"is_error":     isError,
	})
}

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
