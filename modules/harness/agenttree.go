package harness

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
)

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

// AgentTreeCallback is fired when the user focuses an agent. The harness emits
// EventOnCommand with this name and the agent ID, so the extension can switch
// its transcript and input target.
const AgentTreeCallback = "agents:focus"

// AgentTreeView is an interactive overlay listing agents as a tree with
// expand/collapse. It replaces the static modal that previously rendered the
// agent list: a modal cannot express selection, so focus and folding need a
// component with its own key handling.
type AgentTreeView struct {
	Nodes    []AgentTreeNode
	Callback string

	// expanded records which node IDs are open. Nodes default to expanded so
	// the first view shows the whole fleet; collapsing is the deliberate act.
	expanded map[string]bool
	// cursor is an index into the visible (flattened) rows.
	cursor   int
	offset   int
	width    int
	height   int
	active   bool
	tooSmall bool
}

// Open activates the tree with the given nodes, expanding everything by default.
func (t *AgentTreeView) Open(nodes []AgentTreeNode, callback string) {
	t.Nodes = nodes
	t.Callback = callback
	t.expanded = make(map[string]bool, len(nodes))
	for _, n := range nodes {
		t.expanded[n.ID] = true
	}
	t.cursor = 0
	t.offset = 0
	t.active = true
}

// Close deactivates the tree.
func (t *AgentTreeView) Close() {
	t.active = false
	t.Nodes = nil
	t.Callback = ""
	t.expanded = nil
	t.cursor = 0
	t.offset = 0
}

// IsActive reports whether the tree overlay is shown.
func (t *AgentTreeView) IsActive() bool { return t.active }

// SetSize updates the available dimensions.
func (t *AgentTreeView) SetSize(width, height int) {
	t.width = width
	t.height = height
	if h := t.visibleRows(); h >= 1 {
		t.tooSmall = false
	} else {
		t.tooSmall = true
	}
}

// visibleRows is how many node rows fit above the footer.
func (t *AgentTreeView) visibleRows() int {
	// Top border + footer = 2 overhead lines.
	rows := t.height - 2
	if rows < 1 {
		rows = 1
	}
	return rows
}

// row is one flattened, renderable line: a node plus its depth.
type agentTreeRow struct {
	node  AgentTreeNode
	depth int
}

// rows flattens the tree in a stable order, skipping the children of collapsed
// nodes. Siblings are sorted by ID so the view does not reshuffle between
// openings, which a map-backed node list would otherwise cause.
func (t *AgentTreeView) rows() []agentTreeRow {
	children := make(map[string][]AgentTreeNode)
	var roots []AgentTreeNode
	known := make(map[string]bool, len(t.Nodes))
	for _, n := range t.Nodes {
		known[n.ID] = true
	}
	for _, n := range t.Nodes {
		// A node whose parent is absent is treated as a root, so a partial or
		// stale node list still renders every agent rather than dropping them.
		if n.ParentID == "" || !known[n.ParentID] {
			roots = append(roots, n)
			continue
		}
		children[n.ParentID] = append(children[n.ParentID], n)
	}
	// Children are rendered in the order the caller supplied them, which is
	// spawn order. The extension already returns agents in that order, so
	// re-sorting here would present siblings in an order the user did not
	// create them in. Roots keep that same order too.
	var out []agentTreeRow
	var walk func(nodes []AgentTreeNode, depth int)
	walk = func(nodes []AgentTreeNode, depth int) {
		for _, n := range nodes {
			out = append(out, agentTreeRow{node: n, depth: depth})
			kids := children[n.ID]
			if len(kids) == 0 || !t.expanded[n.ID] {
				continue
			}
			walk(kids, depth+1)
		}
	}
	walk(roots, 0)
	return out
}

// Highlighted returns the cursor's node and whether one exists.
func (t *AgentTreeView) Highlighted() (AgentTreeNode, bool) {
	rows := t.rows()
	if len(rows) == 0 || t.cursor < 0 || t.cursor >= len(rows) {
		return AgentTreeNode{}, false
	}
	return rows[t.cursor].node, true
}

// toggle expands or collapses the cursor's node. Returns whether it has
// children (a leaf has nothing to fold), so callers can distinguish the two.
func (t *AgentTreeView) toggle() bool {
	node, ok := t.Highlighted()
	if !ok {
		return false
	}
	if !t.hasChildren(node.ID) {
		return false
	}
	t.expanded[node.ID] = !t.expanded[node.ID]
	t.clampCursor()
	return true
}

// expand opens the cursor's node; collapse closes it.
func (t *AgentTreeView) expand() {
	if node, ok := t.Highlighted(); ok && t.hasChildren(node.ID) {
		t.expanded[node.ID] = true
	}
}

func (t *AgentTreeView) collapse() {
	if node, ok := t.Highlighted(); ok && t.hasChildren(node.ID) {
		t.expanded[node.ID] = false
		t.clampCursor()
	}
}

// hasChildren reports whether any known node names id as its parent. A node
// whose parent is absent is rendered as a root, so it is not a child of
// anything the view can show.
func (t *AgentTreeView) hasChildren(id string) bool {
	if id == "" {
		return false
	}
	for _, n := range t.Nodes {
		if n.ParentID == id && n.ID != id {
			return true
		}
	}
	return false
}

