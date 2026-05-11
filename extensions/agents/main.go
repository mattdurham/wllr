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

// ─── Local host_call for agent/team methods not wrapped by the SDK ────────────

//go:wasmimport env host_call
func _agentsHostCall(reqPtr, reqLen, respPtrPtr, respLenPtr uint32) uint32

// agentCall fires a host_call and returns the raw response bytes, or "".
func agentCall(method string, params any) string {
	type request struct {
		Method string `json:"method"`
		Params any    `json:"params,omitempty"`
	}
	reqBytes, err := json.Marshal(request{Method: method, Params: params})
	if err != nil {
		return ""
	}
	buf := make([]byte, len(reqBytes))
	copy(buf, reqBytes)
	ptr := uintptr(unsafe.Pointer(&buf[0]))

	var respPtr, respLen uint32
	_agentsHostCall(
		uint32(ptr), uint32(len(buf)),
		uint32(uintptr(unsafe.Pointer(&respPtr))),
		uint32(uintptr(unsafe.Pointer(&respLen))),
	)
	if respPtr == 0 || respLen == 0 {
		return ""
	}
	resp := make([]byte, respLen)
	mem := (*[1 << 28]byte)(unsafe.Pointer(uintptr(respPtr)))
	copy(resp, mem[:respLen])
	return string(resp)
}

// ─── Agent registry ───────────────────────────────────────────────────────────

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

// ─── Init ─────────────────────────────────────────────────────────────────────

func init() {
	RegisterTool(
		"create_agent",
		"Create a new agent with a name, system prompt, and optional model",
		json.RawMessage(`{"type":"object","properties":{"name":{"type":"string","description":"Agent name"},"system_prompt":{"type":"string","description":"System prompt for the agent"},"prompt":{"type":"string","description":"Initial prompt to send"},"model":{"type":"string","description":"Model name (optional)"}},"required":["name","system_prompt","prompt"]}`),
	)
	RegisterTool(
		"shutdown_agent",
		"Shut down and remove a running agent",
		json.RawMessage(`{"type":"object","properties":{"agent_id":{"type":"string","description":"ID of the agent to shut down"}},"required":["agent_id"]}`),
	)
	RegisterTool(
		"list_agents",
		"List all running agents",
		json.RawMessage(`{"type":"object","properties":{}}`),
	)
	RegisterTool(
		"create_team",
		"Create a new team",
		json.RawMessage(`{"type":"object","properties":{"name":{"type":"string","description":"Team name"}},"required":["name"]}`),
	)
	RegisterTool(
		"add_to_team",
		"Add an agent to a team",
		json.RawMessage(`{"type":"object","properties":{"team_id":{"type":"string","description":"Team ID"},"agent_id":{"type":"string","description":"Agent ID"}},"required":["team_id","agent_id"]}`),
	)
	RegisterTool(
		"get_team",
		"Get information about a team",
		json.RawMessage(`{"type":"object","properties":{"team_id":{"type":"string","description":"Team ID"}},"required":["team_id"]}`),
	)
	RegisterTool(
		"shutdown_team",
		"Shut down a team and all its members",
		json.RawMessage(`{"type":"object","properties":{"team_id":{"type":"string","description":"Team ID"}},"required":["team_id"]}`),
	)
	RegisterTool(
		"send_message",
		"Send a message to an agent",
		json.RawMessage(`{"type":"object","properties":{"agent_id":{"type":"string","description":"Agent ID"},"message":{"type":"string","description":"Message text"}},"required":["agent_id","message"]}`),
	)

	RegisterCommand("agents", "Show running sub-agents and their status")

	OnSessionStart(onSessionStart)
	OnCommand("agents", onAgentsCommand)

	// Use the raw before_tool_call event so we get the AgentID field too.
	OnBeforeToolCall(onBeforeToolCall)
}

// ─── Event handlers ───────────────────────────────────────────────────────────

