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
