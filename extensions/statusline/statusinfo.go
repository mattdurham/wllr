//go:build wasip1

package main

// StatusInfo holds a snapshot of the current TUI status for status line rendering.
type StatusInfo struct {
	Statuses     map[string]string `json:"statuses"`
	Provider     string            `json:"provider"`
	Model        string            `json:"model"`
	Tokens       int               `json:"tokens"`
	Working      bool              `json:"working"`
	ElapsedMs    int64             `json:"elapsed_ms"`
	ActiveAgents int               `json:"active_agents"`
	Width        int               `json:"width"`
	HasError     bool              `json:"has_error"`
}
