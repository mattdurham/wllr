package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"fmt"
	"strings"
	"sync"

	lipgloss "charm.land/lipgloss/v2"
	"github.com/mattdurham/wllr/modules/sdk"
)

// SceneRenderer holds the declarative UI scene graph for every area an
// extension has created, applies incremental patches, and renders an area's
// tree to a string. It is the generic, data-driven renderer that lets any WASM
// extension drive the TUI (see sdk.UINode / sdk.UIPatchOp).
//
// All methods are safe for concurrent use; the bubbletea Update loop applies
// patches while View reads. A single mutex guards the area map and trees.
type SceneRenderer struct {
	areas map[string]*sceneArea
	order []string // area IDs in creation order, for deterministic compositing
	mu    sync.RWMutex
}

// sceneArea is one named region owned by an extension.
type sceneArea struct {
	root      *sdk.UINode
	id        string
	placement sdk.UIAreaPlacement
	// Sizing constraints — each accepts "" (unconstrained), "N" (absolute), or "N%" (percent).
	minHeight string
	maxHeight string
	minWidth  string
	maxWidth  string
	weight    int
}

// NewSceneRenderer returns an empty SceneRenderer.
func NewSceneRenderer() *SceneRenderer {
	return &SceneRenderer{areas: map[string]*sceneArea{}}
}

// CreateArea registers a new area. It returns an error if the ID already exists.
func (s *SceneRenderer) CreateArea(a sdk.UIArea) error {
	if a.ID == "" {
		return fmt.Errorf("ui_create_area: area id is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.areas[a.ID]; ok {
		return fmt.Errorf("ui_create_area: area already exists: %s", a.ID)
	}
	s.areas[a.ID] = &sceneArea{
		id:        a.ID,
		placement: a.Placement,
		weight:    a.Weight,
		minHeight: a.MinHeight,
		maxHeight: a.MaxHeight,
		minWidth:  a.MinWidth,
		maxWidth:  a.MaxWidth,
	}
	s.order = append(s.order, a.ID)
	return nil
}

// RemoveArea deletes an area and its scene graph. Removing a missing area is a no-op.
func (s *SceneRenderer) RemoveArea(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.areas, id)
	out := s.order[:0]
	for _, x := range s.order {
		if x != id {
			out = append(out, x)
		}
	}
	s.order = out
}

