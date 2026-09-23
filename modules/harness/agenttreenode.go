package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// AgentTreeNode is one agent in the interactive /agents tree. The extension
// supplies a flat list; the tree shape comes from ParentID.
type AgentTreeNode struct {
	// ID is the agent's identity, e.g. "main" or "main/coder".
	ID string
	// ParentID is the owning agent's ID; empty for the root.
	ParentID string
	// Label is the row's primary text (usually the agent name).
	Label string
	// Detail is the status line rendered under the row when expanded.
	Detail string
}
