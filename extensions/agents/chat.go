//go:build wasip1

package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

// This file implements the WASM-driven main chat transcript (UI P4). When the
// host enables it, the agents extension owns the "chat"
// scene area and builds the entire transcript — user prompts and streamed
// assistant text — via scene-graph patches. The harness renders this area's
// content inside its scrollable chat viewport. This proves a WASM component can
// drive the primary UI surface, with the Go side acting only as a bridge to the
// underlying agent layer.

const (
	chatArea   = "chat"
	chatRootID = "chat-root"
)

var (
	chatEnabled         bool
	chatSeq             int    // monotonic counter for unique node IDs
	chatAsstNode        string // ID of the assistant text node for the in-flight turn
	chatPendingAsstNode string // reserved assistant node ID, inserted on first token
	markdownEnabled     bool   // set from WLLR_NO_MARKDOWN on session start
)

// initChat registers the transcript event handlers. They are inert until
// onChatSessionStart enables the feature after checking the host env.
func initChat() {
	OnBeforeAgentStart(onChatUserPrompt)
	OnToken(onChatToken)
	OnMessageEnd(onChatMessageEnd)
	OnNotify(onChatNotify)
}

// onChatSessionStart enables the WASM transcript and creates the chat area with
// an empty vstack root. The harness renders this area as the main transcript;
// there is no built-in fallback renderer.
func onChatSessionStart() {
	chatEnabled = true
	// Markdown rendering is on by default; WLLR_NO_MARKDOWN=1 disables it.
	markdownEnabled = true
	if v, err := GetEnv("WLLR_NO_MARKDOWN"); err == nil && (v == "1" || strings.EqualFold(v, "true")) {
		markdownEnabled = false
	}
	UICreateArea(chatArea, "main", 0, "", "", "", "")
	UIPatch(chatArea, OpSetRoot(UIVStack(chatRootID)))
}

// onChatUserPrompt appends a user message box and reserves a fresh assistant text
// node for the upcoming turn. The assistant box is inserted on first token so an
// empty response box does not appear while the provider is still thinking.
func onChatUserPrompt(agentID, prompt string, _ bool) {
	if !chatEnabled || prompt == "" {
		return
	}
	// Prompts for other agents belong to their own transcript. An absent id is
	// attributed to the root so older dispatch paths keep working.
	owner := agentID
	if owner == "" {
		owner = "main"
	}
	focused := focusedAgentID
	if focused == "" {
		focused = "main"
	}
	if owner != focused {
		return
	}
	chatSeq++
	userID := fmt.Sprintf("u%d", chatSeq)
	chatPendingAsstNode = fmt.Sprintf("a%d", chatSeq)
	chatAsstNode = ""

	userBox := UINode{
		ID:   userID,
		Type: "text",
		// The user prompt is final the moment it's typed, so trailing newlines
		// (e.g. from a pasted block) are trimmed here rather than at render
		// time, which would also clip in-flight streamed assistant text.
		Text:  strings.TrimRight(prompt, "\n\r"),
		Props: &UIProps{Border: "rounded", Fg: "success", Padding: []int{0, 1}, Width: "fill", Wrap: true},
	}
	UIPatch(chatArea, OpInsert(chatRootID, userBox))
}

// onChatToken streams main-agent text into the current assistant node.
func onChatToken(agentID, text string) {
	if !chatEnabled {
		return
	}
	// The transcript shows the focused agent. An empty focus means the root, and
	// a token with no agent id is attributed to the root so older dispatch paths
	// keep working.
	owner := agentID
	if owner == "" {
		owner = "main"
	}
	focused := focusedAgentID
	if focused == "" {
		focused = "main"
	}
	if owner != focused {
		return
	}
	if chatAsstNode == "" {
		asstID := chatPendingAsstNode
		if asstID == "" {
			chatSeq++
			asstID = fmt.Sprintf("a%d", chatSeq)
		}
		chatPendingAsstNode = ""
		chatAsstNode = asstID
		asstBox := UINode{
			ID:    asstID,
			Type:  "text",
			Text:  text,
			Props: &UIProps{Border: "rounded", Fg: "accent", Padding: []int{0, 1}, Width: "fill", Wrap: true},
		}
		UIPatch(chatArea, OpInsert(chatRootID, asstBox))
		return
	}
	UIPatch(chatArea, OpAppendText(chatAsstNode, text))
}

