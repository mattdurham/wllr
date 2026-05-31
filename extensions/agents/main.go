//go:build wasip1

// Package main is the agents built-in extension for the bob coding harness.
// It registers tools for spawning, managing, and messaging agents and teams,
// delegating all operations to the host via host_call.
package main

import (
	"encoding/json"
	"fmt"
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

// initial prompt (truncated)
// most recent action

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
	// safe: WASM is single-threaded; in-place filter avoids allocation
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
		`Create a new agent. The agent ID is {your_agent_id}/{name} (e.g. main creating "researcher" → id="main/researcher"). The returned agent_id is what you pass to send_message and shutdown_agent.`,
		json.RawMessage(`{"type":"object","properties":{"name":{"type":"string","description":"Agent name"},"system_prompt":{"type":"string","description":"System prompt for the agent"},"prompt":{"type":"string","description":"Initial prompt to send"},"model":{"type":"string","description":"Model name (optional)"},"thinking_budget":{"type":"integer","description":"Extended thinking token budget (optional, Anthropic only). Enables deeper reasoning before responding."}},"required":["name","system_prompt","prompt"]}`),
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
	RegisterCommandInstant("agents", "Show running sub-agents and their status")

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
- prompt: the first task. Sub-agents are detected as done automatically when they
  go idle (finish all turns and have no pending work).

  Sub-agents may call send_message("main", result) to pass structured results
  back to the orchestrator for richer output.
- model: optional; defaults to the current session model

Sub-agents should restate their task when reporting back for clarity:
  GOOD: "I was researching X. I found that Y and Z."
  BAD:  "Here are the results."

**send_message(agent_id, message)**
Send a message to an agent and trigger its next turn immediately.
- Use for multi-turn conversations: send follow-up questions or additional tasks.
- Sub-agents are detected as complete when they go idle (not running, no pending
  messages). No send_message is required for the orchestrator to be notified.
- Sub-agents may still call send_message for richer summaries — the message content
  will appear in the caller's conversation but is not needed for wakeup.

**shutdown_agent(agent_id)**
Stop an agent and free its resources. Always shut down agents when their
task is complete. Leaked agents continue consuming memory.

**list_agents()**
Returns all running agent IDs and names.

**get_agent_status(agent_id, history_limit?)**
Diagnostic tool — returns is_running (true if mid-turn), turn_count, and
recent conversation history. Use ONCE to diagnose a stuck agent; do not poll.
- is_running=true: agent is currently working; do not interrupt, end your turn
- is_running=false: agent is idle; read "recent" field to see what it did last
- Always use history_limit=20 to see enough context to understand what happened
history_limit defaults to 10 messages.

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

### Long-running back-and-forth conversations

Agents maintain their full conversation history across turns — you can
exchange many messages with a sub-agent without losing context.

Messages you receive from sub-agents are labeled with one of:
  [from agent 'name' (task: first 80 chars of prompt…)]: — when task is known
  [from agent 'name']:                                    — fallback, no task stored
This lets you track which agent said what across a multi-turn conversation.

The back-and-forth pattern:
  create_agent("researcher", "You research topics. Always restate what you
    were asked to research when reporting findings.", "Research X")

  → researcher does work, calls send_message("main", "I was researching X.
    Found: Y and Z. Shall I go deeper on Y?")

  → Your next turn sees: [from agent 'researcher']: I was researching X...
  → You reply: send_message("researcher", "Yes, go deeper on Y")

  → researcher continues with full context of prior conversation
  → Exchange continues as long as needed
  → shutdown_agent("researcher") when done

Both agents accumulate history across every exchange — neither forgets.

---

### Parallel work pattern

Spawn agents and end your turn. The host wakes you via TASK_DONE or IDLE
notifications when agents finish — you do not need to poll or sleep.

  create_agent("researcher", "...", "Research X.")
  create_agent("coder", "...", "Implement Y.")
  ← end your turn here; you will be woken when agents complete

When woken, check what finished with list_agents() or get_agent_status(),
process the results, then shutdown_agent for each completed agent.

If an agent seems stuck (you have been woken multiple times but it has not
finished):
  get_agent_status("main/coder", 20)  ← diagnose ONCE with high history_limit
  If is_running=true: still working — end your turn again.
  If is_running=false with no useful output: nudge it.
    → send_message("main/coder", "Please report your current status.")

---

### NEVER do this — these do not correctly wait for sub-agents

WRONG — do NOT poll or sleep while waiting for a sub-agent:
  create_agent("researcher", ...)
  exec sleep 10           ← WRONG: wastes a tool call, doesn't actually wait
  get_agent_status(...)   ← WRONG: same turn as above, agent state unchanged
  get_agent_status(...)   ← WRONG: still in the same turn, still useless

WRONG — do NOT call get_agent_status multiple times in one turn:
  get_agent_status("main/coder-1")  ← first call
  get_agent_status("main/coder-1")  ← WRONG: same turn, no time has passed
  get_agent_status("main/coder-1")  ← WRONG: still the same snapshot

RIGHT — spawn agents and end your turn; the host notifies you when done:
  create_agent("researcher", ...)
  ← end your turn; you will be woken by TASK_DONE or IDLE notification
  (on next turn: read results, then shutdown_agent)

RIGHT — if agent seems genuinely stuck:
  get_agent_status("main/coder-1", 20)  ← check ONCE with high history_limit
  (read "recent" history to understand the situation)
  send_message("main/coder-1", "Please report your status.")  ← nudge it

---

### Task-based coordination pattern

Use this pattern when you want to distribute discrete tasks across workers without
blocking on wait_for_all.

**Orchestrator turn:**
1. Call tasklist_create to create a task list (capture list_id and your agent_id).
2. Call tasks_create for each task.
3. Call create_agent for each worker, passing list_id and your agent_id in the system prompt.
4. End your turn immediately — do NOT call wait_for_all.
5. When a worker sends you a message (IDLE or TASK_DONE), wake up, call tasks_list(pending).
   If empty and all tasks are completed, call shutdown_agent for each worker and wrap up.
   If tasks remain, end your turn again and wait.

**Worker turn:**
1. Call tasks_list(list_id, status=pending) to find available tasks.
2. If tasks found: call tasks_update(list_id, task_id, status=in_progress) to claim one.
3. Do the work. Call tasks_update(list_id, task_id, status=completed) when done.
4. Repeat from step 1.
5. If tasks_list returns {"tasks": []} (empty): send send_message("main", "IDLE: no more tasks").
   Then end your turn.

**Note:** Two workers may try to claim the same task. Always re-read the task after claiming
to confirm you own it (tasks_get). If the status is already in_progress by another worker,
skip to the next task.

**Why not wait_for_all?**
wait_for_all blocks the orchestrator's WASM thread during the wait. For long-running tasks
this wastes resources and can cause timeouts. The IDLE signal pattern is fully event-driven.`

	AppendSystemPrompt(guidance)
}

func onAgentsCommand(_ []string) {
	// Query the pool directly — agentRecords is WASM-local and can be stale.
	result := agentCall("agent_list", map[string]string{})
	var poolResp struct {
		Agents []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"agents"`
	}
	if result != "" {
		_ = json.Unmarshal([]byte(result), &poolResp)
	}

	if len(poolResp.Agents) == 0 {
		Modal("No sub-agents running.")
		return
	}

	// Build a lookup of WASM-side metadata (task, last update) by agent ID.
	meta := make(map[string]*agentRecord, len(agentRecords))
	for i := range agentRecords {
		meta[agentRecords[i].id] = &agentRecords[i]
	}

	text := "Sub-agents\n" + strings.Repeat("─", 40) + "\n\n"
	for _, a := range poolResp.Agents {
		text += a.ID
		if a.Name != "" && a.Name != a.ID {
			text += "  (" + a.Name + ")"
		}
		text += "\n"
		if r, ok := meta[a.ID]; ok {
			if r.task != "" {
				text += "  Task: " + r.task + "\n"
			}
			if r.lastUpdate != "" {
				text += "  Last: " + r.lastUpdate + "\n"
			}
		}
		text += "\n"
	}
	Modal(strings.TrimRight(text, "\n"))
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
		Name           string `json:"name"`
		SystemPrompt   string `json:"system_prompt"`
		Prompt         string `json:"prompt"`
		Model          string `json:"model"`
		ThinkingBudget int    `json:"thinking_budget"`
	}
	if err := json.Unmarshal(p.Input, &input); err != nil || input.Name == "" {
		ToolResult(p.ToolCallID, "create_agent: name is required", true)
		return
	}

	// Scope the agent ID to the calling agent to prevent collisions when
	// multiple orchestrators spawn agents with the same name.
	// e.g. orchestrator "main" creating "researcher" → "main/researcher"
	//      sub-agent "planner" creating "researcher" → "planner/researcher"
	scope := p.AgentID
	if scope == "" {
		scope = "main"
	}
	agentID := scope + "/" + input.Name

	// Pass the initial prompt directly to agent_spawn as initial_prompt.
	// The host calls pool.Send after spawning, starting the first turn immediately.
	// Using agent_send_message here would only queue to the inbox with no turn
	// started, leaving the agent permanently idle.
	type spawnParams struct {
		ID             string `json:"id"`
		Name           string `json:"name"`
		SystemPrompt   string `json:"system_prompt"`
		ModelName      string `json:"model_name"`
		InitialPrompt  string `json:"initial_prompt"`
		ThinkingBudget int    `json:"thinking_budget"`
		CallerID       string `json:"caller_id"`
	}
	result := agentCall("agent_spawn", spawnParams{
		ID:             agentID,
		Name:           input.Name,
		SystemPrompt:   input.SystemPrompt,
		ModelName:      input.Model,
		InitialPrompt:  input.Prompt,
		ThinkingBudget: input.ThinkingBudget,
		CallerID:       scope, // the calling agent's ID (p.AgentID or "main")
	})

	var resp struct {
		Error   string `json:"error,omitempty"`
		AgentID string `json:"agent_id,omitempty"`
	}
	if result != "" {
		_ = json.Unmarshal([]byte(result), &resp)
	}
	if resp.Error != "" {
		ToolResult(p.ToolCallID, "create_agent: "+resp.Error, true)
		return
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

	// Build a system shutdown_request message and send it to the target agent's inbox.
	// The agent's finishTurn will detect it, send AGENT_SHUTDOWN back to the creator,
	// and self-close. This avoids forcibly terminating a running agent.
	callerID := p.AgentID
	if callerID == "" {
		callerID = "main"
	}
	payload, _ := json.Marshal(map[string]string{
		"event": "shutdown_request",
		"from":  callerID,
	})
	type sendParams struct {
		ID      string `json:"id"`
		Message string `json:"message"`
		Type    string `json:"type"`
	}
	result := agentCall("agent_send_message", sendParams{
		ID:      input.AgentID,
		Message: string(payload),
		Type:    "system",
	})
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

	// Trigger the agent's next turn so it processes the shutdown_request immediately
	// if it is currently idle. finishTurn handles the message after the turn completes.
	agentCall("agent_run", map[string]string{"id": input.AgentID})

	// Do NOT call removeAgent or agent_close here — the agent will self-close when
	// finishTurn processes the shutdown_request and sends AGENT_SHUTDOWN back.
	ToolResult(p.ToolCallID, `{"status":"shutdown_requested"}`, false)
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
	type teamInfoParams struct {
		TeamID string `json:"team_id"`
	}
	result := agentCall("team_get_info", teamInfoParams{TeamID: input.TeamID})
	if result == "" {
		ToolResult(p.ToolCallID, "get_team: host returned no response", true)
		return
	}
	var resp struct {
		Error string `json:"error,omitempty"`
	}
	if err := json.Unmarshal([]byte(result), &resp); err == nil && resp.Error != "" {
		ToolResult(p.ToolCallID, "get_team: "+resp.Error, true)
		return
	}
	ToolResult(p.ToolCallID, result, false)
}

func handleShutdownTeam(p beforeToolCallPayload) {
	var input struct {
		TeamID string `json:"team_id"`
	}
	if err := json.Unmarshal(p.Input, &input); err != nil || input.TeamID == "" {
		ToolResult(p.ToolCallID, "shutdown_team: team_id is required", true)
		return
	}

	// Get member IDs BEFORE closing so we can clean up agentRecords.
	memberIDs := getTeamMembers(input.TeamID)

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

	// Clean up WASM-side records for all team members.
	for _, id := range memberIDs {
		removeAgent(id)
	}

	ToolResult(p.ToolCallID, `{"status":"closed"}`, false)
}

// getTeamMembers calls team_get_info and returns member IDs, or nil on error.
func getTeamMembers(teamID string) []string {
	result := agentCall("team_get_info", map[string]string{"team_id": teamID})
	if result == "" {
		return nil
	}
	var resp struct {
		Members []string `json:"members"`
		Error   string   `json:"error,omitempty"`
	}
	if err := json.Unmarshal([]byte(result), &resp); err != nil || resp.Error != "" {
		return nil
	}
	return resp.Members
}

func handleSendMessage(p beforeToolCallPayload) {
	var input struct {
		AgentID string `json:"agent_id"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(p.Input, &input); err != nil || input.AgentID == "" || input.Message == "" {
		ToolResult(p.ToolCallID, "send_message: agent_id and message are required and message must be non-empty", true)
		return
	}

	// Label the message with the sender's agent ID so the recipient has
	// thread context — otherwise "here are my findings" arrives with no
	// indication of who sent it or why.
	// Also append the sender's stored task summary to reduce context-loss
	// when the orchestrator wakes up from a send_message.
	labeledMessage := input.Message
	// p.AgentID is "" for main-agent sends; label suppression is intentional
	if p.AgentID != "" && p.AgentID != input.AgentID {
		label := "[from agent '" + p.AgentID + "'"
		for _, r := range agentRecords {
			if r.id == p.AgentID && r.task != "" {
				label += " (task: " + r.task + ")"
				break
			}
		}
		label += "]"
		labeledMessage = label + ": " + input.Message
	}

	type msgParams struct {
		ID      string `json:"id"`
		Message string `json:"message"`
	}
	result := agentCall("agent_send_message", msgParams{ID: input.AgentID, Message: labeledMessage})

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
	// Trigger an immediate turn so the agent processes the queued message now.
	runResult := agentCall("agent_run", map[string]string{"id": input.AgentID})
	var runResp struct {
		Error string `json:"error,omitempty"`
	}
	if runResult != "" {
		_ = json.Unmarshal([]byte(runResult), &runResp)
	}
	if runResp.Error != "" {
		ToolResult(p.ToolCallID, fmt.Sprintf(`{"status":"error","error":"agent_run failed: %s"}`, runResp.Error), true)
		return
	}
	upsertAgent(input.AgentID, "", "", "← "+truncate(input.Message, 60))
	ToolResult(p.ToolCallID, `{"status":"sent"}`, false)
}

func main() {}
