//go:build wasip1

package main

import "fmt"

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
	UICreateArea(chatArea, "main", 0, "", "", "", "")
	UIPatch(chatArea, OpSetRoot(UIVStack(chatRootID)))
}

// onChatUserPrompt appends a user message box and reserves a fresh assistant text
// node for the upcoming turn. The assistant box is inserted on first token so an
// empty response box does not appear while the provider is still thinking.
func onChatUserPrompt(prompt string, _ bool) {
	if !chatEnabled || prompt == "" {
		return
	}
	chatSeq++
	userID := fmt.Sprintf("u%d", chatSeq)
	chatPendingAsstNode = fmt.Sprintf("a%d", chatSeq)
	chatAsstNode = ""

	userBox := UINode{
		ID:    userID,
		Type:  "text",
		Text:  prompt,
		Props: &UIProps{Border: "rounded", Fg: "success", Padding: []int{0, 1}, Width: "fill", Wrap: true},
	}
	UIPatch(chatArea, OpInsert(chatRootID, userBox))
}

// onChatToken streams main-agent text into the current assistant node.
func onChatToken(agentID, text string) {
	if !chatEnabled {
		return
	}
	// Only the main agent's text appears in the transcript; sub-agents work
	// silently in the background.
	if agentID != "" && agentID != "main" {
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

// onChatMessageEnd finalizes the assistant bubble when a response completes
// without any streamed token batches reaching the transcript first.
func onChatMessageEnd(role, content string) {
	if !chatEnabled || role != "assistant" || content == "" {
		return
	}
	if chatAsstNode != "" {
		return
	}
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
		Text:  content,
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
