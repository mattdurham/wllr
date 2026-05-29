package sdk

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// MessageEndPayload is the payload for EventMessageEnd.
type MessageEndPayload struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}
