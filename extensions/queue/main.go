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

// queueCall fires a host_call and returns the raw response bytes, or "".
func queueCall(method string, params any) string {
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
	_queueHostCall(
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

// ─── Command handler ──────────────────────────────────────────────────────────

func onQueueCommand(args []string) {
	// Parse command arguments: /queue [agent-id] [list|delete|edit]
	if len(args) == 0 {
		Modal("/queue command help:\n" +
			"/queue                      - List all agents with pending messages\n" +
			"/queue <agent-id>           - Show inbox for specific agent\n" +
			"/queue delete <agent-id> <index> - Delete message by index\n" +
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
		case "edit":
			if len(args) < 5 {
				Modal("Usage: /queue edit <agent-id> <index> <new-content>")
				return
			}
			content := strings.Join(args[3:], " ")
			editMessage(args[2], args[3], content)
		default:
			Modal(fmt.Sprintf("Unknown action: %s\n\nAvailable actions:\n  list - List agents with pending messages\n  delete <agent-id> <index> - Delete message\n  edit <agent-id> <index> <content> - Edit message", action))
		}
	}
}

func listAgentsWithQueues() {
	result := queueCall("agent_list", map[string]string{})
	var poolResp struct {
		Agents []struct {
			ID              string `json:"id"`
			Name            string `json:"name"`
			PendingMessages int    `json:"pending_messages"`
		} `json:"agents"`
	}

	if result != "" {
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
	result := queueCall("mailbox_snapshot", map[string]string{"id": agentID})
	if result == "" {
		Modal(fmt.Sprintf("Could not fetch inbox for agent: %s", agentID))
		return
	}

	var messages []struct {
		ID      string `json:"id"`
		Role    string `json:"role"`
		Content string `json:"content"`
		Type    string `json:"type"`
	}

	if err := json.Unmarshal([]byte(result), &messages); err != nil {
		Modal(fmt.Sprintf("Failed to parse inbox for agent %s: %v", agentID, err))
		return
	}

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

	result := queueCall("mailbox_delete", map[string]string{
		"id":       agentID,
		"by_index": fmt.Sprintf("%d", index-1), // Convert to 0-based
	})

	if result == "" {
		Modal(fmt.Sprintf("Failed to delete message from agent %s", agentID))
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

	result := queueCall("mailbox_edit", map[string]string{
		"id":          agentID,
		"by_index":    fmt.Sprintf("%d", index-1), // Convert to 0-based
		"new_content": newContent,
	})

	if result == "" {
		Modal(fmt.Sprintf("Failed to edit message in agent %s", agentID))
		return
	}

	var resp struct {
		Success bool `json:"success"`
	}

	if err := json.Unmarshal([]byte(result), &resp); err != nil {
		Modal(fmt.Sprintf("Failed to parse edit response: %v", err))
		return
	}

	if resp.Success {
		Modal(fmt.Sprintf("Successfully edited message %d in agent %s", index, agentID))
	} else {
		Modal(fmt.Sprintf("Failed to edit message %d in agent %s", index, agentID))
	}
}

// ─── Initialization ───────────────────────────────────────────────────────────

func init() {
	// Register the /queue slash command
	RegisterCommand("queue", "Inspect and manage queued agent messages")

	// Register the command handler
	OnCommand("queue", onQueueCommand)
}

func main() {}
