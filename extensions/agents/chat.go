//go:build wasip1

package main

import "fmt"

// This file implements the WASM-driven main chat transcript (UI P4). When the
// host enables it (WLLR_WASM_CHAT=1), the agents extension owns the "chat"
// scene area and builds the entire transcript — user prompts and streamed
// assistant text — via scene-graph patches. The harness renders this area's
// content inside its scrollable chat viewport. This proves a WASM component can
// drive the primary UI surface, with the Go side acting only as a bridge to the
// underlying agent layer.

const chatArea = "chat"

var (
	chatEnabled  bool
	chatSeq      int    // monotonic counter for unique node IDs
	chatAsstNode string // ID of the assistant text node for the in-flight turn
)

// initChat registers the transcript event handlers. They are inert until
// onChatSessionStart enables the feature after checking the host env.
func initChat() {
	OnBeforeAgentStart(onChatUserPrompt)
	OnToken(onChatToken)
	OnNotify(onChatNotify)
}

// onChatSessionStart enables the WASM transcript and creates the chat area with
// an empty vstack root. The harness renders this area as the main transcript;
// there is no built-in fallback renderer.
func onChatSessionStart() {
	chatEnabled = true
	UICreateArea(chatArea, "main", 0, "", "", "", "")
	UIPatch(chatArea, OpSetRoot(UIVStack("chat-root")))
}

// onChatUserPrompt appends a user message box and opens a fresh assistant text
// node for the upcoming turn.
func onChatUserPrompt(prompt string) {
	if !chatEnabled {
		return
	}
	chatSeq++
	userID := fmt.Sprintf("u%d", chatSeq)
	asstID := fmt.Sprintf("a%d", chatSeq)
	chatAsstNode = asstID

	userBox := UINode{
		ID:    userID,
		Type:  "text",
		Text:  prompt,
		Props: &UIProps{Border: "rounded", Fg: "success", Padding: []int{0, 1}, Width: "fill", Wrap: true},
	}
	asstBox := UINode{
		ID:    asstID,
		Type:  "text",
		Text:  "",
		Props: &UIProps{Border: "rounded", Fg: "accent", Padding: []int{0, 1}, Width: "fill", Wrap: true},
	}
	UIPatch(chatArea,
		OpInsert("chat-root", userBox),
		OpInsert("chat-root", asstBox),
	)
}

// onChatToken streams main-agent text into the current assistant node.
func onChatToken(agentID, text string) {
	if !chatEnabled || chatAsstNode == "" {
		return
	}
	// Only the main agent's text appears in the transcript; sub-agents work
	// silently in the background.
	if agentID != "" && agentID != "main" {
		return
	}
	UIPatch(chatArea, OpAppendText(chatAsstNode, text))
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
	UIPatch(chatArea, OpInsert("chat-root", node))
}
