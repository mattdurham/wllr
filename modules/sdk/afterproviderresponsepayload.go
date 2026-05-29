package sdk

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// AfterProviderResponsePayload is the payload for EventAfterProviderResponse.
type AfterProviderResponsePayload struct {
	Usage UsageStats `json:"usage"`
}
