//go:build wasip1

// Package main is the agents built-in extension for the bob coding harness.
// It registers tools for spawning, managing, and messaging agents and teams,
// delegating all operations to the host via host_call.
package main

import (
	"encoding/json"
	"strings"
	"unsafe"
)

//go:wasmimport env host_log
func hostLog(level, ptr, length uint32)

//go:wasmimport env host_call
func hostCall(reqPtr, reqLen, respPtrPtr, respLenPtr uint32) uint32

var pinned = map[uintptr][]byte{}

// agentRecord tracks a running sub-agent's status for the /agents modal.
type agentRecord struct {
	id         string
	name       string
	task       string // initial prompt (truncated)
	lastUpdate string // most recent action
}

var agentRecords []agentRecord // ordered by creation

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

func upsertAgent(id, name, task, lastUpdate string) {
	for i := range agentRecords {
		if agentRecords[i].id == id {
			if name != "" {
				agentRecords[i].name = name
			}
			if task != "" {
				agentRecords[i].task = task
			}
			if lastUpdate != "" {
				agentRecords[i].lastUpdate = lastUpdate
			}
			return
		}
	}
	agentRecords = append(agentRecords, agentRecord{id: id, name: name, task: task, lastUpdate: lastUpdate})
}

func removeAgent(id string) {
	out := agentRecords[:0]
	for _, r := range agentRecords {
		if r.id != id {
			out = append(out, r)
		}
	}
	agentRecords = out
}

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
	tools := []struct {
		name   string
		desc   string
		schema string
	}{
		{
			"create_agent",
			"Create a new agent with a name, system prompt, and optional model",
			`{"type":"object","properties":{"name":{"type":"string","description":"Agent name"},"system_prompt":{"type":"string","description":"System prompt for the agent"},"prompt":{"type":"string","description":"Initial prompt to send"},"model":{"type":"string","description":"Model name (optional)"}},"required":["name","system_prompt","prompt"]}`,
		},
		{
			"shutdown_agent",
			"Shut down and remove a running agent",
			`{"type":"object","properties":{"agent_id":{"type":"string","description":"ID of the agent to shut down"}},"required":["agent_id"]}`,
		},
		{
			"list_agents",
			"List all running agents",
			`{"type":"object","properties":{}}`,
		},
		{
			"create_team",
			"Create a new team",
			`{"type":"object","properties":{"name":{"type":"string","description":"Team name"}},"required":["name"]}`,
		},
		{
			"add_to_team",
			"Add an agent to a team",
			`{"type":"object","properties":{"team_id":{"type":"string","description":"Team ID"},"agent_id":{"type":"string","description":"Agent ID"}},"required":["team_id","agent_id"]}`,
		},
		{
			"get_team",
			"Get information about a team",
			`{"type":"object","properties":{"team_id":{"type":"string","description":"Team ID"}},"required":["team_id"]}`,
		},
		{
			"shutdown_team",
			"Shut down a team and all its members",
			`{"type":"object","properties":{"team_id":{"type":"string","description":"Team ID"}},"required":["team_id"]}`,
		},
		{
			"send_message",
			"Send a message to an agent",
			`{"type":"object","properties":{"agent_id":{"type":"string","description":"Agent ID"},"message":{"type":"string","description":"Message text"}},"required":["agent_id","message"]}`,
		},
	}

	for _, t := range tools {
		if rc := registerTool(t.name, t.desc, t.schema); rc != 0 {
			return rc
		}
	}
	if rc := hostCallJSON("subscribe", map[string]string{"event": "session_start"}); rc != 0 {
		return rc
	}
	if rc := hostCallJSON("subscribe", map[string]string{"event": "on_command"}); rc != 0 {
		return rc
	}
	type cmdParams struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	return hostCallJSON(
		"register_command",
		cmdParams{Name: "agents", Description: "Show running sub-agents and their status"},
	)
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
	guidance := `
## Agent and Team Tools

Use these tools to spawn sub-agents and coordinate work across teams.

### How agents work
- create_agent spawns a sub-agent and sends its first prompt. The agent
  runs asynchronously; its response streams back through the session.
- send_message sends a follow-up message to a running agent. Each message
  is added to that agent's conversation history so context accumulates.
- There is no inbox to poll. Results from sub-agents arrive as part of
  the normal response stream — just continue reasoning after sending.
- shutdown_agent when the agent's task is complete.

### Workflow
1. create_agent — spawn with a focused system prompt and initial task
2. send_message — send follow-ups as needed (each adds to agent context)
3. shutdown_agent — clean up when done

Teams group agents for broadcast coordination:
create_team / add_to_team / shutdown_team`

	type params struct {
		Text string `json:"text"`
	}
	hostCallJSON("append_system_prompt", params{Text: guidance})
}

