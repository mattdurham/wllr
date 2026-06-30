package sdk

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

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
