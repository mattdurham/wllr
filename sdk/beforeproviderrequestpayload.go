package sdk

// BeforeProviderRequestPayload is the payload for EventBeforeProviderRequest.
type BeforeProviderRequestPayload struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
}
