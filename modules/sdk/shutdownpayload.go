package sdk

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// ShutdownPayload is the payload for EventShutdown.
type ShutdownPayload struct {
	Reason string `json:"reason"`
}
