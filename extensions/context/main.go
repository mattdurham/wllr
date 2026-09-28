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
		prompt, parts := buildPromptParts(payload.Tools, payload.Commands)
		if prompt != "" {
			SetSystemPrompt(prompt)
			// Report the labeled decomposition so the /context breakdown can
			// attribute the prompt's cost to its sources (built-in rules,
			// tools list, prompt files, AGENTS.md, cwd note). The guest-side
			// component type differs from cwd.go's host-testable mirror, so
			// the part list is converted here.
			components := make([]SystemPromptComponent, 0, len(parts))
			for _, part := range parts {
				components = append(components, SystemPromptComponent{
					Source: part.Source,
					Chars:  part.Chars,
				})
			}
			SetSystemPromptComponents(components)
			Logf(1, "prompt: loaded system prompt (%d bytes, %d components)", len(prompt), len(components))
		}
	})
}

func main() {}
