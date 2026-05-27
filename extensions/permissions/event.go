package main

import "encoding/json"

// Event represents an event dispatched from the host.
type Event struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}
