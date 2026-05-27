package sdk

// ShutdownPayload is the payload for EventShutdown.
type ShutdownPayload struct {
	Reason string `json:"reason"`
}
