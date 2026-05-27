package main

import "encoding/json"

// HostCallRequest is the JSON-RPC envelope for host_call.
type HostCallRequest struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}
