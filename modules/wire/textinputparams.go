package wire

// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// TextInputParams is the payload for KindTextInput, mirroring the argument list
// of extension.UIBridge.ShowTextInput.
type TextInputParams struct {
	Title        string `json:"title"`
	Placeholder  string `json:"placeholder,omitempty"`
	InitialValue string `json:"initialValue,omitempty"`
	Callback     string `json:"callback"`
}
