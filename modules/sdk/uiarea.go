package sdk

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// UIArea declares a named region of the screen owned by a single extension.
// An extension may own one area, inject into an existing area's scene graph, or
// spawn additional areas. The harness composites areas by Placement and Weight.
//
// ID must be unique across all areas. Two extensions may not own the same area;
// the host rejects a create for an ID that already exists.
//
// Sizing constraint fields (MinHeight, MaxHeight, MinWidth, MaxWidth) accept
// either an absolute cell/line count ("3") or a percentage of the terminal
// dimension ("20%"). Empty string means unconstrained. The harness clamps the
// rendered output to [MinHeight, MaxHeight] lines and resolves width before
// calling Render. Constraints can be updated after creation via ui_update_area.
type UIArea struct {
	ID        string          `json:"id"`
	Placement UIAreaPlacement `json:"placement"`
	// Weight is a relative size hint among areas sharing a placement. Zero means
	// "harness default". Higher weights receive proportionally more space.
	Weight int `json:"weight,omitempty"`

	// Height constraints. "" means unconstrained.
	// Values: "3" (absolute lines) or "20%" (percent of terminal height).
	// Rendered output is padded up to MinHeight and truncated to MaxHeight.
	MinHeight string `json:"min_height,omitempty"`
	MaxHeight string `json:"max_height,omitempty"`

	// Width constraints. "" means full available width.
	// Values: "80" (absolute cols) or "50%" (percent of terminal width).
	MinWidth string `json:"min_width,omitempty"`
	MaxWidth string `json:"max_width,omitempty"`
}
