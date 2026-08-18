package sdk

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// ShowTextInputParams is the params blob for the show_text_input host_call.
type ShowTextInputParams struct {
	Title        string `json:"title"`
	Placeholder  string `json:"placeholder,omitempty"`
	InitialValue string `json:"initialValue,omitempty"`
	Callback     string `json:"callback"`
}
