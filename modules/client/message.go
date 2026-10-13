package client

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// Message is one history entry with its server-rendered HTML body.
type Message struct {
	ID      string `json:"id,omitempty"`
	Role    string `json:"role"`
	Content string `json:"content"`
	Type    string `json:"type,omitempty"`
	HTML    string `json:"html,omitempty"`
}