// onChatMessageEnd finalizes the assistant bubble when a response completes.
// For streamed turns it replaces the in-flight assistant node's raw markdown
// with the rendered (ANSI-styled) result; for non-streamed turns it inserts a
// freshly rendered assistant node. When markdown is disabled, content is left
// verbatim.
func onChatMessageEnd(role, content string) {
	if !chatEnabled || role != "assistant" || content == "" {
		return
	}
	display := content
	if markdownEnabled {
		display = FormatMarkdown(content)
		if display == "" {
			display = content
		}
	}
	// The response is final at this point, so trim trailing newlines here
	// rather than at render time, which would also clip in-flight streamed
	// text before the turn is actually done.
	display = strings.TrimRight(display, "\n\r")
	if chatAsstNode != "" {
		// Streamed turn: the in-flight node already holds the raw text. Swap it
		// for the rendered version without breaking append-mode streaming.
		UIPatch(chatArea, OpReplaceText(chatAsstNode, display))
		chatAsstNode = ""
		return
	}
	asstID := chatPendingAsstNode
	if asstID == "" {
		chatSeq++
		asstID = fmt.Sprintf("a%d", chatSeq)
	}
	chatPendingAsstNode = ""
	chatAsstNode = ""
	asstBox := UINode{
		ID:    asstID,
		Type:  "text",
		Text:  display,
		Props: &UIProps{Border: "rounded", Fg: "accent", Padding: []int{0, 1}, Width: "fill", Wrap: true},
	}
	UIPatch(chatArea, OpInsert(chatRootID, asstBox))
}

// onChatNotify renders a system notification as an italic line in the transcript.
func onChatNotify(text string) {
	if !chatEnabled {
		return
	}
	chatSeq++
	node := UINode{
		ID:    fmt.Sprintf("n%d", chatSeq),
		Type:  "text",
		Text:  "» " + text,
		Props: &UIProps{Fg: "muted", Italic: true, Width: "fill", Wrap: true},
	}
	UIPatch(chatArea, OpInsert(chatRootID, node))
}

// focusedAgentID is the agent whose conversation the transcript shows. Empty
// means the root agent; the root is not special, only the default.
var focusedAgentID string

// RebuildTranscriptFor switches the transcript to the named agent and repaints
// it from that agent's stored history. This is what makes main and sub-agents
// interchangeable: the view is a function of the focused agent, not of a
// hardcoded main conversation.
func RebuildTranscriptFor(id string) {
	if !chatEnabled {
		return
	}
	focusedAgentID = id
	// Reset the transcript: nodes are re-created from history, so stale text
	// from the previous agent cannot linger.
	UIPatch(chatArea, OpSetRoot(UIVStack(chatRootID)))
	chatSeq = 0
	chatAsstNode = ""
	chatPendingAsstNode = ""

	result := agentCall("agent_get_history", map[string]string{"id": id})
	var hist struct {
		ID       string `json:"id"`
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
			Type    string `json:"type"`
		} `json:"messages"`
	}
	if result != "" {
		_ = json.Unmarshal([]byte(result), &hist)
	}
	for _, m := range hist.Messages {
		// Control and protocol messages are broker/lifecycle traffic, not
		// conversation. They are model-visible (the orchestrator relies on them)
		// but must not render as user bubbles.
		if m.Type == "system" || m.Type == "protocol" || strings.TrimSpace(m.Content) == "" {
			continue
		}
		switch m.Role {
		case "user":
			appendUserBox(m.Content)
		case "assistant":
			appendAssistantBox(m.Content)
		}
	}
}

// appendUserBox inserts a user message box.
func appendUserBox(text string) {
	chatSeq++
	box := UINode{
		ID:    fmt.Sprintf("u%d", chatSeq),
		Type:  "text",
		Text:  strings.TrimRight(text, "\n\r"),
		Props: &UIProps{Border: "rounded", Fg: "success", Padding: []int{0, 1}, Width: "fill", Wrap: true},
	}
	UIPatch(chatArea, OpInsert(chatRootID, box))
}

// appendAssistantBox inserts a finalized assistant message box.
func appendAssistantBox(text string) {
	display := text
	if markdownEnabled {
		if rendered := FormatMarkdown(text); rendered != "" {
			display = rendered
		}
	}
	chatSeq++
	box := UINode{
		ID:    fmt.Sprintf("a%d", chatSeq),
		Type:  "text",
		Text:  strings.TrimRight(display, "\n\r"),
		Props: &UIProps{Border: "rounded", Fg: "accent", Padding: []int{0, 1}, Width: "fill", Wrap: true},
	}
	UIPatch(chatArea, OpInsert(chatRootID, box))
}
