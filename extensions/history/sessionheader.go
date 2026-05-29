package main

//lint:ignore U1000 used in WASM build (wasip1 tag)
type sessionHeader struct {
	Type      string `json:"type"`
	ID        string `json:"id"`
	Timestamp string `json:"timestamp"`
	CWD       string `json:"cwd"`
}
