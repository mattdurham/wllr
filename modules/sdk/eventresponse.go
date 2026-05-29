package sdk

// EventResponse is the optional JSON response from _on_event.
type EventResponse struct {
	Error  string `json:"error,omitempty"`
	Cancel bool   `json:"cancel,omitempty"`
	Block  bool   `json:"block,omitempty"`
}
