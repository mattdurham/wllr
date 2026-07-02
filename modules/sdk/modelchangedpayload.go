package sdk

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// ModelChangedPayload is the payload for EventModelChanged.
type ModelChangedPayload struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
}