// UpdateArea applies a UIUpdateAreaParams to an existing area, replacing only
// the fields that are non-empty / non-nil. Returns an error if the ID is unknown.
func (s *SceneRenderer) UpdateArea(p sdk.UIUpdateAreaParams) error {
	if p.ID == "" {
		return fmt.Errorf("ui_update_area: area id is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	area, ok := s.areas[p.ID]
	if !ok {
		return fmt.Errorf("ui_update_area: area not found: %s", p.ID)
	}
	if p.MinHeight != "" {
		area.minHeight = p.MinHeight
	}
	if p.MaxHeight != "" {
		area.maxHeight = p.MaxHeight
	}
	if p.MinWidth != "" {
		area.minWidth = p.MinWidth
	}
	if p.MaxWidth != "" {
		area.maxWidth = p.MaxWidth
	}
	if p.Weight != nil {
		area.weight = *p.Weight
	}
	return nil
}

// ConstrainWidth resolves the MinWidth/MaxWidth constraints for an area against
// the terminal width and returns the clamped render width. Returns termWidth
// unchanged for an unknown area or when no constraints are set.
func (s *SceneRenderer) ConstrainWidth(id string, termWidth int) int {
	s.mu.RLock()
	area, ok := s.areas[id]
	s.mu.RUnlock()
	if !ok {
		return termWidth
	}
	w := termWidth
	if min, ok := resolveConstraint(area.minWidth, termWidth); ok && w < min {
		w = min
	}
	if max, ok := resolveConstraint(area.maxWidth, termWidth); ok && w > max {
		w = max
	}
	if w < 0 {
		w = 0
	}
	return w
}

// ConstrainHeight clamps a rendered line count to the MinHeight/MaxHeight
// constraints for an area, resolved against termHeight. Returns lines unchanged
// for an unknown area or when no constraints are set.
func (s *SceneRenderer) ConstrainHeight(id string, lines int, termHeight int) int {
	s.mu.RLock()
	area, ok := s.areas[id]
	s.mu.RUnlock()
	if !ok {
		return lines
	}
	if min, ok := resolveConstraint(area.minHeight, termHeight); ok && lines < min {
		lines = min
	}
	if max, ok := resolveConstraint(area.maxHeight, termHeight); ok && lines > max {
		lines = max
	}
	if lines < 0 {
		lines = 0
	}
	return lines
}

// resolveConstraint parses a constraint string ("N" absolute or "N%" percent
// of total) and returns the resolved int value. Returns (0, false) for empty
// or unparseable strings.
func resolveConstraint(v string, total int) (int, bool) {
	if v == "" {
		return 0, false
	}
	if strings.HasSuffix(v, "%") {
		n, ok := parseDecimal(v[:len(v)-1])
		if !ok || n < 0 {
			return 0, false
		}
		return total * n / 100, true
	}
	n, ok := parseDecimal(v)
	if !ok {
		return 0, false
	}
	return n, true
}

// parseDecimal parses a non-negative decimal integer string. Returns (0, false)
// for empty strings, negative values, or non-digit characters.
func parseDecimal(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	n := 0
	for _, ch := range s {
		if ch < '0' || ch > '9' {
			return 0, false
		}
		n = n*10 + int(ch-'0')
	}
	return n, true
}

// HasArea reports whether an area exists.
func (s *SceneRenderer) HasArea(id string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.areas[id]
	return ok
}

// AreasByPlacement returns the IDs of areas with the given placement, in
// creation order.
func (s *SceneRenderer) AreasByPlacement(p sdk.UIAreaPlacement) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var ids []string
	for _, id := range s.order {
		if s.areas[id].placement == p {
			ids = append(ids, id)
		}
	}
	return ids
}

// ApplyPatch applies a batch of ops to an area atomically. The batch is
// validated against a working copy first; if any op references a missing node
// or targets a missing area, the whole batch is rejected and the live tree is
// unchanged.
func (s *SceneRenderer) ApplyPatch(p sdk.UIPatchParams) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	area, ok := s.areas[p.Area]
	if !ok {
		return fmt.Errorf("ui_patch: unknown area: %s", p.Area)
	}
	// Work on a clone so a mid-batch failure leaves the live tree intact.
	working := cloneNode(area.root)
	for i, op := range p.Ops {
		next, err := applyOp(working, op)
		if err != nil {
			return fmt.Errorf("ui_patch op %d (%s): %w", i, op.Op, err)
		}
		working = next
	}
	area.root = working
	return nil
}

// applyOp applies a single op to root, returning the new root.
func applyOp(root *sdk.UINode, op sdk.UIPatchOp) (*sdk.UINode, error) {
	switch op.Op {
	case sdk.UIOpSetRoot:
		if op.Node == nil {
			return nil, fmt.Errorf("set_root requires node")
		}
		n := cloneNode(op.Node)
		return n, nil
	case sdk.UIOpInsert:
		if op.Node == nil {
			return nil, fmt.Errorf("insert requires node")
		}
		if op.Parent == "" {
			// Insert at the root container level requires an existing root container.
			if root == nil {
				return nil, fmt.Errorf("insert: no root; use set_root first")
			}
			if err := insertChild(root, op.Index, *cloneNode(op.Node)); err != nil {
				return nil, err
			}
			return root, nil
		}
		parent := findNode(root, op.Parent)
		if parent == nil {
			return nil, fmt.Errorf("insert: parent not found: %s", op.Parent)
		}
		if err := insertChild(parent, op.Index, *cloneNode(op.Node)); err != nil {
			return nil, err
		}
		return root, nil
	case sdk.UIOpUpdate:
		n := findNode(root, op.ID)
		if n == nil {
			return nil, fmt.Errorf("update: node not found: %s", op.ID)
		}
		n.Props = cloneProps(op.Props)
		return root, nil
	case sdk.UIOpRemove:
		if root != nil && root.ID == op.ID {
			return nil, nil
		}
		if !removeNode(root, op.ID) {
			return nil, fmt.Errorf("remove: node not found: %s", op.ID)
		}
		return root, nil
	case sdk.UIOpAppendText:
		n := findNode(root, op.ID)
		if n == nil {
			return nil, fmt.Errorf("append_text: node not found: %s", op.ID)
		}
		if n.Type != sdk.UINodeText {
			return nil, fmt.Errorf("append_text: node %s is not a text node", op.ID)
		}
		n.Text += op.Text
		return root, nil
	default:
		return nil, fmt.Errorf("unknown op: %s", op.Op)
	}
}

