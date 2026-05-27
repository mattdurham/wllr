package sdk

// MessageEndPayload is the payload for EventMessageEnd.
type MessageEndPayload struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}
