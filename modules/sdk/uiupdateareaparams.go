package sdk

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// UIUpdateAreaParams is the params blob for the ui_update_area host_call.
// All constraint fields are optional — omitted (empty) fields leave the current
// value on the area unchanged. Weight nil means "leave unchanged".
// Returns an error if the area ID does not exist.
type UIUpdateAreaParams struct {
	ID string `json:"id"`

	// Height constraints. "" means "leave unchanged".
	MinHeight string `json:"min_height,omitempty"`
	MaxHeight string `json:"max_height,omitempty"`

	// Width constraints. "" means "leave unchanged".
	MinWidth string `json:"min_width,omitempty"`
	MaxWidth string `json:"max_width,omitempty"`

	// Weight, when non-nil, replaces the area's current weight.
	Weight *int `json:"weight,omitempty"`
}
