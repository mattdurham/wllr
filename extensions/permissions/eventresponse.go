package main

// EventResponse is returned by OnEvent to signal cancellation, blocking, or errors.
type EventResponse struct {
	Cancel bool   `json:"cancel,omitempty"`
	Block  bool   `json:"block,omitempty"`
	Error  string `json:"error,omitempty"`
}
