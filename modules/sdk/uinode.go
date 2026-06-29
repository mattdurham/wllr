package sdk

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// UINodeType identifies the kind of a UI scene-graph node. Values map to
// bubbletea/lipgloss rendering primitives in the harness. The set is stable
// across ABI versions; an unknown type must render as an empty box so that
// forward-compatible extensions degrade gracefully.
type UINodeType string

const (
	// UINodeText is a leaf node carrying a string of text (the Text field).
	UINodeText UINodeType = "text"
	// UINodeVStack stacks its children vertically (lipgloss.JoinVertical).
	UINodeVStack UINodeType = "vstack"
	// UINodeHStack stacks its children horizontally (lipgloss.JoinHorizontal).
	UINodeHStack UINodeType = "hstack"
	// UINodeViewport is a scrollable region wrapping a single child subtree.
	UINodeViewport UINodeType = "viewport"
	// UINodeSpinner is an animated activity indicator (no children, no text).
	UINodeSpinner UINodeType = "spinner"
	// UINodeDivider is a horizontal rule spanning the available width.
	UINodeDivider UINodeType = "divider"
)

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
