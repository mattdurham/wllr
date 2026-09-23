package sdk

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// ShowPickerParams is the params blob for the show_picker host_call.
type ShowPickerParams struct {
	Title    string           `json:"title"`
	Callback string           `json:"callback"`
	Items    []ShowPickerItem `json:"items"`
	// Split requests a two-pane layout: a type-to-filter list on the left
	// and the highlighted item's Preview text on the right.
	Split bool `json:"split,omitempty"`
}
