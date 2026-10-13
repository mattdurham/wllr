package server

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// htmlscene.go renders an sdk.UINode tree to HTML. It is the browser counterpart
// of the harness's lipgloss renderer (modules/harness/scene.go): the same
// declarative node vocabulary, the same props, rendered to CSS instead of
// terminal cells. Both renderers mutate the graph through modules/sdk
// (uigraph.go); only this final node-to-pixels step is front-end specific.
//
// The renderer is total: every node type and every prop has a defined mapping,
// and an unknown node type renders as an empty box so a forward-compatible
// extension (or a newer node type) degrades gracefully rather than erroring —
// matching the terminal renderer's contract.
//
// Named colour tokens resolve to CSS custom properties with a hex fallback
// identical to the terminal theme, so the host keeps control of theming and a
// browser that never loaded the stylesheet still shows the intended colours.

import (
	"fmt"
	"html"
	"strings"

	"github.com/mattdurham/wllr/modules/sdk"
)

// themeTokenCSS maps a named theme token to a CSS colour: a custom property
// with a hex fallback. A raw hex token passes through; anything else is
// unresolvable and reports ok=false (the caller then emits no colour).
func themeTokenCSS(token string) (string, bool) {
	switch token {
	case "":
		return "", false
	case "accent":
		return "var(--wn-accent, #89CFF0)", true
	case "muted":
		return "var(--wn-muted, #888888)", true
	case "error":
		return "var(--wn-error, #FF4444)", true
	case "success":
		return "var(--wn-success, #44CC66)", true
	case "warning":
		return "var(--wn-warning, #E5C07B)", true
	case "fg":
		return "var(--wn-fg, #FFFFFF)", true
	case "bg":
		return "var(--wn-bg, #1A1A1A)", true
	}
	if strings.HasPrefix(token, "#") {
		return token, true
	}
	return "", false
}

// borderCSS maps a UIProps border name to a CSS border declaration. "none" and
// "" mean no border. currentColor follows the node's Fg, so a bordered node
// inherits its foreground colour the way the terminal border does.
func borderCSS(border string) string {
	switch border {
	case "", "none":
		return ""
	case "normal":
		return "border:1px solid currentColor;"
	case "rounded":
		return "border:1px solid currentColor;border-radius:6px;"
	case "thick":
		return "border:2px solid currentColor;"
	case "double":
		return "border:3px double currentColor;"
	}
	return ""
}

// nodeStyle builds the CSS declarations for a node's props. It always ends the
// declaration list with white-space so text preserves the newlines the terminal
// renderer would honor.
func nodeStyle(p *sdk.UIProps) string {
	var b strings.Builder
	if p == nil {
		b.WriteString("white-space:pre;")
		return b.String()
	}
	if c, ok := themeTokenCSS(p.Fg); ok {
		fmt.Fprintf(&b, "color:%s;", c)
	}
	if c, ok := themeTokenCSS(p.Bg); ok {
		fmt.Fprintf(&b, "background-color:%s;", c)
	}
	if p.Bold {
		b.WriteString("font-weight:700;")
	}
	if p.Italic {
		b.WriteString("font-style:italic;")
	}
	if p.Underline {
		b.WriteString("text-decoration:underline;")
	}
	if p.Faint {
		b.WriteString("opacity:0.6;")
	}
	b.WriteString(borderCSS(p.Border))
	if css := sdk.ExpandSideCSS(p.Padding, "ch"); css != "" {
		fmt.Fprintf(&b, "padding:%s;", css)
	}
	if css := sdk.ExpandSideCSS(p.Margin, "ch"); css != "" {
		fmt.Fprintf(&b, "margin:%s;", css)
	}
	switch p.Align {
	case "center":
		b.WriteString("text-align:center;")
	case "right":
		b.WriteString("text-align:right;")
	case "left":
		b.WriteString("text-align:left;")
	}
	if w, ok := sdk.ParseDimension(p.Width, 0); ok {
		fmt.Fprintf(&b, "width:%dch;", w)
	} else if p.Width == "fill" {
		b.WriteString("width:100%;")
	}
	if h, ok := sdk.ParseDimension(p.Height, 0); ok {
		// lh is one line box; it approximates a terminal line of height.
		fmt.Fprintf(&b, "height:%dlh;", h)
	}
	if p.Wrap {
		b.WriteString("white-space:pre-wrap;overflow-wrap:anywhere;")
	} else {
		b.WriteString("white-space:pre;")
	}
	return b.String()
}

