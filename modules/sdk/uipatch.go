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

// UIPatchOp is a single scene-graph mutation. Which fields are meaningful
// depends on Op (see UIPatchOpType). Unused fields are omitted on the wire.
type UIPatchOp struct {
	Op UIPatchOpType `json:"op"`
	// Parent is the parent node ID for UIOpInsert. "" targets the area root.
	Parent string `json:"parent,omitempty"`
	// Index is the insert position for UIOpInsert. Nil appends to the end.
	Index *int `json:"index,omitempty"`
	// ID targets a node for UIOpUpdate, UIOpRemove, and UIOpAppendText.
	ID string `json:"id,omitempty"`
	// Node is the payload for UIOpSetRoot and UIOpInsert.
	Node *UINode `json:"node,omitempty"`
	// Props is the payload for UIOpUpdate.
	Props *UIProps `json:"props,omitempty"`
	// Text is the payload for UIOpAppendText.
	Text string `json:"text,omitempty"`
}

// UIPatchParams is the params blob for the ui_patch host_call. Ops apply in
// order to the named Area; the batch is all-or-nothing — if any op references a
// missing node the host rejects the whole batch with an error response.
type UIPatchParams struct {
	Area string      `json:"area"`
	Ops  []UIPatchOp `json:"ops"`
}
