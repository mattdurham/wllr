package sdk

import "encoding/json"

// HostCallResponse is the JSON response returned by the host via host_call.
type HostCallResponse struct {
	Error  string          `json:"error,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
}