func onSessionStart() {
	guidance := `
## Agent and Team Tools

### Tool reference

**create_agent(name, system_prompt, prompt, model?)**
Spawn a sub-agent and send its first task. The agent starts immediately.
Its output does NOT appear in your chat — it works silently in the background.
- name: short label shown in /agents status (e.g. "researcher", "coder-1")
- system_prompt: the agent's role, constraints, and output format — be explicit
- prompt: the first task. Tell the agent to call send_message back to you with results.
- model: optional; defaults to the current session model

**send_message(agent_id, message)**
Queue a message into an agent's inbox. Delivered at the start of that
agent's next turn — does NOT start an immediate turn.
- To ask a sub-agent for results: send_message(id, "Report your findings")
  then call create_agent or any tool to trigger that agent's next turn.
- Sub-agents reporting back to you: they call send_message(your_id, result),
  which queues into your inbox and appears in your context next turn.

**shutdown_agent(agent_id)**
Stop an agent and free its resources. Always shut down agents when their
task is complete. Leaked agents continue consuming memory.

**list_agents()**
Returns all currently running agents with their IDs and names.

**create_team(name)** / **add_to_team(team_id, agent_id)** / **shutdown_team(team_id)**
Group agents for coordinated work. shutdown_team stops all members at once.

---

### When to use sub-agents

Use a sub-agent when:
- A task can run in parallel with other work (research while you code)
- A task needs a different persona or specialised focus (strict reviewer,
  cautious planner, aggressive refactorer)
- A task is long and you want it isolated from the main conversation

Do it yourself when the task is a single tool call or a quick sequence.

---

### Writing good system prompts

A sub-agent's system_prompt defines its entire world. Be explicit:

**Good:**
"You are a strict code reviewer. Read the files you are given and return
a bullet list of issues grouped by severity (critical / high / low).
Do not suggest fixes — only identify problems. Be terse."

**Bad:**
"You are a helpful assistant."

Include: role, output format, constraints, what NOT to do.

---

### Receiving results

Sub-agent output does NOT stream into your chat. Agents work silently.
Results reach you in two ways:

1. **Agent reports back**: tell the agent in its system_prompt or initial
   prompt to call send_message back to you when done. The message queues
   in your inbox and appears in your context on your next turn.

2. **You ask for a summary**: send_message(id, "Summarise your findings"),
   then trigger the agent's next turn by calling create_agent or any other
   tool. The agent processes your queued message and calls send_message
   back with results.

Pattern to collect results and shut down:
  send_message(id, "Give me your 3 key findings, then stop.")
  shutdown_agent(id)  ← after you see the findings in context

---

### Parallel work pattern

To run two tasks simultaneously:
  create_agent("researcher", "...", "Research X, then send_message me the findings")  → id-1
  create_agent("coder", "...", "Implement Y, then send_message me the result")        → id-2
  (both run; they send_message their results back to your inbox when done)
  shutdown_agent(id-1)
  shutdown_agent(id-2)
  (on your next turn, inbox has both results in context)`

	AppendSystemPrompt(guidance)
}

