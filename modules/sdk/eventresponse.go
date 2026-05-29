package sdk

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// EventResponse is the optional JSON response from _on_event.
type EventResponse struct {
	Error  string `json:"error,omitempty"`
	Cancel bool   `json:"cancel,omitempty"`
	Block  bool   `json:"block,omitempty"`
}