func insertChild(parent *sdk.UINode, index *int, child sdk.UINode) error {
	at := len(parent.Children)
	if index != nil {
		at = *index
		if at < 0 || at > len(parent.Children) {
			return fmt.Errorf("insert index out of range: %d", at)
		}
	}
	parent.Children = append(parent.Children, sdk.UINode{})
	copy(parent.Children[at+1:], parent.Children[at:])
	parent.Children[at] = child
	return nil
}

// findNode returns a pointer to the node with the given ID, or nil.
func findNode(n *sdk.UINode, id string) *sdk.UINode {
	if n == nil {
		return nil
	}
	if n.ID == id {
		return n
	}
	for i := range n.Children {
		if found := findNode(&n.Children[i], id); found != nil {
			return found
		}
	}
	return nil
}

// removeNode removes the child with the given ID from n's subtree. Returns true
// if removed. Cannot remove the root itself (handled by caller).
func removeNode(n *sdk.UINode, id string) bool {
	if n == nil {
		return false
	}
	for i := range n.Children {
		if n.Children[i].ID == id {
			n.Children = append(n.Children[:i], n.Children[i+1:]...)
			return true
		}
		if removeNode(&n.Children[i], id) {
			return true
		}
	}
	return false
}

func cloneNode(n *sdk.UINode) *sdk.UINode {
	if n == nil {
		return nil
	}
	out := sdk.UINode{ID: n.ID, Type: n.Type, Text: n.Text, Props: cloneProps(n.Props)}
	if len(n.Children) > 0 {
		out.Children = make([]sdk.UINode, len(n.Children))
		for i := range n.Children {
			out.Children[i] = *cloneNode(&n.Children[i])
		}
	}
	return &out
}

func cloneProps(p *sdk.UIProps) *sdk.UIProps {
	if p == nil {
		return nil
	}
	out := *p
	if p.Padding != nil {
		out.Padding = append([]int(nil), p.Padding...)
	}
	if p.Margin != nil {
		out.Margin = append([]int(nil), p.Margin...)
	}
	return &out
}

// Render renders the area's scene graph to a string sized to width. Returns ""
// for an unknown or empty area.
func (s *SceneRenderer) Render(areaID string, width int) string {
	s.mu.RLock()
	area, ok := s.areas[areaID]
	var root *sdk.UINode
	if ok {
		root = cloneNode(area.root)
	}
	s.mu.RUnlock()
	if !ok || root == nil {
		return ""
	}
	return renderNode(*root, width)
}

// RenderNode renders a single node in an area. When textOverride is non-nil and
// the node is a text node, the override is rendered with the node's current
// styling without mutating the live scene.
func (s *SceneRenderer) RenderNode(areaID, nodeID string, width int, textOverride *string) (string, bool) {
	s.mu.RLock()
	area, ok := s.areas[areaID]
	var node *sdk.UINode
	if ok {
		node = cloneNode(findNode(area.root, nodeID))
	}
	s.mu.RUnlock()
	if !ok || node == nil {
		return "", false
	}
	if textOverride != nil && node.Type == sdk.UINodeText {
		node.Text = *textOverride
	}
	return renderNode(*node, width), true
}

// RenderAppendTextNode renders the current and previous states of an appended
// text node. The previous state is derived by removing appendedText from the
// current node text; the live scene is not mutated.
func (s *SceneRenderer) RenderAppendTextNode(areaID, nodeID string, width int, appendedText string) (previous, current string, ok bool) {
	if appendedText == "" {
		return "", "", false
	}
	s.mu.RLock()
	area, areaOK := s.areas[areaID]
	var node *sdk.UINode
	if areaOK {
		node = cloneNode(findNode(area.root, nodeID))
	}
	s.mu.RUnlock()
	if !areaOK || node == nil || node.Type != sdk.UINodeText {
		return "", "", false
	}
	previousText, hasSuffix := strings.CutSuffix(node.Text, appendedText)
	if !hasSuffix {
		return "", "", false
	}
	current = renderNode(*node, width)
	node.Text = previousText
	previous = renderNode(*node, width)
	return previous, current, true
}

