//go:build wasip1

package main

// Message is a chat message for AgentResetHistory.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}
