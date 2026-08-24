//go:build wasip1

// Package main is the prompt built-in extension for the wllr coding harness.
// It owns the complete base prompt while other extensions may append sections.
package main

import "encoding/json"

func init() {
	OnRawSessionStart(func(data []byte) {
		var payload sessionPromptPayload
		if err := json.Unmarshal(data, &payload); err != nil {
			Logf(2, "prompt: invalid session_start payload: %v", err)
			return
		}
		prompt := buildPrompt(payload.Tools, payload.Commands)
		if prompt != "" {
			SetSystemPrompt(prompt)
			Logf(1, "prompt: loaded system prompt (%d bytes)", len(prompt))
		}
	})
}

func main() {}
