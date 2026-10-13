package sdk

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// This file is the single source of truth for scene-graph mutation. Both
// front-end renderers depend on it: the terminal renderer (modules/harness
// SceneRenderer) and the HTML renderer (modules/server SceneModel). Keeping the
// ops here means the two renderers cannot drift on insert/remove/append
// semantics — the graph is manipulated identically and only the final
// node-to-pixels step differs.
//
// Everything here is pure: no mutation of shared state, no locking, no I/O.
// Callers own concurrency.

import (
	"fmt"
	"strings"
)

// ApplyOp applies a single patch op to root and returns the new root. It is the
// reference implementation of UIPatchOp semantics. A returned error means the
// op was rejected and the caller's tree must be left untouched (callers apply
// ops to a clone and only publish it if the whole batch succeeds).
func ApplyOp(root *UINode, op UIPatchOp) (*UINode, error) {
	switch op.Op {
	case UIOpSetRoot:
		if op.Node == nil {
			return nil, fmt.Errorf("set_root requires node")
		}
		return CloneNode(op.Node), nil
	case UIOpInsert:
		if op.Node == nil {
			return nil, fmt.Errorf("insert requires node")
		}
		if op.Parent == "" {
			// Insert at the root container level requires an existing root container.
			if root == nil {
				return nil, fmt.Errorf("insert: no root; use set_root first")
			}
			if err := InsertChild(root, op.Index, *CloneNode(op.Node)); err != nil {
				return nil, err
			}
			return root, nil
		}
		parent := FindNode(root, op.Parent)
		if parent == nil {
			return nil, fmt.Errorf("insert: parent not found: %s", op.Parent)
		}
		if err := InsertChild(parent, op.Index, *CloneNode(op.Node)); err != nil {
			return nil, err
		}
		return root, nil
	case UIOpUpdate:
		n := FindNode(root, op.ID)
		if n == nil {
			return nil, fmt.Errorf("update: node not found: %s", op.ID)
		}
		n.Props = CloneProps(op.Props)
		return root, nil
	case UIOpRemove:
		if root != nil && root.ID == op.ID {
			return nil, nil
		}
		if !RemoveNode(root, op.ID) {
			return nil, fmt.Errorf("remove: node not found: %s", op.ID)
		}
		return root, nil
	case UIOpAppendText:
		n := FindNode(root, op.ID)
		if n == nil {
			return nil, fmt.Errorf("append_text: node not found: %s", op.ID)
		}
		if n.Type != UINodeText {
			return nil, fmt.Errorf("append_text: node %s is not a text node", op.ID)
		}
		n.Text += op.Text
		return root, nil
	case UIOpReplaceText:
		n := FindNode(root, op.ID)
		if n == nil {
			return nil, fmt.Errorf("replace_text: node not found: %s", op.ID)
		}
		if n.Type != UINodeText {
			return nil, fmt.Errorf("replace_text: node %s is not a text node", op.ID)
		}
		n.Text = op.Text
		return root, nil
	default:
		return nil, fmt.Errorf("unknown op: %s", op.Op)
	}
}

// InsertChild inserts child into parent at index. A nil index appends. An
// out-of-range index is an error and leaves parent unchanged.
func InsertChild(parent *UINode, index *int, child UINode) error {
	at := len(parent.Children)
	if index != nil {
		at = *index
		if at < 0 || at > len(parent.Children) {
			return fmt.Errorf("insert index out of range: %d", at)
		}
	}
	parent.Children = append(parent.Children, UINode{})
	copy(parent.Children[at+1:], parent.Children[at:])
	parent.Children[at] = child
	return nil
}

// FindNode returns a pointer to the node with the given ID, or nil. The pointer
// aliases the tree, so callers that must not mutate the live graph work on a
// CloneNode first.
func FindNode(n *UINode, id string) *UINode {
	if n == nil {
		return nil
	}
	if n.ID == id {
		return n
	}
	for i := range n.Children {
		if found := FindNode(&n.Children[i], id); found != nil {
			return found
		}
	}
	return nil
}

// RemoveNode removes the child with the given ID from n's subtree. It returns
// true if removed. It cannot remove n itself; callers handle the root case.
func RemoveNode(n *UINode, id string) bool {
	if n == nil {
		return false
	}
	for i := range n.Children {
		if n.Children[i].ID == id {
			n.Children = append(n.Children[:i], n.Children[i+1:]...)
			return true
		}
		if RemoveNode(&n.Children[i], id) {
			return true
		}
	}
	return false
}

// CloneNode returns a deep copy of n, so a batch of ops can be validated on a
// working copy without touching the live tree. A nil input yields nil.
func CloneNode(n *UINode) *UINode {
	if n == nil {
		return nil
	}
	out := UINode{ID: n.ID, Type: n.Type, Text: n.Text, Props: CloneProps(n.Props)}
	if len(n.Children) > 0 {
		out.Children = make([]UINode, len(n.Children))
		for i := range n.Children {
			out.Children[i] = *CloneNode(&n.Children[i])
		}
	}
	return &out
}

// CloneProps returns a deep copy of props (the slice fields are duplicated so a
// caller cannot alias the live node's padding/margin). A nil input yields nil.
func CloneProps(p *UIProps) *UIProps {
	if p == nil {
		return nil
	}
	out := *p
	out.Padding = append([]int(nil), p.Padding...)
	out.Margin = append([]int(nil), p.Margin...)
	return &out
}

// CollapseBlankLineRuns collapses runs of 3+ consecutive newlines down to 2
// (at most one blank line). Renderers that keep a node's text streaming via
// append_text cannot strip trailing newlines (the text may still be growing),
// but an already-excessive run is safe to collapse mid-stream. Shared so both
// renderers agree on the spacing rule.
func CollapseBlankLineRuns(s string) string {
	for strings.Contains(s, "\n\n\n") {
		s = strings.ReplaceAll(s, "\n\n\n", "\n\n")
	}
	return s
}
