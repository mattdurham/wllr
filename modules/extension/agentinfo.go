package extension

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// AgentInfo describes a running agent. Returned by AgentBridge.List.
type AgentInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
