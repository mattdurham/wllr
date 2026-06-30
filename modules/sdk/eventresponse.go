package sdk

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import "encoding/json"

// EventResponse is the optional JSON response from _on_event.
//
// An interceptor returns one of:
//   - observe:   nil / zero value
//   - transform: Payload set to a modified event payload (same shape as the
//     incoming Event.Payload). The host threads this through the interceptor
//     chain and applies it to the underlying operation (see DispatchEventChain).
//   - block:     Block (or Cancel) true, with Error carrying the surfaced reason.
type EventResponse struct {
	Error string `json:"error,omitempty"`
	// Payload, when non-empty, is the transformed event payload. It must be the
	// same JSON shape as the incoming Event.Payload for the dispatched EventType.
	// Empty means "no transformation" — the payload is left unchanged.
	Payload json.RawMessage `json:"payload,omitempty"`
	Cancel  bool            `json:"cancel,omitempty"`
	Block   bool            `json:"block,omitempty"`
}