type beforeToolCallPayload struct {
	AgentID    string          `json:"agent_id"`
	ToolCallID string          `json:"tool_call_id"`
	ToolName   string          `json:"tool_name"`
	Input      json.RawMessage `json:"input"`
}

func onBeforeToolCall(raw json.RawMessage) {
	var p beforeToolCallPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return
	}

	// Track the tool call against the agent that made it.
	if p.AgentID != "" && p.AgentID != "main" {
		upsertAgent(p.AgentID, "", "", "→ "+p.ToolName+" "+truncate(string(p.Input), 50))
	}

	switch p.ToolName {
	case "create_agent":
		handleCreateAgent(p)
	case "shutdown_agent":
		handleShutdownAgent(p)
	case "list_agents":
		handleListAgents(p)
	case "create_team":
		handleCreateTeam(p)
	case "add_to_team":
		handleAddToTeam(p)
	case "get_team":
		handleGetTeam(p)
	case "shutdown_team":
		handleShutdownTeam(p)
	case "send_message":
		handleSendMessage(p)
	}
}

func handleCreateAgent(p beforeToolCallPayload) {
	var input struct {
		Name         string `json:"name"`
		SystemPrompt string `json:"system_prompt"`
		Prompt       string `json:"prompt"`
		Model        string `json:"model"`
	}
	if err := json.Unmarshal(p.Input, &input); err != nil || input.Name == "" {
		sendToolResult(p.ToolCallID, "create_agent: name and system_prompt are required", true)
		return
	}

	// Generate a deterministic ID from the name (host may override).
	agentID := "agent-" + input.Name

	type spawnParams struct {
		ID           string `json:"id"`
		Name         string `json:"name"`
		SystemPrompt string `json:"system_prompt"`
		ModelName    string `json:"model_name"`
	}
	result := hostCallWithResponse("agent_spawn", spawnParams{
		ID:           agentID,
		Name:         input.Name,
		SystemPrompt: input.SystemPrompt,
		ModelName:    input.Model,
	})

	var resp struct {
		Error string `json:"error,omitempty"`
	}
	if result != "" {
		_ = json.Unmarshal([]byte(result), &resp)
	}
	if resp.Error != "" {
		sendToolResult(p.ToolCallID, "create_agent: "+resp.Error, true)
		return
	}

	// If an initial prompt was provided, send it.
	if input.Prompt != "" {
		type msgParams struct {
			ID      string `json:"id"`
			Message string `json:"message"`
		}
		hostCallWithResponse("agent_send_message", msgParams{ID: agentID, Message: input.Prompt})
	}

	upsertAgent(agentID, input.Name, truncate(input.Prompt, 80), "")
	out, _ := json.Marshal(map[string]string{"agent_id": agentID, "status": "created"})
	sendToolResult(p.ToolCallID, string(out), false)
}

func handleShutdownAgent(p beforeToolCallPayload) {
	var input struct {
		AgentID string `json:"agent_id"`
	}
	if err := json.Unmarshal(p.Input, &input); err != nil || input.AgentID == "" {
		sendToolResult(p.ToolCallID, "shutdown_agent: agent_id is required", true)
		return
	}

	type closeParams struct {
		ID string `json:"id"`
	}
	result := hostCallWithResponse("agent_close", closeParams{ID: input.AgentID})

	var resp struct {
		Error string `json:"error,omitempty"`
	}
	if result != "" {
		_ = json.Unmarshal([]byte(result), &resp)
	}
	if resp.Error != "" {
		sendToolResult(p.ToolCallID, "shutdown_agent: "+resp.Error, true)
		return
	}
	removeAgent(input.AgentID)
	sendToolResult(p.ToolCallID, `{"status":"closed"}`, false)
}

func handleListAgents(p beforeToolCallPayload) {
	result := hostCallWithResponse("agent_list", map[string]string{})
	if result == "" {
		sendToolResult(p.ToolCallID, `{"agents":[]}`, false)
		return
	}
	sendToolResult(p.ToolCallID, result, false)
}

func handleCreateTeam(p beforeToolCallPayload) {
	var input struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(p.Input, &input); err != nil || input.Name == "" {
		sendToolResult(p.ToolCallID, "create_team: name is required", true)
		return
	}

	teamID := "team-" + input.Name

	type createParams struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	result := hostCallWithResponse("team_create", createParams{ID: teamID, Name: input.Name})

	var resp struct {
		Error string `json:"error,omitempty"`
	}
	if result != "" {
		_ = json.Unmarshal([]byte(result), &resp)
	}
	if resp.Error != "" {
		sendToolResult(p.ToolCallID, "create_team: "+resp.Error, true)
		return
	}

	out, _ := json.Marshal(map[string]string{"team_id": teamID, "status": "created"})
	sendToolResult(p.ToolCallID, string(out), false)
}

