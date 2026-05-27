package sdk

// ShowPickerParams is the params blob for the show_picker host_call.
type ShowPickerParams struct {
	Title    string           `json:"title"`
	Callback string           `json:"callback"`
	Items    []ShowPickerItem `json:"items"`
}
