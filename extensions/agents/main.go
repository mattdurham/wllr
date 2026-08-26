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

const subAgentCodeIntelligenceGuidance = `## Code Intelligence Tool Use
If this task involves reading, changing, reviewing, or testing source code:
- Use lsp_capabilities if you are unsure what code-intelligence backends exist.
- Use lsp_symbols, lsp_definition, and lsp_references before broad grep/rg/find searches or large read_file sweeps for code structure.
- Use lsp_refactor_preview before renames or shared API refactors.
- Use lsp_diagnostics or lsp_lint after source edits before raw shell test commands, unless the user explicitly asked for the shell command.
- Use exec and manual search as fallbacks when LSP output is unavailable, incomplete, or unrelated to the task.`

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

func formatDurationMS(ms int64) string {
	if ms < 1000 {
		return fmt.Sprintf("%dms", ms)
	}
	sec := ms / 1000
	if sec < 60 {
		return fmt.Sprintf("%ds", sec)
	}
	min := sec / 60
	rem := sec % 60
	if min < 60 {
		return fmt.Sprintf("%dm%02ds", min, rem)
	}
	hour := min / 60
	return fmt.Sprintf("%dh%02dm", hour, min%60)
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
	RegisterToolWithOutput(
		"create_agent",
		`Create a new agent. The agent ID is {your_agent_id}/{name} (e.g. main creating "researcher" → id="main/researcher"). The returned agent_id is what you pass to send_message and shutdown_agent.`,
		json.RawMessage(
			`{"type":"object","properties":{"name":{"type":"string","description":"Agent name"},"system_prompt":{"type":"string","description":"System prompt for the agent"},"prompt":{"type":"string","description":"Initial prompt to send"},"model":{"type":"string","description":"Model name (optional)"},"thinking_budget":{"type":"integer","description":"Extended thinking token budget (optional, Anthropic only). Enables deeper reasoning before responding."}},"required":["name","system_prompt","prompt"]}`,
		),
		json.RawMessage(
			`{"type":"object","description":"Host agent creation result including the new agent_id on success"}`,
		),
	)
	RegisterToolWithOutput(
		"shutdown_agent",
		"Request graceful shutdown for an agent. A running agent may continue until its current turn finishes; confirm with list_agents before taking over its work.",
		json.RawMessage(
			`{"type":"object","properties":{"agent_id":{"type":"string","description":"ID of the agent to shut down"}},"required":["agent_id"]}`,
		),
		json.RawMessage(`{"type":"object","description":"Host shutdown result"}`),
	)
	RegisterToolWithOutput(
		"list_agents",
		"List live agents with running state, working/liveness status, queued messages, recent activity age, active/last tool, and shutdown request state. working=true means the child is working unless liveness=dead.",
		json.RawMessage(`{"type":"object","properties":{}}`),
		json.RawMessage(
			`{"type":"object","properties":{"agents":{"type":"array","items":{"type":"object","properties":{"id":{"type":"string"},"name":{"type":"string"},"is_running":{"type":"boolean"},"working":{"type":"boolean"},"liveness":{"type":"string","enum":["idle","working","stopping","dead"]},"pending_messages":{"type":"integer"},"last_activity_age_ms":{"type":"integer"},"turn_duration_ms":{"type":"integer"},"last_tool_age_ms":{"type":"integer"},"last_tool_done_age_ms":{"type":"integer"},"active_tool":{"type":"string"},"last_tool":{"type":"string"},"shutdown_requested":{"type":"boolean"}}}}}}`,
		),
	)
	RegisterToolWithOutput(
		"create_team",
		"Create a new team",
		json.RawMessage(
			`{"type":"object","properties":{"name":{"type":"string","description":"Team name"}},"required":["name"]}`,
		),
		json.RawMessage(`{"type":"object","description":"Host team creation result including team_id on success"}`),
	)
	RegisterToolWithOutput(
		"add_to_team",
		"Add an agent to a team",
		json.RawMessage(
			`{"type":"object","properties":{"team_id":{"type":"string","description":"Team ID"},"agent_id":{"type":"string","description":"Agent ID"}},"required":["team_id","agent_id"]}`,
		),
		json.RawMessage(`{"type":"object","description":"Host team membership update result"}`),
	)
	RegisterToolWithOutput(
		"get_team",
		"Get information about a team",
		json.RawMessage(
			`{"type":"object","properties":{"team_id":{"type":"string","description":"Team ID"}},"required":["team_id"]}`,
		),
		json.RawMessage(`{"type":"object","description":"Team details returned by the host"}`),
	)
	RegisterToolWithOutput(
		"shutdown_team",
		"Shut down a team and all its members",
		json.RawMessage(
			`{"type":"object","properties":{"team_id":{"type":"string","description":"Team ID"}},"required":["team_id"]}`,
		),
		json.RawMessage(`{"type":"object","description":"Host team shutdown result"}`),
	)
	RegisterToolWithOutput(
		"send_message",
		"Send a message to an agent and wake it. If the agent is already running, the message is queued for its next turn; do not use as a ping.",
		json.RawMessage(
			`{"type":"object","properties":{"agent_id":{"type":"string","description":"Agent ID"},"message":{"type":"string","description":"Message text"}},"required":["agent_id","message"]}`,
		),
		json.RawMessage(`{"type":"object","description":"Host message delivery result"}`),
	)
	RegisterCommandInstant("agents", "Show running sub-agents and their status")

	OnSessionStart(onSessionStart)
	OnCommand("agents", onAgentsCommand)

	// Use the raw before_tool_call event so we get the AgentID field too.
	OnBeforeToolCall(onBeforeToolCall)

	// Register the WASM-driven chat transcript handlers (UI P4).
	initChat()
}