// renderNode renders a single node and its subtree to a string given the
// available content width.
func renderNode(n sdk.UINode, width int) string {
	style, innerWidth := styleFromProps(n.Props, width)
	var body string
	switch n.Type {
	case sdk.UINodeText:
		body = n.Text
		if n.Props != nil && n.Props.Wrap && innerWidth > 0 {
			body = lipgloss.Wrap(body, innerWidth, "")
		}
	case sdk.UINodeDivider:
		w := innerWidth
		if w <= 0 {
			w = width
		}
		if w < 1 {
			w = 1
		}
		body = strings.Repeat("─", w)
	case sdk.UINodeSpinner:
		body = "⠋"
	case sdk.UINodeHStack:
		parts := make([]string, 0, len(n.Children))
		for _, c := range n.Children {
			parts = append(parts, renderNode(c, innerWidth))
		}
		body = lipgloss.JoinHorizontal(lipgloss.Top, parts...)
	case sdk.UINodeVStack, sdk.UINodeViewport:
		parts := make([]string, 0, len(n.Children))
		for _, c := range n.Children {
			parts = append(parts, renderNode(c, innerWidth))
		}
		body = lipgloss.JoinVertical(lipgloss.Left, parts...)
	default:
		// Unknown node type: render an empty box (forward-compatibility).
		body = ""
	}
	return style.Render(body)
}

// themeColor resolves a named theme token to a hex colour string. Unknown
// tokens that look like hex pass through; anything else yields ok=false.
func themeColor(token string) (string, bool) {
	switch token {
	case "":
		return "", false
	case "accent":
		return "#89CFF0", true
	case "muted":
		return "#888888", true
	case "error":
		return "#FF4444", true
	case "success":
		return "#44CC66", true
	case "warning":
		return "#E5C07B", true
	case "fg":
		return "#FFFFFF", true
	case "bg":
		return "#1A1A1A", true
	}
	if strings.HasPrefix(token, "#") {
		return token, true
	}
	return "", false
}

// styleFromProps builds a lipgloss.Style from props and returns the style plus
// the inner content width available to children after borders/padding.
func styleFromProps(p *sdk.UIProps, width int) (lipgloss.Style, int) {
	style := lipgloss.NewStyle()
	inner := width
	if p == nil {
		return style, inner
	}
	if c, ok := themeColor(p.Fg); ok {
		style = style.Foreground(lipgloss.Color(c))
	}
	if c, ok := themeColor(p.Bg); ok {
		style = style.Background(lipgloss.Color(c))
	}
	if p.Bold {
		style = style.Bold(true)
	}
	if p.Italic {
		style = style.Italic(true)
	}
	if p.Underline {
		style = style.Underline(true)
	}
	if p.Faint {
		style = style.Faint(true)
	}
	switch p.Border {
	case "normal":
		style = style.Border(lipgloss.NormalBorder())
		inner -= 2
	case borderRounded:
		style = style.Border(lipgloss.RoundedBorder())
		inner -= 2
	case "thick":
		style = style.Border(lipgloss.ThickBorder())
		inner -= 2
	case "double":
		style = style.Border(lipgloss.DoubleBorder())
		inner -= 2
	}
	if t, r, b, l, ok := sides(p.Padding); ok {
		style = style.Padding(t, r, b, l)
		inner -= l + r
	}
	if t, r, b, l, ok := sides(p.Margin); ok {
		style = style.Margin(t, r, b, l)
		inner -= l + r
	}
	switch p.Align {
	case "center":
		style = style.Align(lipgloss.Center)
	case "right":
		style = style.Align(lipgloss.Right)
	case "left":
		style = style.Align(lipgloss.Left)
	}
	if w, ok := parseDimension(p.Width, width); ok {
		style = style.Width(w)
		inner = w
		// account for border/padding already subtracted from a fresh width
		if t, r, b, l, has := sides(p.Padding); has {
			inner -= l + r
			_ = t
			_ = b
		}
		if p.Border != "" && p.Border != "none" {
			inner -= 2
		}
	}
	if h, ok := parseDimension(p.Height, 0); ok {
		style = style.Height(h)
	}
	if inner < 0 {
		inner = 0
	}
	return style, inner
}

// sides expands a 1/2/4-length cell spec into top,right,bottom,left.
func sides(v []int) (t, r, b, l int, ok bool) {
	switch len(v) {
	case 1:
		return v[0], v[0], v[0], v[0], true
	case 2:
		return v[0], v[1], v[0], v[1], true
	case 4:
		return v[0], v[1], v[2], v[3], true
	}
	return 0, 0, 0, 0, false
}

// parseDimension resolves "fill", "auto", or a decimal cell count. "fill"
// resolves to fillWidth; "auto"/"" returns ok=false (size to content).
func parseDimension(v string, fillWidth int) (int, bool) {
	switch v {
	case "", "auto":
		return 0, false
	case "fill":
		if fillWidth <= 0 {
			return 0, false
		}
		return fillWidth, true
	}
	n := 0
	for _, ch := range v {
		if ch < '0' || ch > '9' {
			return 0, false
		}
		n = n*10 + int(ch-'0')
	}
	return n, true
}
