package sdk

import "encoding/json"

// HostCallRequest is the JSON payload sent by an extension via host_call.
type HostCallRequest struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}
