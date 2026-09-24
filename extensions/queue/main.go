//go:build wasip1

// Package main is the /queue built-in extension for the bob coding harness.
// It provides a slash command to inspect and manage queued agent messages (inboxes).
package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"unsafe"
)

// ─── Local host_call for agent/team methods not wrapped by the SDK ────────────

//go:wasmimport env host_call
func _queueHostCall(reqPtr, reqLen, respPtrPtr, respLenPtr uint32) uint32

// queueCall fires a host_call and returns the unwrapped Result payload.
func queueCall(method string, params any) (string, error) {
	type request struct {
		Method string `json:"method"`
		Params any    `json:"params,omitempty"`
	}
	reqBytes, err := json.Marshal(request{Method: method, Params: params})
	if err != nil {
		return "", err
	}
	buf := make([]byte, len(reqBytes))
	copy(buf, reqBytes)
	ptr := uintptr(unsafe.Pointer(&buf[0]))

	var respPtr, respLen uint32
	_queueHostCall(
		uint32(ptr), uint32(len(buf)),
		uint32(uintptr(unsafe.Pointer(&respPtr))),
		uint32(uintptr(unsafe.Pointer(&respLen))),
	)
	if respPtr == 0 || respLen == 0 {
		return "", fmt.Errorf("host returned no response")
	}
	resp := make([]byte, respLen)
	mem := (*[1 << 28]byte)(unsafe.Pointer(uintptr(respPtr)))
	copy(resp, mem[:respLen])
	return unwrapHostCallEnvelope(resp)
}

// ─── Command handler ──────────────────────────────────────────────────────────

func onQueueCommand(args []string) {
	// Parse command arguments: /queue [agent-id] [list|delete|edit]
	if len(args) == 0 {
		Modal("/queue command help:\n" +
			"/queue                      - List all agents with pending messages\n" +
			"/queue <agent-id>           - Show inbox for specific agent\n" +
			"/queue list                 - List all agents with pending messages\n" +
			"/queue delete <agent-id> <index> - Delete message by index\n" +
			"/queue clear <agent-id>     - Discard ALL queued messages (works while a turn is running)\n" +
			"/queue edit <agent-id> <index> <content> - Edit message by index")
		return
	}

	if len(args) == 1 {
		// Show inbox for specific agent
		showAgentInbox(args[0])
		return
	}

	if len(args) >= 2 {
		action := args[1]
		switch action {
		case "list":
			listAgentsWithQueues()
		case "delete":
			if len(args) < 4 {
				Modal("Usage: /queue delete <agent-id> <index>")
				return
			}
			deleteMessage(args[2], args[3])
		case "clear":
			if len(args) < 3 {
				Modal("Usage: /queue clear <agent-id>")
				return
			}
			clearQueue(args[2])
		case "edit":
			if len(args) < 5 {
				Modal("Usage: /queue edit <agent-id> <index> <new-content>")
				return
			}
			content := strings.Join(args[3:], " ")
			editMessage(args[2], args[3], content)
		default:
			Modal(
				fmt.Sprintf(
					"Unknown action: %s\n\nAvailable actions:\n  list - List agents with pending messages\n  delete <agent-id> <index> - Delete message\n  clear <agent-id> - Discard all queued messages (works while a turn is running)\n  edit <agent-id> <index> <content> - Edit message",
					action,
				),
			)
		}
	}
}

func listAgentsWithQueues() {
	result, callErr := queueCall("agent_list", map[string]string{})
	var poolResp struct {
		Agents []struct {
			ID              string `json:"id"`
			Name            string `json:"name"`
			PendingMessages int    `json:"pending_messages"`
		} `json:"agents"`
	}

	if callErr == nil && result != "" {
		_ = json.Unmarshal([]byte(result), &poolResp)
	}

	var sb strings.Builder
	sb.WriteString("Agents with Queued Messages\n")
	sb.WriteString(strings.Repeat("─", 50))
	sb.WriteString("\n\n")

	found := false
	for _, a := range poolResp.Agents {
		if a.PendingMessages > 0 {
			found = true
			sb.WriteString(fmt.Sprintf("%s", a.ID))
			if a.Name != "" && a.Name != a.ID {
				sb.WriteString(fmt.Sprintf("  (%s)", a.Name))
			}
			sb.WriteString(fmt.Sprintf("\n  Pending messages: %d\n\n", a.PendingMessages))
		}
	}

	if !found {
		sb.WriteString("No agents have queued messages.")
	}

	Modal(strings.TrimRight(sb.String(), "\n"))
}

