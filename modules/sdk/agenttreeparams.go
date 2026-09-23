package sdk

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// AgentTreeNode is one node supplied to the agent tree overlay. The harness
// derives the tree shape from ParentID, so extensions describe the fleet as a
// flat list rather than pre-rendering a hierarchy.
type AgentTreeNode struct {
	// ID is the agent's identity, e.g. "main" or "main/coder".
	ID string `json:"id"`
	// ParentID is the owning agent's ID; empty for the root.
	ParentID string `json:"parent_id,omitempty"`
	// Label is the row's primary text (usually the agent name).
	Label string `json:"label,omitempty"`
	// Detail is the status line shown under the row while it is expanded.
	Detail string `json:"detail,omitempty"`
}

// ShowAgentTreeParams is the params blob for show_agent_tree.
type ShowAgentTreeParams struct {
	Title    string          `json:"title,omitempty"`
	Callback string          `json:"callback"`
	Nodes    []AgentTreeNode `json:"nodes"`
}

// SetFocusedAgentParams is the params blob for set_focused_agent.
type SetFocusedAgentParams struct {
	// ID is the agent receiving user input and owning the transcript. Empty
	// means the root agent.
	ID string `json:"id"`
}