// ─── Event handlers ───────────────────────────────────────────────────────────

func onSessionStart() {
	onChatSessionStart()

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

### Parent/child lifecycle notifications

The host sends lifecycle notifications to the recorded creator as JSON messages
and wakes that creator. These are protocol messages, not child work results:

- event: "agent_idle": the child finished its current work and has no pending
  messages. Inspect its status/history, then process its result and shut it down
  when finished.
- event: "agent_failed": the child turn failed. The error field explains
  why; inspect the child and decide whether to retry, recover, or report failure.
- event: "AGENT_SHUTDOWN": the child completed a graceful shutdown and is no
  longer available. Remove it from your active-worker set.

Every lifecycle notification includes agent_id; it may include creator_id,
error, and a human-readable message. Do not wait for a child by polling or
by sending pings. A notification is the wake-up signal; the live status fields
and child messages are the source of truth for what to do next. Always send a
substantive result with send_message before going idle, especially for coding
work.

**shutdown_agent(agent_id)**
Request graceful shutdown. If the agent is running, it may continue its current
turn and tool calls before stopping. Do not take over its files until list_agents
no longer shows it, or until it is idle with shutdown_requested=false.
Always shut down agents when their task is complete. Leaked agents continue
consuming memory.

**list_agents()**
Returns all live agents with IDs, names, is_running, pending_messages, and
liveness fields: working, liveness, last_activity_age_ms, turn_duration_ms,
active_tool, last_tool, last_tool_age_ms, last_tool_done_age_ms, and
shutdown_requested.
Treat working=true as authoritative: the child is doing its current turn unless
liveness is dead. Do not ping, poll, interrupt, or take over a working child.
If list_agents shows working=true, stop and end your turn. Do not call
list_agents again in the same wait cycle.
If shutdown_requested=true, do not call shutdown_agent again unless the user
explicitly asked you to retry.

**get_agent_status(agent_id, history_limit?)**
Diagnostic tool — returns live state for one agent: is_running, pending_messages,
working, liveness, last_activity_age_ms, turn_duration_ms, active_tool,
last_tool, last_tool_done_age_ms, and shutdown_requested. Use ONCE to diagnose
a child only when the user asks you to inspect it; do not poll.
- working=true: agent is currently working; do not interrupt, do not send another
  message, and do not take over. End your turn and wait for its notification.
- liveness=dead: the child is not making progress; inspect once, then nudge or
  request graceful shutdown
- is_running=false: agent is idle; review its final message or task output
- pending_messages>0: messages are queued for the next turn; the child may not
  have read your latest message yet
- shutdown_requested=true: graceful shutdown has been requested but the agent may
  still be finishing its current turn; do not assume it has stopped
history_limit is accepted for compatibility, but live state is the reliable
liveness signal.

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

For coding agents, include code-intelligence tool rules directly in the
system_prompt. Tell the agent:
- Use lsp_capabilities if it is unsure what code-intelligence backends exist.
- Use lsp_symbols, lsp_definition, and lsp_references before broad
  grep/rg/find searches or large read_file sweeps for code structure.
- Use lsp_refactor_preview before renames or shared API refactors.
- Use lsp_diagnostics or lsp_lint after source edits before raw shell test
  commands, unless the user explicitly asked for the shell command.
- Use exec and manual search as fallbacks when LSP output is unavailable,
  incomplete, or unrelated to the task.

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

When woken by a child notification, check what finished with list_agents() or
get_agent_status(), process the results, then shutdown_agent for each completed
agent.

Only send a message to a child when one of these is true:
- The user explicitly asked you to talk to that child.
- The child is idle and needs a follow-up turn.
- The child reports liveness=dead and you are nudging or shutting it down.

Do not send progress probes to working children. A message sent while the child
is working only queues behind its current turn and does not make it finish sooner.

If an agent seems stuck (you have been woken multiple times but it has not
finished):
  get_agent_status("main/coder", 20)  ← diagnose ONCE
  If working=true: still working — end your turn again.
  If liveness=dead: nudge once or request graceful shutdown.
  If is_running=false with no useful output: nudge it.
    → send_message("main/coder", "Please report your current status.")
  Do not loop on list_agents while waiting. One status check, then wait for a
  notification or a real state change.

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
  get_agent_status("main/coder-1", 20)  ← check ONCE
  (read liveness fields to understand the situation)
  if liveness=dead: send_message("main/coder-1", "Please report your status.")  ← nudge it

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
1. Call tasks_claim(list_id, agent_id=your_id) to atomically claim the next available task.
2. If it returns a task: do the work, then call tasks_update(list_id, task_id, status=completed).
3. Repeat from step 1.
4. If tasks_claim returns {"task": null} (nothing available): send send_message("main", "IDLE: no more tasks").
   Then end your turn.

**Why tasks_claim and not tasks_list + tasks_update?**
tasks_claim finds the next pending, dependency-satisfied task AND marks it in_progress in a
single atomic step, recording you as the assignee. Two workers can never claim the same task.
The old list-then-update pattern had a race window where both workers saw the same pending
task and both "claimed" it. Use tasks_claim — no re-read-to-confirm dance is needed.

**Why not wait_for_all?**
wait_for_all blocks the orchestrator's WASM thread during the wait. For long-running tasks
this wastes resources and can cause timeouts. The IDLE signal pattern is fully event-driven.`

	AppendSystemPrompt(guidance)
	AppendSystemPrompt(`## Durable task coordination

Tasks are host-owned and survive extension restart and compaction. Parents must
create tasks with owner/parent IDs and a workspace_mode of shared, worktree, or
readonly (metadata placeholders only), then pass list, task, and attempt IDs to
workers. Workers claim once and report completed, blocked, failed, or cancelled
through tasks_report; completed reports require a structured
result and other terminal reports require a reason or error. Include and retain
the returned version; updates use expected-version CAS and claims produce a
new attempt_id.

After every wake or compaction, reconcile with tasks_events_after from the
last cursor (cursor zero is valid), deduplicating by event_id. A notification
is only a wake hint; the ledger is authoritative. Use send_message for prose,
progress, and questions. Never infer completion from TASK_DONE text or idle
state, and inspect liveness before retrying or requeueing. Workers report before
going idle. Never poll, sleep, or call wait_for_all.
`)
}

func onAgentsCommand(_ []string) {
	// Query the pool directly — agentRecords is WASM-local and can be stale.
	result := agentCall("agent_list", map[string]string{})
	var poolResp struct {
		Agents []struct {
			ID                string `json:"id"`
			Name              string `json:"name"`
			IsRunning         bool   `json:"is_running"`
			PendingMessages   int    `json:"pending_messages"`
			LastActivityAgeMS int64  `json:"last_activity_age_ms"`
			TurnDurationMS    int64  `json:"turn_duration_ms"`
			LastToolDoneAgeMS int64  `json:"last_tool_done_age_ms"`
			Liveness          string `json:"liveness"`
			ActiveTool        string `json:"active_tool"`
			LastTool          string `json:"last_tool"`
			Working           bool   `json:"working"`
			ShutdownRequested bool   `json:"shutdown_requested"`
		} `json:"agents"`
	}
	if result != "" {
		_ = json.Unmarshal([]byte(result), &poolResp)
	}

	subAgents := poolResp.Agents[:0]
	for _, a := range poolResp.Agents {
		if a.ID != "" && a.ID != "main" {
			subAgents = append(subAgents, a)
		}
	}

	if len(subAgents) == 0 {
		Modal("No sub-agents running.")
		return
	}

	// Build a lookup of WASM-side metadata (task, last update) by agent ID.
	meta := make(map[string]*agentRecord, len(agentRecords))
	for i := range agentRecords {
		meta[agentRecords[i].id] = &agentRecords[i]
	}

	var sb strings.Builder
	sb.WriteString("Sub-agents\n")
	sb.WriteString(strings.Repeat("─", 40))
	sb.WriteString("\n\n")
	for _, a := range subAgents {
		sb.WriteString(a.ID)
		if a.Name != "" && a.Name != a.ID {
			sb.WriteString("  (" + a.Name + ")")
		}
		sb.WriteString("\n")
		if a.IsRunning {
			sb.WriteString("  Status: running\n")
			if a.TurnDurationMS > 0 {
				sb.WriteString(fmt.Sprintf("  Turn running: %s\n", formatDurationMS(a.TurnDurationMS)))
			}
		} else {
			sb.WriteString("  Status: idle\n")
		}
		if a.Liveness != "" {
			sb.WriteString("  Liveness: " + a.Liveness + "\n")
		}
		if a.Working {
			sb.WriteString("  Working: true\n")
		}
		if a.LastActivityAgeMS > 0 {
			sb.WriteString(fmt.Sprintf("  Last activity: %s ago\n", formatDurationMS(a.LastActivityAgeMS)))
		}
		if a.ActiveTool != "" {
			sb.WriteString("  Active tool: " + a.ActiveTool + "\n")
		} else if a.LastTool != "" {
			sb.WriteString("  Last tool: " + a.LastTool + "\n")
		}
		if a.LastToolDoneAgeMS > 0 {
			sb.WriteString(fmt.Sprintf("  Last tool done: %s ago\n", formatDurationMS(a.LastToolDoneAgeMS)))
		}
		if a.ShutdownRequested {
			sb.WriteString("  Shutdown: requested\n")
		}
		if a.PendingMessages > 0 {
			sb.WriteString(fmt.Sprintf("  Pending messages: %d\n", a.PendingMessages))
		}
		if r, ok := meta[a.ID]; ok {
			if r.task != "" {
				sb.WriteString("  Task: " + r.task + "\n")
			}
			if r.lastUpdate != "" {
				sb.WriteString("  Last: " + r.lastUpdate + "\n")
			}
		}
		sb.WriteString("\n")
	}
	Modal(strings.TrimRight(sb.String(), "\n"))
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

	systemPrompt := strings.TrimSpace(input.SystemPrompt)
	if systemPrompt != "" {
		systemPrompt += "\n\n"
	}
	systemPrompt += subAgentCodeIntelligenceGuidance

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
		SystemPrompt:   systemPrompt,
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

	if state, ok := lookupAgentInfo(input.AgentID); ok && state.ShutdownRequested {
		out := map[string]any{
			"status":   "already_requested",
			"agent_id": input.AgentID,
			"stopped":  false,
		}
		if state.IsRunning {
			out["is_running"] = state.IsRunning
			out["working"] = state.Working
			out["liveness"] = state.Liveness
			out["pending_messages"] = state.PendingMessages
			out["last_activity_age_ms"] = state.LastActivityAgeMS
			out["shutdown_requested"] = state.ShutdownRequested
		}
		encoded, _ := json.Marshal(out)
		ToolResult(p.ToolCallID, string(encoded), false)
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
	// agent_deliver atomically queues the shutdown_request and wakes the agent so
	// it processes the request immediately if idle. finishTurn handles it after
	// the turn completes (sends AGENT_SHUTDOWN to the creator and self-closes).
	type deliverParams struct {
		ID      string `json:"id"`
		Message string `json:"message"`
		Type    string `json:"type"`
	}
	result := agentCall("agent_deliver", deliverParams{
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

	// Do NOT call removeAgent or agent_close here — the agent will self-close when
	// finishTurn processes the shutdown_request and sends AGENT_SHUTDOWN back.
	out := map[string]any{
		"status":   "shutdown_requested",
		"agent_id": input.AgentID,
		"stopped":  false,
	}
	if state, ok := lookupAgentInfo(input.AgentID); ok {
		out["is_running"] = state.IsRunning
		out["working"] = state.Working
		out["liveness"] = state.Liveness
		out["pending_messages"] = state.PendingMessages
		out["last_activity_age_ms"] = state.LastActivityAgeMS
		out["shutdown_requested"] = state.ShutdownRequested
	}
	encoded, _ := json.Marshal(out)
	ToolResult(p.ToolCallID, string(encoded), false)
}

func handleListAgents(p beforeToolCallPayload) {
	result := agentCall("agent_list", map[string]string{})
	if result == "" {
		ToolResult(p.ToolCallID, `{"agents":[]}`, false)
		return
	}
	ToolResult(p.ToolCallID, result, false)
}

type agentInfoView struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	IsRunning         bool   `json:"is_running"`
	PendingMessages   int    `json:"pending_messages"`
	LastActivityAgeMS int64  `json:"last_activity_age_ms"`
	TurnDurationMS    int64  `json:"turn_duration_ms"`
	LastToolDoneAgeMS int64  `json:"last_tool_done_age_ms"`
	Liveness          string `json:"liveness"`
	ActiveTool        string `json:"active_tool"`
	LastTool          string `json:"last_tool"`
	Working           bool   `json:"working"`
	ShutdownRequested bool   `json:"shutdown_requested"`
}

func lookupAgentInfo(id string) (agentInfoView, bool) {
	result := agentCall("agent_list", map[string]string{})
	var poolResp struct {
		Agents []agentInfoView `json:"agents"`
	}
	if result == "" || json.Unmarshal([]byte(result), &poolResp) != nil {
		return agentInfoView{}, false
	}
	for _, a := range poolResp.Agents {
		if a.ID == id {
			return a, true
		}
	}
	return agentInfoView{}, false
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

	// agent_deliver atomically queues the message AND wakes the recipient so it
	// processes the message immediately if idle (or via drain-until-empty if
	// already running). Replaces the prior agent_send_message + agent_run pair,
	// which could leave a message queued but unprocessed if the run leg failed.
	type deliverParams struct {
		ID      string `json:"id"`
		Message string `json:"message"`
	}
	result := agentCall("agent_deliver", deliverParams{ID: input.AgentID, Message: labeledMessage})

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
