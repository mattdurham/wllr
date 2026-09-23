package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// AgentTreeKeyResult reports what a key press did to the tree.
type AgentTreeKeyResult struct {
	// Focused is the agent to switch to, set when the user confirms with enter.
	Focused string
	// Closed is set when the user dismissed the tree with q.
	Closed bool
	// Folded is set when a node was expanded or collapsed, so the caller can
	// re-render without re-fetching nodes.
	Folded bool
	// Handled is false when the key is not the tree's.
	Handled bool
}
