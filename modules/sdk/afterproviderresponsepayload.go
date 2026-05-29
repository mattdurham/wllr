package sdk

// AfterProviderResponsePayload is the payload for EventAfterProviderResponse.
type AfterProviderResponsePayload struct {
	Usage UsageStats `json:"usage"`
}
