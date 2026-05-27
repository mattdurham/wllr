package main

import "encoding/json"

type toolPayload struct {
	ToolCallID string
	Input      json.RawMessage
}
