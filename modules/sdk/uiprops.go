package sdk

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// UIProps carries optional style and layout attributes for a UINode. All fields
// are optional; a nil *UIProps or a zero-valued struct means "inherit / use
// harness defaults". Color fields (Fg, Bg) reference named theme tokens, never
// raw color values, so the host retains full control of theming.
type UIProps struct {
	// Width and Height accept "fill" (consume available space), "auto" (size to
	// content), or a decimal cell count (e.g. "20"). Empty means "auto".
	Width  string `json:"width,omitempty"`
	Height string `json:"height,omitempty"`
	// Border names a border style: "none", "normal", "rounded", "thick",
	// "double". Empty means "none".
	Border string `json:"border,omitempty"`
	// Align controls horizontal alignment of content: "left", "center", "right".
	Align string `json:"align,omitempty"`
	// Fg and Bg name theme colour tokens (e.g. "accent", "muted", "error").
	Fg string `json:"fg,omitempty"`
	Bg string `json:"bg,omitempty"`
	// Padding and Margin are cell counts. Length 1 = all sides; length 2 =
	// [vertical, horizontal]; length 4 = [top, right, bottom, left].
	Padding []int `json:"padding,omitempty"`
	Margin  []int `json:"margin,omitempty"`
	// Text style flags.
	Bold      bool `json:"bold,omitempty"`
	Italic    bool `json:"italic,omitempty"`
	Underline bool `json:"underline,omitempty"`
	Faint     bool `json:"faint,omitempty"`
	// Wrap enables soft-wrapping of text to the node width. Default false.
	Wrap bool `json:"wrap,omitempty"`
}
