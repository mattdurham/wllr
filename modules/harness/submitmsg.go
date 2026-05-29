package harness

// SubmitMsg carries user-submitted input text.
// Display, if non-empty, is shown in the chat instead of Content
// (useful when Content is a large internal payload like a skill XML block).
type SubmitMsg struct {
	Content string
	Display string
}
