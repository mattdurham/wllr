package extension

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// AgentInfo describes a running agent. Returned by AgentBridge.List.
type AgentInfo struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	ActiveTool string `json:"active_tool,omitempty"`
	LastTool   string `json:"last_tool,omitempty"`
	Liveness   string `json:"liveness"`

	PendingMessages   int64 `json:"pending_messages"`
	LastActivityAgeMS int64 `json:"last_activity_age_ms"`
	TurnDurationMS    int64 `json:"turn_duration_ms"`
	LastToolAgeMS     int64 `json:"last_tool_age_ms"`
	LastToolDoneAgeMS int64 `json:"last_tool_done_age_ms"`

	IsRunning         bool `json:"is_running"`
	Working           bool `json:"working"`
	ShutdownRequested bool `json:"shutdown_requested"`
}
