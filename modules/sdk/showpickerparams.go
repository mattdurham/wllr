package sdk

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// ShowPickerParams is the params blob for the show_picker host_call.
type ShowPickerParams struct {
	Title    string           `json:"title"`
	Callback string           `json:"callback"`
	Items    []ShowPickerItem `json:"items"`
}
