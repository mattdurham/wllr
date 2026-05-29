package sdk

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// Message is a chat message.
type Message struct {
	Role    Role   `json:"role"`
	Content string `json:"content"`
}
