package testutil

// RecordedCall holds the inputs captured from a single Stream or Generate call.
type RecordedCall struct {
	// SystemPrompt is the system message content, if any.
	SystemPrompt string
	// Prompt is the final user-turn text (last user message in the prompt).
	Prompt string
	// Messages contains "role: content" strings for each non-system prompt message.
	Messages []string
}
