package sdk

import "encoding/json"

// Tool is a function the LLM may call.
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
	Override    bool            `json:"override,omitempty"`
}
