package testutil

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// RecordedCall holds the inputs captured from a single Stream or Generate call.
type RecordedCall struct {
	SystemPrompt string
	Prompt       string
	Messages     []string
}
