package sdk

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// BeforeProviderRequestPayload is the payload for EventBeforeProviderRequest.
type BeforeProviderRequestPayload struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
}
