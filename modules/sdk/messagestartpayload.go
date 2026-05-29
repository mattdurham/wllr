package sdk

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// MessageStartPayload is the payload for EventMessageStart.
type MessageStartPayload struct {
	Role string `json:"role"`
}