func handleAddToTeam(p beforeToolCallPayload) {
	var input struct {
		TeamID  string `json:"team_id"`
		AgentID string `json:"agent_id"`
	}
	if err := json.Unmarshal(p.Input, &input); err != nil || input.TeamID == "" || input.AgentID == "" {
		sendToolResult(p.ToolCallID, "add_to_team: team_id and agent_id are required", true)
		return
	}

	type addParams struct {
		TeamID  string `json:"team_id"`
		AgentID string `json:"agent_id"`
	}
	result := hostCallWithResponse("team_add_member", addParams{TeamID: input.TeamID, AgentID: input.AgentID})

	var resp struct {
		Error string `json:"error,omitempty"`
	}
	if result != "" {
		_ = json.Unmarshal([]byte(result), &resp)
	}
	if resp.Error != "" {
		sendToolResult(p.ToolCallID, "add_to_team: "+resp.Error, true)
		return
	}
	sendToolResult(p.ToolCallID, `{"status":"added"}`, false)
}

func handleGetTeam(p beforeToolCallPayload) {
	var input struct {
		TeamID string `json:"team_id"`
	}
	if err := json.Unmarshal(p.Input, &input); err != nil || input.TeamID == "" {
		sendToolResult(p.ToolCallID, "get_team: team_id is required", true)
		return
	}
	// Return available info. The host doesn't have a dedicated get_team method yet;
	// we return what we know from the agent list.
	result := hostCallWithResponse("agent_list", map[string]string{})
	if result == "" {
		result = `{"agents":[]}`
	}
	out, _ := json.Marshal(map[string]string{"team_id": input.TeamID, "members": result})
	sendToolResult(p.ToolCallID, string(out), false)
}

func handleShutdownTeam(p beforeToolCallPayload) {
	var input struct {
		TeamID string `json:"team_id"`
	}
	if err := json.Unmarshal(p.Input, &input); err != nil || input.TeamID == "" {
		sendToolResult(p.ToolCallID, "shutdown_team: team_id is required", true)
		return
	}

	type closeParams struct {
		ID string `json:"id"`
	}
	result := hostCallWithResponse("team_close", closeParams{ID: input.TeamID})

	var resp struct {
		Error string `json:"error,omitempty"`
	}
	if result != "" {
		_ = json.Unmarshal([]byte(result), &resp)
	}
	if resp.Error != "" {
		sendToolResult(p.ToolCallID, "shutdown_team: "+resp.Error, true)
		return
	}
	sendToolResult(p.ToolCallID, `{"status":"closed"}`, false)
}

func handleSendMessage(p beforeToolCallPayload) {
	var input struct {
		AgentID string `json:"agent_id"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(p.Input, &input); err != nil || input.AgentID == "" {
		sendToolResult(p.ToolCallID, "send_message: agent_id and message are required", true)
		return
	}

	type msgParams struct {
		ID      string `json:"id"`
		Message string `json:"message"`
	}
	result := hostCallWithResponse("agent_send_message", msgParams{ID: input.AgentID, Message: input.Message})

	var resp struct {
		Error string `json:"error,omitempty"`
	}
	if result != "" {
		_ = json.Unmarshal([]byte(result), &resp)
	}
	if resp.Error != "" {
		sendToolResult(p.ToolCallID, "send_message: "+resp.Error, true)
		return
	}
	upsertAgent(input.AgentID, "", "", "← "+truncate(input.Message, 60))
	sendToolResult(p.ToolCallID, `{"status":"sent"}`, false)
}

func onCommand(raw json.RawMessage) {
	var payload struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil || payload.Name != "agents" {
		return
	}

	if len(agentRecords) == 0 {
		hostCallJSON("modal", map[string]string{"text": "No sub-agents running."})
		return
	}

	text := "Sub-agents\n" + strings.Repeat("─", 40) + "\n\n"
	for _, r := range agentRecords {
		text += r.id
		if r.name != "" && r.name != r.id {
			text += "  (" + r.name + ")"
		}
		text += "\n"
		if r.task != "" {
			text += "  Task: " + r.task + "\n"
		}
		if r.lastUpdate != "" {
			text += "  Last: " + r.lastUpdate + "\n"
		}
		text += "\n"
	}
	hostCallJSON("modal", map[string]string{"text": strings.TrimRight(text, "\n")})
}

func registerTool(name, desc, inputSchema string) int32 {
	type toolParams struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		InputSchema json.RawMessage `json:"input_schema"`
	}
	rc := hostCallJSON(
		"register_tool",
		toolParams{Name: name, Description: desc, InputSchema: json.RawMessage(inputSchema)},
	)
	if rc != 0 {
		return rc
	}
	return hostCallJSON("subscribe", map[string]string{"event": "before_tool_call"})
}

func sendToolResult(toolCallID, result string, isError bool) {
	hostCallJSON("tool_result", map[string]any{"tool_call_id": toolCallID, "result": result, "is_error": isError})
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

func main() {}
