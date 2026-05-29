package main

//lint:ignore U1000 used in WASM build (wasip1 tag)
type messageEntry struct {
	Type      string `json:"type"`
	ID        string `json:"id"`
	Timestamp string `json:"timestamp"`
	Role      string `json:"role"`
	Content   string `json:"content"`
}
