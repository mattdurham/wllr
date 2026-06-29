package sdk

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// UIAreaPlacement is a layout hint telling the harness where to composite an
// area. The harness owns final layout; placement is advisory.
type UIAreaPlacement string

const (
	// UIAreaMain is the primary content region (e.g. the chat transcript).
	UIAreaMain UIAreaPlacement = "main"
	// UIAreaSidebar is a vertical strip beside the main region.
	UIAreaSidebar UIAreaPlacement = "sidebar"
	// UIAreaStatus is a thin region along the bottom edge.
	UIAreaStatus UIAreaPlacement = "status"
	// UIAreaOverlay floats above other areas (e.g. modals, pickers).
	UIAreaOverlay UIAreaPlacement = "overlay"
)

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

// UICreateAreaParams is the params blob for the ui_create_area host_call.
type UICreateAreaParams struct {
	Area UIArea `json:"area"`
}
