//go:build wasip1

package main

import (
	"encoding/json"
	"fmt"
)

func init() {
	RegisterTool(
		"write_file",
		"Write content to a file on the filesystem",
		json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"Path of the file to write"},"content":{"type":"string","description":"Content to write to the file"}},"required":["path","content"]}`),
	)
	OnToolCall(func(callID, name string, input json.RawMessage) (string, bool) {
		if name != "write_file" {
			return "", false
		}
		var in struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		}
		if err := json.Unmarshal(input, &in); err != nil || in.Path == "" {
			return "path is required", true
		}
		if err := WriteFile(in.Path, in.Content); err != nil {
			return "write_file: " + err.Error(), true
		}
		return fmt.Sprintf("written %d bytes to %s", len(in.Content), in.Path), false
	})
}

func main() {}
