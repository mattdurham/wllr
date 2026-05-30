package sdk

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// StatusInfo is returned by the get_status_info host_call.
// It gives extensions a read-only snapshot of the current status bar state
// so they can compose a fully custom status line.
type StatusInfo struct {
	Statuses     map[string]string `json:"statuses"`
	Provider     string            `json:"provider"`
	Model        string            `json:"model"`
	Tokens       int               `json:"tokens"`
	ElapsedMs    int64             `json:"elapsed_ms"`    // ms since current stream started; 0 when idle
	ActiveAgents int               `json:"active_agents"` // number of sub-agents currently in the pool
	Width        int               `json:"width"`         // terminal width in columns
	Working      bool              `json:"working"`
	HasError     bool              `json:"has_error"` // true when last stream ended with an error
}
