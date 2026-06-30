package sdk

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// UIPatchOpType identifies a single mutation applied to an area's scene graph.
// The host applies a batch of ops atomically in order (see UIPatchParams).
type UIPatchOpType string

const (
	// UIOpSetRoot replaces the entire scene graph of the target area with Node.
	// Parent, Index, ID, Props, and Text are ignored.
	UIOpSetRoot UIPatchOpType = "set_root"
	// UIOpInsert inserts Node as a child of Parent at position Index. A nil
	// Index appends. Parent == "" targets the area root container.
	UIOpInsert UIPatchOpType = "insert"
	// UIOpUpdate replaces the Props of the node identified by ID. A nil Props
	// clears all props back to defaults.
	UIOpUpdate UIPatchOpType = "update"
	// UIOpRemove removes the node identified by ID and its subtree.
	UIOpRemove UIPatchOpType = "remove"
	// UIOpAppendText appends Text to the existing text of the UINodeText node
	// identified by ID. This is the cheap streaming op: token deltas append
	// without re-serializing the surrounding subtree.
	UIOpAppendText UIPatchOpType = "append_text"
)
