package sdk

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// SessionStartPayload is the payload for EventSessionStart.
type SessionStartPayload struct {
	Reason string `json:"reason"`
}