func showAgentInbox(agentID string) {
	result, callErr := queueCall("mailbox_snapshot", map[string]string{"id": agentID})
	if callErr != nil {
		Modal(fmt.Sprintf("Could not fetch inbox for agent %s: %v", agentID, callErr))
		return
	}

	var snapshot struct {
		Messages []struct {
			ID      string `json:"id"`
			Role    string `json:"role"`
			Content string `json:"content"`
			Type    string `json:"type"`
		} `json:"messages"`
	}
	if err := json.Unmarshal([]byte(result), &snapshot); err != nil {
		Modal(fmt.Sprintf("Failed to parse inbox for agent %s: %v", agentID, err))
		return
	}
	messages := snapshot.Messages

	if len(messages) == 0 {
		Modal(fmt.Sprintf("Agent %s has no queued messages.", agentID))
		return
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Inbox for %s (%d messages)\n", agentID, len(messages)))
	sb.WriteString(strings.Repeat("─", 60))
	sb.WriteString("\n\n")

	for i, msg := range messages {
		sb.WriteString(fmt.Sprintf("[%d] %s", i+1, strings.Title(msg.Role)))
		if msg.ID != "" {
			sb.WriteString(fmt.Sprintf(" (ID: %s)", msg.ID))
		}
		sb.WriteString("\n")

		content := msg.Content
		if len(content) > 200 {
			content = content[:200] + "…"
		}
		sb.WriteString(fmt.Sprintf("    %s\n", strings.ReplaceAll(content, "\n", " ")))
		sb.WriteString("\n")
	}

	Modal(strings.TrimRight(sb.String(), "\n"))
}

func deleteMessage(agentID, indexStr string) {
	// Try to parse as integer index
	var index int
	fmt.Sscanf(indexStr, "%d", &index)

	if index <= 0 {
		Modal("Index must be a positive integer")
		return
	}

	result, callErr := queueCall("mailbox_delete", map[string]any{
		"id":       agentID,
		"by_index": index - 1, // Convert to 0-based
	})

	if callErr != nil {
		Modal(fmt.Sprintf("Failed to delete message from agent %s: %v", agentID, callErr))
		return
	}

	var resp struct {
		Deleted int `json:"deleted"`
	}

	if err := json.Unmarshal([]byte(result), &resp); err != nil {
		Modal(fmt.Sprintf("Failed to parse delete response: %v", err))
		return
	}

	if resp.Deleted > 0 {
		Modal(fmt.Sprintf("Deleted %d message(s) from agent %s", resp.Deleted, agentID))
	} else {
		Modal(fmt.Sprintf("No messages deleted from agent %s", agentID))
	}
}

func clearQueue(agentID string) {
	result, callErr := queueCall("mailbox_clear", map[string]string{"id": agentID})
	if callErr != nil {
		Modal(fmt.Sprintf("Failed to clear queue for agent %s: %v", agentID, callErr))
		return
	}

	var resp struct {
		Removed int `json:"removed"`
	}

	if err := json.Unmarshal([]byte(result), &resp); err != nil {
		Modal(fmt.Sprintf("Failed to parse clear response: %v", err))
		return
	}

	if resp.Removed > 0 {
		Modal(fmt.Sprintf("Cleared %d queued message(s) from agent %s", resp.Removed, agentID))
	} else {
		Modal(fmt.Sprintf("Agent %s has no queued messages", agentID))
	}
}

func editMessage(agentID, indexStr, newContent string) {
	// Try to parse as integer index
	var index int
	fmt.Sscanf(indexStr, "%d", &index)

	if index <= 0 {
		Modal("Index must be a positive integer")
		return
	}

	if newContent == "" {
		Modal("New content cannot be empty")
		return
	}

	_, callErr := queueCall("mailbox_edit", map[string]any{
		"id":          agentID,
		"by_index":    index - 1, // Convert to 0-based
		"new_content": newContent,
	})

	// mailbox_edit returns no body on success; a failure arrives as an
	// envelope error, which queueCall surfaces as callErr.
	if callErr != nil {
		Modal(fmt.Sprintf("Failed to edit message in agent %s: %v", agentID, callErr))
		return
	}

	Modal(fmt.Sprintf("Successfully edited message %d in agent %s", index, agentID))
}

// ─── Initialization ───────────────────────────────────────────────────────────

func init() {
	// Register the /queue slash command
	RegisterCommand("queue", "Inspect and manage queued agent messages")

	// Register the command handler
	OnCommand("queue", onQueueCommand)

	// Agent-facing queue tools: sub-agents can see and cancel their own queued
	// messages (the orchestrator can target its sub-agents). Ownership is
	// enforced in resolveQueueTarget — the host mailbox calls are ungated by
	// design (the user's /queue command and ctrl+x rely on that).
	RegisterToolWithOutput(
		"queue_peek",
		"List messages queued for an agent's next turn (your own queue by default; you may name one of your own sub-agents). Returns each message with its 0-based index. Use queue_cancel to discard queued messages.",
		json.RawMessage(
			`{"type":"object","properties":{"agent_id":{"type":"string","description":"Agent whose queue to show (optional; defaults to your own queue)"}}}`,
		),
		json.RawMessage(
			`{"type":"object","properties":{"agent_id":{"type":"string"},"messages":{"type":"array","items":{"type":"object","properties":{"index":{"type":"integer"},"id":{"type":"string"},"role":{"type":"string"},"content":{"type":"string"},"type":{"type":"string"}}}}}}`,
		),
	)
	RegisterToolWithOutput(
		"queue_cancel",
		"Cancel messages queued for an agent (your own queue by default; you may name one of your own sub-agents). With no index or message_id this discards the whole queue — works even mid-turn. To cancel a single message, use message_id from queue_peek (also works mid-turn); the 0-based index selector requires the agent to be idle. A cancelled message cannot be recovered.",
		json.RawMessage(
			`{"type":"object","properties":{"agent_id":{"type":"string","description":"Agent whose queue to cancel (optional; defaults to your own queue)"},"index":{"type":"integer","description":"Cancel only the queued message at this 0-based index (from queue_peek)"},"message_id":{"type":"string","description":"Cancel only the queued message with this ID (from queue_peek)"}}}`,
		),
		json.RawMessage(
			`{"type":"object","properties":{"agent_id":{"type":"string"},"cancelled":{"type":"integer"}}}`,
		),
	)
	OnBeforeToolCall(onQueueToolCall)
}

// ─── Agent-facing queue tools ─────────────────────────────────────────────────

// onQueueToolCall answers the queue extension's own tools during the
// before_tool_call chain, mirroring how the implementing extension returns
// tool_result synchronously (see modules/extension ExecuteTool).
func onQueueToolCall(payload json.RawMessage) {
	var p beforeToolCallPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return
	}
	switch p.ToolName {
	case "queue_peek":
		handleQueuePeek(p)
	case "queue_cancel":
		handleQueueCancel(p)
	}
}