func onAgentsCommand(_ []string) {
	if len(agentRecords) == 0 {
		Modal("No sub-agents running.")
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
	Modal(strings.TrimRight(text, "\n"))
}

type beforeToolCallPayload struct {
	AgentID    string          `json:"agent_id"`
	ToolCallID string          `json:"tool_call_id"`
	ToolName   string          `json:"tool_name"`
	Input      json.RawMessage `json:"input"`
}

func onBeforeToolCall(payload json.RawMessage) {
	var p beforeToolCallPayload
	if err := json.Unmarshal(payload, &p); err != nil {
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

// ─── Tool handlers ────────────────────────────────────────────────────────────

func handleCreateAgent(p beforeToolCallPayload) {
	var input struct {
		Name         string `json:"name"`
		SystemPrompt string `json:"system_prompt"`
		Prompt       string `json:"prompt"`
		Model        string `json:"model"`
	}
	if err := json.Unmarshal(p.Input, &input); err != nil || input.Name == "" {
		ToolResult(p.ToolCallID, "create_agent: name and system_prompt are required", true)
		return
	}

	agentID := "agent-" + input.Name

	type spawnParams struct {
		ID           string `json:"id"`
		Name         string `json:"name"`
		SystemPrompt string `json:"system_prompt"`
		ModelName    string `json:"model_name"`
	}
	result := agentCall("agent_spawn", spawnParams{
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
		ToolResult(p.ToolCallID, "create_agent: "+resp.Error, true)
		return
	}

	if input.Prompt != "" {
		type msgParams struct {
			ID      string `json:"id"`
			Message string `json:"message"`
		}
		agentCall("agent_send_message", msgParams{ID: agentID, Message: input.Prompt})
	}

	upsertAgent(agentID, input.Name, truncate(input.Prompt, 80), "")
	out, _ := json.Marshal(map[string]string{"agent_id": agentID, "status": "created"})
	ToolResult(p.ToolCallID, string(out), false)
}

func handleShutdownAgent(p beforeToolCallPayload) {
	var input struct {
		AgentID string `json:"agent_id"`
	}
	if err := json.Unmarshal(p.Input, &input); err != nil || input.AgentID == "" {
		ToolResult(p.ToolCallID, "shutdown_agent: agent_id is required", true)
		return
	}

	type closeParams struct {
		ID string `json:"id"`
	}
	result := agentCall("agent_close", closeParams{ID: input.AgentID})

	var resp struct {
		Error string `json:"error,omitempty"`
	}
	if result != "" {
		_ = json.Unmarshal([]byte(result), &resp)
	}
	if resp.Error != "" {
		ToolResult(p.ToolCallID, "shutdown_agent: "+resp.Error, true)
		return
	}
	removeAgent(input.AgentID)
	ToolResult(p.ToolCallID, `{"status":"closed"}`, false)
}

func handleListAgents(p beforeToolCallPayload) {
	result := agentCall("agent_list", map[string]string{})
	if result == "" {
		ToolResult(p.ToolCallID, `{"agents":[]}`, false)
		return
	}
	ToolResult(p.ToolCallID, result, false)
}

func handleCreateTeam(p beforeToolCallPayload) {
	var input struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(p.Input, &input); err != nil || input.Name == "" {
		ToolResult(p.ToolCallID, "create_team: name is required", true)
		return
	}

	teamID := "team-" + input.Name

	type createParams struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	result := agentCall("team_create", createParams{ID: teamID, Name: input.Name})

	var resp struct {
		Error string `json:"error,omitempty"`
	}
	if result != "" {
		_ = json.Unmarshal([]byte(result), &resp)
	}
	if resp.Error != "" {
		ToolResult(p.ToolCallID, "create_team: "+resp.Error, true)
		return
	}

	out, _ := json.Marshal(map[string]string{"team_id": teamID, "status": "created"})
	ToolResult(p.ToolCallID, string(out), false)
}

func handleAddToTeam(p beforeToolCallPayload) {
	var input struct {
		TeamID  string `json:"team_id"`
		AgentID string `json:"agent_id"`
	}
	if err := json.Unmarshal(p.Input, &input); err != nil || input.TeamID == "" || input.AgentID == "" {
		ToolResult(p.ToolCallID, "add_to_team: team_id and agent_id are required", true)
		return
	}

	type addParams struct {
		TeamID  string `json:"team_id"`
		AgentID string `json:"agent_id"`
	}
	result := agentCall("team_add_member", addParams{TeamID: input.TeamID, AgentID: input.AgentID})

	var resp struct {
		Error string `json:"error,omitempty"`
	}
	if result != "" {
		_ = json.Unmarshal([]byte(result), &resp)
	}
	if resp.Error != "" {
		ToolResult(p.ToolCallID, "add_to_team: "+resp.Error, true)
		return
	}
	ToolResult(p.ToolCallID, `{"status":"added"}`, false)
}

func handleGetTeam(p beforeToolCallPayload) {
	var input struct {
		TeamID string `json:"team_id"`
	}
	if err := json.Unmarshal(p.Input, &input); err != nil || input.TeamID == "" {
		ToolResult(p.ToolCallID, "get_team: team_id is required", true)
		return
	}
	// Return available info. The host doesn't have a dedicated get_team method yet;
	// we return what we know from the agent list.
	result := agentCall("agent_list", map[string]string{})
	if result == "" {
		result = `{"agents":[]}`
	}
	out, _ := json.Marshal(map[string]string{"team_id": input.TeamID, "members": result})
	ToolResult(p.ToolCallID, string(out), false)
}

func handleShutdownTeam(p beforeToolCallPayload) {
	var input struct {
		TeamID string `json:"team_id"`
	}
	if err := json.Unmarshal(p.Input, &input); err != nil || input.TeamID == "" {
		ToolResult(p.ToolCallID, "shutdown_team: team_id is required", true)
		return
	}

	type closeParams struct {
		ID string `json:"id"`
	}
	result := agentCall("team_close", closeParams{ID: input.TeamID})

	var resp struct {
		Error string `json:"error,omitempty"`
	}
	if result != "" {
		_ = json.Unmarshal([]byte(result), &resp)
	}
	if resp.Error != "" {
		ToolResult(p.ToolCallID, "shutdown_team: "+resp.Error, true)
		return
	}
	ToolResult(p.ToolCallID, `{"status":"closed"}`, false)
}

func handleSendMessage(p beforeToolCallPayload) {
	var input struct {
		AgentID string `json:"agent_id"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(p.Input, &input); err != nil || input.AgentID == "" {
		ToolResult(p.ToolCallID, "send_message: agent_id and message are required", true)
		return
	}

	type msgParams struct {
		ID      string `json:"id"`
		Message string `json:"message"`
	}
	result := agentCall("agent_send_message", msgParams{ID: input.AgentID, Message: input.Message})

	var resp struct {
		Error string `json:"error,omitempty"`
	}
	if result != "" {
		_ = json.Unmarshal([]byte(result), &resp)
	}
	if resp.Error != "" {
		ToolResult(p.ToolCallID, "send_message: "+resp.Error, true)
		return
	}
	upsertAgent(input.AgentID, "", "", "← "+truncate(input.Message, 60))
	ToolResult(p.ToolCallID, `{"status":"sent"}`, false)
}

func main() {}
