package sdk

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// OnCommandPayload is the payload for EventOnCommand.
type OnCommandPayload struct {
	Name string   `json:"name"`
	Args []string `json:"args"`
}