// handleQueuePeek lists the target agent's queued messages annotated with their
// 0-based index. Every path answers the tool call — ExecuteTool blocks until a
// tool_result arrives.
func handleQueuePeek(p beforeToolCallPayload) {
	req, err := parseQueueToolRequest(p.Input)
	if err != nil {
		ToolResult(p.ToolCallID, "queue_peek: "+err.Error(), true)
		return
	}
	caller := p.AgentID
	target, ok := resolveQueueTarget(caller, req.AgentID)
	if !ok {
		ToolResult(
			p.ToolCallID,
			fmt.Sprintf("queue_peek: agent_id %q is not you or one of your sub-agents", req.AgentID),
			true,
		)
		return
	}
	result, callErr := queueCall("mailbox_snapshot", map[string]string{"id": target})
	if callErr != nil {
		ToolResult(p.ToolCallID, fmt.Sprintf("queue_peek: %v", callErr), true)
		return
	}
	if result == "" {
		ToolResult(p.ToolCallID, "queue_peek: host returned no response", true)
		return
	}
	var snapshot struct {
		Messages []struct {
			ID      string `json:"id"`
			Role    string `json:"role"`
			Content string `json:"content"`
			Type    string `json:"type"`
		} `json:"messages"`
	}
	if err := json.Unmarshal([]byte(result), &snapshot); err != nil {
		ToolResult(p.ToolCallID, fmt.Sprintf("queue_peek: failed to parse inbox for agent %s: %v", target, err), true)
		return
	}
	entries := make([]map[string]any, 0, len(snapshot.Messages))
	for i, m := range snapshot.Messages {
		e := map[string]any{"index": i, "role": m.Role, "content": m.Content, "type": m.Type}
		if m.ID != "" {
			e["id"] = m.ID
		}
		entries = append(entries, e)
	}
	out, _ := json.Marshal(map[string]any{"agent_id": target, "messages": entries})
	ToolResult(p.ToolCallID, string(out), false)
}

// handleQueueCancel discards the whole target queue or a single message, and
// reports how many messages were cancelled.
func handleQueueCancel(p beforeToolCallPayload) {
	req, err := parseQueueToolRequest(p.Input)
	if err != nil {
		ToolResult(p.ToolCallID, "queue_cancel: "+err.Error(), true)
		return
	}
	caller := p.AgentID
	plan, err := planCancel(caller, req)
	if err != nil {
		ToolResult(p.ToolCallID, "queue_cancel: "+err.Error(), true)
		return
	}
	var result string
	var callErr error
	switch {
	case plan.Clear:
		result, callErr = queueCall("mailbox_clear", map[string]string{"id": plan.Target})
	case plan.ByIndex != nil:
		result, callErr = queueCall("mailbox_delete", map[string]any{
			"id":       plan.Target,
			"by_index": *plan.ByIndex,
		})
	default:
		result, callErr = queueCall("mailbox_delete", map[string]string{
			"id":            plan.Target,
			"by_message_id": plan.ByMessageID,
		})
	}
	if callErr != nil {
		ToolResult(p.ToolCallID, fmt.Sprintf("queue_cancel: %v", callErr), true)
		return
	}
	var count struct {
		Removed int `json:"removed"`
		Deleted int `json:"deleted"`
	}
	_ = json.Unmarshal([]byte(result), &count)
	cancelled := count.Removed + count.Deleted
	out, _ := json.Marshal(map[string]any{"agent_id": plan.Target, "cancelled": cancelled})
	ToolResult(p.ToolCallID, string(out), false)
}

func main() {}