// clampCursor keeps the cursor inside the visible rows after a collapse hides
// rows beneath it.
func (t *AgentTreeView) clampCursor() {
	rows := t.rows()
	if len(rows) == 0 {
		t.cursor = 0
		t.offset = 0
		return
	}
	if t.cursor >= len(rows) {
		t.cursor = len(rows) - 1
	}
	if t.cursor < 0 {
		t.cursor = 0
	}
	t.ensureVisible()
}

// ensureVisible scrolls so the cursor is inside the visible window.
func (t *AgentTreeView) ensureVisible() {
	visible := t.visibleRows()
	if t.cursor < t.offset {
		t.offset = t.cursor
	}
	if t.cursor >= t.offset+visible {
		t.offset = t.cursor - visible + 1
	}
	if t.offset < 0 {
		t.offset = 0
	}
}

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

// HandleKey processes a key press, reporting whether the tree consumed it.
func (t *AgentTreeView) HandleKey(kp tea.KeyPressMsg) AgentTreeKeyResult {
	rows := t.rows()
	switch kp.String() {
	case "q", keyEsc:
		return AgentTreeKeyResult{Closed: true, Handled: true}
	case "enter":
		if node, ok := t.Highlighted(); ok {
			return AgentTreeKeyResult{Focused: node.ID, Handled: true}
		}
		return AgentTreeKeyResult{Handled: true}
	case "up":
		if t.cursor > 0 {
			t.cursor--
			t.ensureVisible()
		}
		return AgentTreeKeyResult{Handled: true}
	case "down":
		if t.cursor < len(rows)-1 {
			t.cursor++
			t.ensureVisible()
		}
		return AgentTreeKeyResult{Handled: true}
	case "right", "l":
		t.expand()
		return AgentTreeKeyResult{Folded: true, Handled: true}
	case "left", "h":
		t.collapse()
		return AgentTreeKeyResult{Folded: true, Handled: true}
	case " ":
		t.toggle()
		return AgentTreeKeyResult{Folded: true, Handled: true}
	}
	// Anything else (notably esc) is left for the caller.
	return AgentTreeKeyResult{}
}

var (
	treeBorderStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))
	treeTitleStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#89CFF0"))
	treeSelectStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("#1A4A8A")).
			Foreground(lipgloss.Color("#FFFFFF"))
)

// View renders the overlay into a block of exactly t.height lines.
func (t *AgentTreeView) View() string {
	if t.width < 12 {
		t.width = 12
	}
	if t.height < 3 {
		t.height = 3
	}
	inner := t.width - 2
	content := inner - 2

	var sb strings.Builder
	title := " Agents  (↑↓ move · →← fold · enter focus · q=quit) "
	sb.WriteString(treeBorderStyle.Render("╭") +
		treeTitleStyle.Render(truncateRunes(title, inner)) +
		treeBorderStyle.Render(strings.Repeat("─", max(0, inner-lipgloss.Width(title)))+"╮") + "\n")

	rows := t.rows()
	visible := t.visibleRows()
	end := t.offset + visible
	if end > len(rows) {
		end = len(rows)
	}
	rendered := 0
	for i := t.offset; i < end; i++ {
		r := rows[i]
		line := t.renderRow(r, i == t.cursor, content)
		padded := line + strings.Repeat(" ", max(0, content-lipgloss.Width(line)))
		if i == t.cursor {
			sb.WriteString(treeBorderStyle.Render("│") + " " + treeSelectStyle.Render(padded) + " " + treeBorderStyle.Render("│") + "\n")
		} else {
			sb.WriteString(treeBorderStyle.Render("│") + " " + padded + " " + treeBorderStyle.Render("│") + "\n")
		}
		rendered++
	}
	for rendered < visible {
		sb.WriteString(treeBorderStyle.Render("│") + " " + strings.Repeat(" ", content) + " " + treeBorderStyle.Render("│") + "\n")
		rendered++
	}

	footer := " " + t.footerHint() + " "
	sb.WriteString(treeBorderStyle.Render("╰") +
		treeBorderStyle.Render(footer+strings.Repeat("─", max(0, inner-lipgloss.Width(footer)))) + "╯")
	return sb.String()
}

// renderRow draws one node: an indent, a fold marker for parents, the label,
// and the detail line when the node is expanded.
func (t *AgentTreeView) renderRow(r agentTreeRow, selected bool, width int) string {
	indent := strings.Repeat("  ", r.depth)
	marker := "  "
	if t.hasChildren(r.node.ID) {
		if t.expanded[r.node.ID] {
			marker = "▾ "
		} else {
			marker = "▸ "
		}
	}
	label := r.node.Label
	if label == "" {
		label = r.node.ID
	}
	line := indent + marker + label
	if r.node.Detail != "" && t.expanded[r.node.ID] {
		line += "  " + r.node.Detail
	}
	return truncateRunes(line, width)
}

// footerHint summarizes position and fold state.
func (t *AgentTreeView) footerHint() string {
	rows := t.rows()
	pos := ""
	if len(rows) > 0 {
		pos = " " + itoa(t.cursor+1) + "/" + itoa(len(rows))
	}
	if node, ok := t.Highlighted(); ok && t.hasChildren(node.ID) {
		if t.expanded[node.ID] {
			pos += "  (expanded)"
		} else {
			pos += "  (collapsed)"
		}
	}
	return pos
}

// itoa is a tiny int-to-string helper to avoid pulling strconv into the render
// path; the values here are always small.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
