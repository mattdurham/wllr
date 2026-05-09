//go:build wasip1

package main

import (
	"encoding/json"
	"os"
)

func init() {
	RegisterTool(
		"read_file",
		"Read the contents of a file from the filesystem",
		json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"Absolute or relative path of the file to read"}},"required":["path"]}`),
	)
	OnToolCall(func(callID, name string, input json.RawMessage) (string, bool) {
		if name != "read_file" {
			return "", false
		}
		var in struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal(input, &in); err != nil || in.Path == "" {
			return "path is required", true
		}
		content, err := os.ReadFile(in.Path)
		if err != nil {
			return "read_file: " + err.Error(), true
		}
		return string(content), false
	})
}

func main() {}
