package main

// shouldRecordUserPrompt accepts prompts sent to the root agent by the user.
// Queued inbox work is skipped unless it is user-authored /steer guidance.
func shouldRecordUserPrompt(agentID string, queued, steer bool) bool {
	if agentID != "" && agentID != "main" {
		return false
	}
	return !queued || steer
}
