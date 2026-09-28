package agent

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// messagesize.go holds the per-message attribution value type returned inside
// ContextBreakdown (LargestMessages). It lives apart from contextbreakdown.go
// because that file's revive max-public-structs budget is spent on
// ContextBreakdown itself.

// MessageSize is one history message's estimated size, used to surface the
// individual prompts that dominate the window.
type MessageSize struct {
	Role   string // "user" or "assistant"
	Tokens int64
	Chars  int
}

// maxLargestMessages caps the LargestMessages slice so the modal stays a
// summary, not a message listing.
const maxLargestMessages = 5
