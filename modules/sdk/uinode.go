package sdk

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// UINode is one node in an area's UI scene graph. Nodes form a tree addressed
// by stable IDs so the harness can apply incremental patches (see UIPatchOp)
// without re-serializing the whole tree.
//
// ID must be unique within its owning area. Type selects the rendering
// primitive. Text is meaningful only for UINodeText nodes. Props carries
// optional style/layout attributes. Children is meaningful only for container
// types (vstack, hstack, viewport).
type UINode struct {
	ID       string     `json:"id"`
	Type     UINodeType `json:"type"`
	Text     string     `json:"text,omitempty"`
	Props    *UIProps   `json:"props,omitempty"`
	Children []UINode   `json:"children,omitempty"`
}
