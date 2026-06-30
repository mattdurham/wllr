package sdk

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// UIArea declares a named region of the screen owned by a single extension.
// An extension may own one area, inject into an existing area's scene graph, or
// spawn additional areas. The harness composites areas by Placement and Weight.
//
// ID must be unique across all areas. Two extensions may not own the same area;
// the host rejects a create for an ID that already exists.
type UIArea struct {
	ID        string          `json:"id"`
	Placement UIAreaPlacement `json:"placement"`
	// Weight is a relative size hint among areas sharing a placement. Zero means
	// "harness default". Higher weights receive proportionally more space.
	Weight int `json:"weight,omitempty"`
}
