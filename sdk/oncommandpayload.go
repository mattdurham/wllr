package sdk

// OnCommandPayload is the payload for EventOnCommand.
type OnCommandPayload struct {
	Name string   `json:"name"`
	Args []string `json:"args"`
}