// nodeAttr renders the shared opening tag for a node, carrying its ID so the
// client can address it for an incremental append_text.
func nodeOpen(class, id, style string) string {
	return fmt.Sprintf(`<div class="wn-node %s" data-node-id="%s" style="%s">`, class, html.EscapeString(id), style)
}

// RenderNodeHTML renders a single node and its subtree to HTML.
func RenderNodeHTML(n sdk.UINode) string {
	style := nodeStyle(n.Props)
	switch n.Type {
	case sdk.UINodeText:
		// Text can carry ANSI styling: the bundled agents extension renders
		// markdown to ANSI before placing it in a node, so the terminal and the
		// browser see the same styled content. CollapseBlankLineRuns is the same
		// spacing rule the terminal renderer applies (harness renderNode), so the
		// two front ends agree on blank-line runs. ANSItoHTML escapes the text.
		return nodeOpen("wn-text", n.ID, style) +
			`<span class="wn-text-content">` + ANSItoHTML(sdk.CollapseBlankLineRuns(n.Text)) + `</span></div>`
	case sdk.UINodeDivider:
		return fmt.Sprintf(`<hr class="wn-node wn-divider" data-node-id="%s" style="%s">`,
			html.EscapeString(n.ID), style)
	case sdk.UINodeSpinner:
		return nodeOpen("wn-spinner", n.ID, style) + `<span class="wn-spin"></span></div>`
	case sdk.UINodeHStack:
		return nodeOpen("wn-hstack", n.ID, style+`display:flex;flex-direction:row;align-items:flex-start;`) +
			renderChildren(n.Children) + `</div>`
	case sdk.UINodeVStack:
		return nodeOpen("wn-vstack", n.ID, style+`display:flex;flex-direction:column;`) +
			renderChildren(n.Children) + `</div>`
	case sdk.UINodeViewport:
		return nodeOpen("wn-viewport", n.ID, style+`display:flex;flex-direction:column;overflow-y:auto;`) +
			renderChildren(n.Children) + `</div>`
	default:
		// Unknown node type: an empty box, for forward-compatibility.
		return nodeOpen("wn-unknown", n.ID, style) + `</div>`
	}
}

// renderChildren renders a container's children in order.
func renderChildren(children []sdk.UINode) string {
	var b strings.Builder
	for _, c := range children {
		b.WriteString(RenderNodeHTML(c))
	}
	return b.String()
}

// constraintCSS translates an area constraint value into a CSS length: a bare
// number becomes a cell/line unit ("Nch" for width, "Nlh" for height) and a
// percentage passes through. It returns "" for unset or unparseable values,
// which the caller omits from the style.
func constraintCSS(v string, unit string) string {
	if v == "" {
		return ""
	}
	if n, ok := sdk.ParseDecimal(strings.TrimSuffix(v, "%")); ok && strings.HasSuffix(v, "%") {
		return fmt.Sprintf("%d%%", n)
	}
	if n, ok := sdk.ParseDecimal(v); ok {
		return fmt.Sprintf("%d%s", n, unit)
	}
	return ""
}

// areaStyle builds the CSS for an area's sizing constraints and weight.
func areaStyle(a sdk.UIArea) string {
	var b strings.Builder
	if v := constraintCSS(a.MinHeight, "lh"); v != "" {
		fmt.Fprintf(&b, "min-height:%s;", v)
	}
	if v := constraintCSS(a.MaxHeight, "lh"); v != "" {
		fmt.Fprintf(&b, "max-height:%s;", v)
	}
	if v := constraintCSS(a.MinWidth, "ch"); v != "" {
		fmt.Fprintf(&b, "min-width:%s;", v)
	}
	if v := constraintCSS(a.MaxWidth, "ch"); v != "" {
		fmt.Fprintf(&b, "max-width:%s;", v)
	}
	if a.Weight > 0 {
		fmt.Fprintf(&b, "flex-grow:%d;", a.Weight)
	}
	return b.String()
}

// RenderAreaHTML renders an area's root tree to an HTML fragment, wrapped in an
// element carrying the area ID and placement so a client can composite areas by
// region. A nil root renders an empty area (registered but not yet populated).
func RenderAreaHTML(a sdk.UIArea, root *sdk.UINode) string {
	var inner string
	if root != nil {
		inner = RenderNodeHTML(*root)
	}
	return fmt.Sprintf(
		`<div class="wn-area" data-area-id="%s" data-placement="%s" style="%s">%s</div>`,
		html.EscapeString(a.ID), html.EscapeString(string(a.Placement)), areaStyle(a), inner,
	)
}
