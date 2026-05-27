package main

import "encoding/json"

// HostCallResponse is the JSON-RPC response from host_call.
type HostCallResponse struct {
	Result json.RawMessage `json:"result"`
	Error  string          `json:"error"`
}
