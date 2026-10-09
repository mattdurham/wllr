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
	if min, ok := sdk.ResolveConstraint(area.minWidth, termWidth); ok && w < min {
		w = min
	}
	if max, ok := sdk.ResolveConstraint(area.maxWidth, termWidth); ok && w > max {
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
	if min, ok := sdk.ResolveConstraint(area.minHeight, termHeight); ok && lines < min {
		lines = min
	}
	if max, ok := sdk.ResolveConstraint(area.maxHeight, termHeight); ok && lines > max {
		lines = max
	}
	if lines < 0 {
		lines = 0
	}
	return lines
}

// resolveConstraint delegates to sdk.ResolveConstraint. It is kept as a thin
// package-local shim so existing call sites and tests read unchanged.
func resolveConstraint(v string, total int) (int, bool) {
	return sdk.ResolveConstraint(v, total)
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
//
// The per-op semantics live in sdk.ApplyOp so the HTML renderer
// (modules/server) manipulates the graph identically.
func (s *SceneRenderer) ApplyPatch(p sdk.UIPatchParams) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	area, ok := s.areas[p.Area]
	if !ok {
		return fmt.Errorf("ui_patch: unknown area: %s", p.Area)
	}
	// Work on a clone so a mid-batch failure leaves the live tree intact.
	working := sdk.CloneNode(area.root)
	for i, op := range p.Ops {
		next, err := sdk.ApplyOp(working, op)
		if err != nil {
			return fmt.Errorf("ui_patch op %d (%s): %w", i, op.Op, err)
		}
		working = next
	}
	area.root = working
	return nil
}

// Render renders the area's scene graph to a string sized to width. Returns ""
// for an unknown or empty area.
func (s *SceneRenderer) Render(areaID string, width int) string {
	s.mu.RLock()
	area, ok := s.areas[areaID]
	var root *sdk.UINode
	if ok {
		root = sdk.CloneNode(area.root)
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
		node = sdk.CloneNode(sdk.FindNode(area.root, nodeID))
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
func (s *SceneRenderer) RenderAppendTextNode(
	areaID, nodeID string,
	width int,
	appendedText string,
) (previous, current string, ok bool) {
	if appendedText == "" {
		return "", "", false
	}
	s.mu.RLock()
	area, areaOK := s.areas[areaID]
	var node *sdk.UINode
	if areaOK {
		node = sdk.CloneNode(sdk.FindNode(area.root, nodeID))
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

// collapseBlankLineRuns collapses runs of 3+ consecutive newlines down to 2
// (i.e. at most one blank line), fixing excessive vertical gaps (issue #26)
// without ever removing a genuine single paragraph break. Unlike trimming
// trailing newlines, this is safe to apply mid-stream: a node's text may
// still be growing via append_text, and a run this large is already
// excessive regardless of whether more text follows.
func collapseBlankLineRuns(s string) string { return sdk.CollapseBlankLineRuns(s) }

// renderNode renders a single node and its subtree to a string given the
// available content width. Text nodes only have excessive (3+) consecutive
// newlines collapsed here; a wholly-trailing newline is NOT stripped at
// render time, since a node's text may still be growing mid-stream (via
// append_text) and a "trailing" newline at render time is often not actually
// final — stripping it here would visibly drop paragraph breaks for the
// window between it arriving and the next chunk. Producers of genuinely
// final text (e.g. the chat extension's onChatMessageEnd) are responsible
// for trimming trailing whitespace before it reaches the scene.
func renderNode(n sdk.UINode, width int) string {
	style, innerWidth := styleFromProps(n.Props, width)
	var body string
	switch n.Type {
	case sdk.UINodeText:
		body = collapseBlankLineRuns(n.Text)
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
	if t, r, b, l, ok := sdk.PropSides(p.Padding); ok {
		style = style.Padding(t, r, b, l)
		inner -= l + r
	}
	if t, r, b, l, ok := sdk.PropSides(p.Margin); ok {
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
	if w, ok := sdk.ParseDimension(p.Width, width); ok {
		style = style.Width(w)
		inner = w
		// account for border/padding already subtracted from a fresh width
		if t, r, b, l, has := sdk.PropSides(p.Padding); has {
			inner -= l + r
			_ = t
			_ = b
		}
		if p.Border != "" && p.Border != "none" {
			inner -= 2
		}
	}
	if h, ok := sdk.ParseDimension(p.Height, 0); ok {
		style = style.Height(h)
	}
	if inner < 0 {
		inner = 0
	}
	return style, inner
}
