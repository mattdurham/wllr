package sdk

import "encoding/json"

// Event is dispatched to extensions via _on_event.
type Event struct {
	Type    EventType       `json:"type"`
	Payload json.RawMessage `json:"payload"`
}
