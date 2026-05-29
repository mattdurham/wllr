package testutil

// RecordedCall holds the inputs captured from a single Stream or Generate call.
type RecordedCall struct {
	SystemPrompt string
	Prompt       string
	Messages     []string
}
