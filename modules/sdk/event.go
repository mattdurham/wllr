package sdk

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import "encoding/json"

// Event is dispatched to extensions via _on_event.
type Event struct {
	Type    EventType       `json:"type"`
	Payload json.RawMessage `json:"payload"`
}
