//go:build wasip1

package main

import "encoding/json"

func init() {
	RegisterTool(
		"get_env",
		"Read environment variables from the host system",
		json.RawMessage(`{"type":"object","properties":{"name":{"type":"string","description":"Specific env var name to look up (optional — omit to get all)"}}}`),
	)
	OnToolCall(func(callID, name string, input json.RawMessage) (string, bool) {
		if name != "get_env" {
			return "", false
		}
		var in struct {
			Name string `json:"name"`
		}
		json.Unmarshal(input, &in)
		val, err := GetEnv(in.Name)
		if err != nil {
			return err.Error(), true
		}
		return val, false
	})
}

func main() {}
