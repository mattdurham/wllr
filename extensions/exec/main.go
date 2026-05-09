//go:build wasip1

package main

import "encoding/json"

func init() {
	RegisterTool(
		"exec",
		"Execute a shell command on the host system",
		json.RawMessage(`{"type":"object","properties":{"command":{"type":"string","description":"Shell command to execute"},"dir":{"type":"string","description":"Working directory (optional, defaults to current)"}},"required":["command"]}`),
	)
	OnToolCall(func(callID, name string, input json.RawMessage) (string, bool) {
		if name != "exec" {
			return "", false
		}
		var in struct {
			Command string `json:"command"`
			Dir     string `json:"dir"`
		}
		if err := json.Unmarshal(input, &in); err != nil || in.Command == "" {
			return "command is required", true
		}
		out, err := Exec(in.Command, in.Dir)
		if err != nil {
			if out != "" {
				return out + "\nerror: " + err.Error(), true
			}
			return err.Error(), true
		}
		return out, false
	})
}

func main() {}
