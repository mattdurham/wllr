package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// SubmitMsg carries user-submitted input text.
// Display, if non-empty, is shown in the chat instead of Content
// (useful when Content is a large internal payload like a skill XML block).
type SubmitMsg struct {
	Content string
	Display string
}
